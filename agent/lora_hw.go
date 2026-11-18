package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Low-level Linux device helpers for the Cap LoRa-1262 backends (LoRa chat
// and GPS): spidev, the GPIO character device (v2 uAPI), i2c-dev, termios
// and the Cap's shared 5 V rail. Plain ioctls, no cgo. Other hardware groups
// may carry similar helpers; the integrator dedupes them.

func loraIoctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// ---- spidev ----

const (
	loraSPIIocMessage1   = 0x40206b00 // SPI_IOC_MESSAGE(1)
	loraSPIIocWrMode     = 0x40016b01
	loraSPIIocWrBits     = 0x40016b03
	loraSPIIocWrMaxSpeed = 0x40046b04
)

type loraSPITransfer struct { // struct spi_ioc_transfer
	TxBuf       uint64
	RxBuf       uint64
	Len         uint32
	SpeedHz     uint32
	DelayUsecs  uint16
	BitsPerWord uint8
	CsChange    uint8
	TxNbits     uint8
	RxNbits     uint8
	WordDelay   uint8
	Pad         uint8
}

type loraSPI struct {
	f     *os.File
	path  string
	speed uint32
}

// openLoraSPI opens a spidev node in mode 0, 8 bits per word. The CC1101
// Caps use the same chip select, so the node is also locked (advisory
// flock) to keep two ZeroDesk radio backends off the bus at once.
func openLoraSPI(path string, speed uint32) (*loraSPI, error) {
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, syscall.EBUSY)
	}
	mode, bits := uint8(0), uint8(8)
	fd := f.Fd()
	if err := loraIoctl(fd, loraSPIIocWrMode, unsafe.Pointer(&mode)); err != nil {
		f.Close()
		return nil, fmt.Errorf("spi mode: %w", err)
	}
	if err := loraIoctl(fd, loraSPIIocWrBits, unsafe.Pointer(&bits)); err != nil {
		f.Close()
		return nil, fmt.Errorf("spi bits: %w", err)
	}
	if err := loraIoctl(fd, loraSPIIocWrMaxSpeed, unsafe.Pointer(&speed)); err != nil {
		f.Close()
		return nil, fmt.Errorf("spi speed: %w", err)
	}
	return &loraSPI{f: f, path: path, speed: speed}, nil
}

// transfer clocks tx out (chip select held for the whole buffer) and
// returns the bytes clocked in.
func (s *loraSPI) transfer(tx []byte) ([]byte, error) {
	n := len(tx)
	if n == 0 {
		return nil, nil
	}
	// variable-size make: both buffers live on the (non-moving) heap, so
	// their addresses stay valid inside the transfer struct
	out, rx := make([]byte, n), make([]byte, n)
	copy(out, tx)
	t := loraSPITransfer{
		TxBuf:       uint64(uintptr(unsafe.Pointer(&out[0]))),
		RxBuf:       uint64(uintptr(unsafe.Pointer(&rx[0]))),
		Len:         uint32(n),
		SpeedHz:     s.speed,
		BitsPerWord: 8,
	}
	err := loraIoctl(s.f.Fd(), loraSPIIocMessage1, unsafe.Pointer(&t))
	runtime.KeepAlive(out)
	runtime.KeepAlive(rx)
	return rx, err
}

func (s *loraSPI) close() {
	if s != nil && s.f != nil {
		s.f.Close()
		s.f = nil
	}
}

// ---- GPIO character device, v2 uAPI ----

const (
	loraGPIOGetLine      = 0xc250b407 // GPIO_V2_GET_LINE_IOCTL
	loraGPIOGetValues    = 0xc010b40e // GPIO_V2_LINE_GET_VALUES_IOCTL
	loraGPIOSetValues    = 0xc010b40f // GPIO_V2_LINE_SET_VALUES_IOCTL
	loraGPIOFlagInput    = 1 << 2
	loraGPIOFlagOutput   = 1 << 3
	loraGPIOAttrOutValue = 2
)

type loraGPIOLineAttribute struct {
	ID      uint32
	Padding uint32
	Value   uint64
}

type loraGPIOLineConfigAttribute struct {
	Attr loraGPIOLineAttribute
	Mask uint64
}

type loraGPIOLineConfig struct {
	Flags    uint64
	NumAttrs uint32
	Padding  [5]uint32
	Attrs    [10]loraGPIOLineConfigAttribute
}

type loraGPIOLineRequest struct {
	Offsets         [64]uint32
	Consumer        [32]byte
	Config          loraGPIOLineConfig
	NumLines        uint32
	EventBufferSize uint32
	Padding         [5]uint32
	Fd              int32
}

type loraGPIOLineValues struct {
	Bits uint64
	Mask uint64
}

// loraGPIOLines is one line request (several offsets, same direction).
type loraGPIOLines struct {
	fd int
	n  int
}

// requestLoraGPIO requests lines on a gpiochip as inputs, or as outputs
// driven to the bits of initial.
func requestLoraGPIO(chip string, offsets []int, output bool, initial uint64, consumer string) (*loraGPIOLines, error) {
	f, err := os.OpenFile(chip, os.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var req loraGPIOLineRequest
	for i, o := range offsets {
		req.Offsets[i] = uint32(o)
	}
	copy(req.Consumer[:len(req.Consumer)-1], consumer)
	req.NumLines = uint32(len(offsets))
	all := uint64(1)<<len(offsets) - 1
	if output {
		req.Config.Flags = loraGPIOFlagOutput
		req.Config.NumAttrs = 1
		req.Config.Attrs[0] = loraGPIOLineConfigAttribute{Attr: loraGPIOLineAttribute{ID: loraGPIOAttrOutValue, Value: initial & all}, Mask: all}
	} else {
		req.Config.Flags = loraGPIOFlagInput
	}
	if err := loraIoctl(f.Fd(), loraGPIOGetLine, unsafe.Pointer(&req)); err != nil {
		return nil, fmt.Errorf("%s lines %v: %w", chip, offsets, err)
	}
	return &loraGPIOLines{fd: int(req.Fd), n: len(offsets)}, nil
}

func (l *loraGPIOLines) get() (uint64, error) {
	v := loraGPIOLineValues{Mask: uint64(1)<<l.n - 1}
	err := loraIoctl(uintptr(l.fd), loraGPIOGetValues, unsafe.Pointer(&v))
	return v.Bits, err
}

func (l *loraGPIOLines) set(bits uint64) error {
	v := loraGPIOLineValues{Bits: bits, Mask: uint64(1)<<l.n - 1}
	return loraIoctl(uintptr(l.fd), loraGPIOSetValues, unsafe.Pointer(&v))
}

func (l *loraGPIOLines) close() {
	if l != nil && l.fd >= 0 {
		syscall.Close(l.fd)
		l.fd = -1
	}
}

// ---- i2c-dev ----

const loraI2CRdwr = 0x0707 // I2C_RDWR

type loraI2CMsg struct {
	Addr  uint16
	Flags uint16
	Len   uint16
	_     uint16
	Buf   uint64
}

type loraI2CRdwrData struct {
	Msgs  uint64
	Nmsgs uint32
	_     uint32
}

// loraI2C keeps its message buffers inside the (heap-allocated) struct so
// the addresses handed to the kernel cannot move.
type loraI2C struct {
	f    *os.File
	addr uint16
	buf  [4]byte
	msgs [2]loraI2CMsg
	data loraI2CRdwrData
}

func openLoraI2C(bus string, addr uint16) (*loraI2C, error) {
	f, err := os.OpenFile(bus, os.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return &loraI2C{f: f, addr: addr}, nil
}

func (c *loraI2C) xfer(n int) error {
	c.data = loraI2CRdwrData{Msgs: uint64(uintptr(unsafe.Pointer(&c.msgs[0]))), Nmsgs: uint32(n)}
	err := loraIoctl(c.f.Fd(), loraI2CRdwr, unsafe.Pointer(&c.data))
	runtime.KeepAlive(c)
	return err
}

func (c *loraI2C) readReg(reg uint8) (uint8, error) {
	c.buf[0], c.buf[1] = reg, 0
	c.msgs[0] = loraI2CMsg{Addr: c.addr, Len: 1, Buf: uint64(uintptr(unsafe.Pointer(&c.buf[0])))}
	c.msgs[1] = loraI2CMsg{Addr: c.addr, Flags: 1 /* I2C_M_RD */, Len: 1, Buf: uint64(uintptr(unsafe.Pointer(&c.buf[1])))}
	err := c.xfer(2)
	return c.buf[1], err
}

func (c *loraI2C) writeReg(reg, v uint8) error {
	c.buf[0], c.buf[1] = reg, v
	c.msgs[0] = loraI2CMsg{Addr: c.addr, Len: 2, Buf: uint64(uintptr(unsafe.Pointer(&c.buf[0])))}
	return c.xfer(1)
}

func (c *loraI2C) close() {
	if c != nil && c.f != nil {
		c.f.Close()
		c.f = nil
	}
}

// ---- serial port (termios) ----

// Linux (asm-generic, arm64) termios layout and constants, spelled out so
// the package still builds on the developer's Mac.
type loraTermios struct {
	Iflag, Oflag, Cflag, Lflag uint32
	Line                       uint8
	Cc                         [19]uint8
	Ispeed, Ospeed             uint32
}

const (
	loraTCGETS   = 0x5401
	loraTCSETS   = 0x5402
	loraTCFLSH   = 0x540b
	loraTIOCEXCL = 0x540c
	loraB115200  = 0x1002
)

type loraSerial struct {
	f    *os.File
	prev loraTermios
}

// openLoraSerial opens a UART read-only for exclusive use at 115200 8N1 raw.
// The file is non-blocking so reads go through Go's poller and honour
// deadlines.
func openLoraSerial(path string) (*loraSerial, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOCTTY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	s := &loraSerial{f: f}
	rc, err := f.SyscallConn()
	if err != nil {
		f.Close()
		return nil, err
	}
	var opErr error
	err = rc.Control(func(fd uintptr) {
		if e := syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
			opErr = fmt.Errorf("lock %s: %w", path, e)
			return
		}
		if e := loraIoctl(fd, loraTIOCEXCL, nil); e != nil {
			opErr = fmt.Errorf("exclusive %s: %w", path, e)
			return
		}
		if e := loraIoctl(fd, loraTCGETS, unsafe.Pointer(&s.prev)); e != nil {
			opErr = fmt.Errorf("tcgetattr %s: %w", path, e)
			return
		}
		t := loraRawTermios(s.prev)
		if e := loraIoctl(fd, loraTCSETS, unsafe.Pointer(&t)); e != nil {
			opErr = fmt.Errorf("configure %s for 115200 8N1: %w", path, e)
			return
		}
		_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, fd, loraTCFLSH, 0 /* TCIFLUSH */)
	})
	if err == nil {
		err = opErr
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return s, nil
}

// loraRawTermios is cfmakeraw + 115200 8N1, receiver on, no flow control,
// VMIN 1 / VTIME 0.
func loraRawTermios(t loraTermios) loraTermios {
	t.Iflag &^= 0x1 | 0x2 | 0x8 | 0x20 | 0x40 | 0x80 | 0x100 | 0x400 // IGNBRK BRKINT PARMRK ISTRIP INLCR IGNCR ICRNL IXON
	t.Oflag &^= 0x1                                                  // OPOST
	t.Lflag &^= 0x8 | 0x40 | 0x2 | 0x1 | 0x8000                      // ECHO ECHONL ICANON ISIG IEXTEN
	t.Cflag &^= 0x30 | 0x100 | 0x40 | 0x100f | 0x80000000            // CSIZE PARENB CSTOPB CBAUD CRTSCTS
	t.Cflag |= 0x30 | 0x80 | 0x800 | loraB115200                     // CS8 CREAD CLOCAL
	t.Ispeed, t.Ospeed = 115200, 115200
	t.Cc[6], t.Cc[5] = 1, 0 // VMIN, VTIME
	return t
}

func (s *loraSerial) close() {
	if s == nil || s.f == nil {
		return
	}
	if rc, err := s.f.SyscallConn(); err == nil {
		_ = rc.Control(func(fd uintptr) {
			_ = loraIoctl(fd, loraTCSETS, unsafe.Pointer(&s.prev))
			_ = syscall.Flock(int(fd), syscall.LOCK_UN)
		})
	}
	s.f.Close()
	s.f = nil
}

// loraConsoleOn reports whether the kernel console or a serial login uses
// the UART (the original GPS app refuses to open it then).
func loraConsoleOn(dev string) bool {
	name := filepath.Base(dev)
	if b, err := os.ReadFile("/proc/cmdline"); err == nil {
		s := string(b)
		if strings.Contains(s, "console="+name) || strings.Contains(s, "console=serial0") {
			return true
		}
	}
	if b, err := os.ReadFile("/proc/consoles"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if f := strings.Fields(line); len(f) > 0 && f[0] == name {
				return true
			}
		}
	}
	return false
}

// ---- the Cap's 5 V rail ----

// The Cap is powered from the CardputerZero's EXT 5V output (and the LoRa
// app also turns on Grove 5V), exposed as LED-class devices that the gpio
// group may write (udev rule from the Cap-LoRa-1262 package). The LoRa and
// GPS windows can run at the same time, so the original values are recorded
// once in a state file and restored only by the last ZeroDesk user. Each
// user holds a shared flock on a "use" file; changes happen under a short
// exclusive flock on a separate mutex file, and the releaser that can
// upgrade the use lock to exclusive is the last one.

const (
	loraExt5V   = "/sys/class/leds/ext_5v_out/brightness"
	loraGrove5V = "/sys/class/leds/grove_5v_out/brightness"
)

type loraCapPower struct {
	use *os.File
	dir string
}

func loraPowerDir() string {
	d := os.Getenv("XDG_RUNTIME_DIR")
	if d == "" {
		d = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return filepath.Join(d, "zerodesk")
}

func (p *loraCapPower) statePath() string { return filepath.Join(p.dir, "cap-lora-power.state") }

// loraPowerMutex takes the short-lived exclusive lock; closing the file
// releases it.
func loraPowerMutex(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "cap-lora-power.mutex"), os.O_RDWR|os.O_CREATE|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func readLoraPowerState(path string) map[string]string {
	m := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return m
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			m[k] = v
		}
	}
	return m
}

func writeLoraPowerState(path string, m map[string]string) error {
	var b strings.Builder
	for k, v := range m {
		b.WriteString(k + "=" + v + "\n")
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loraPowerOn turns the given rails on (recording their previous values).
func loraPowerOn(rails ...string) (*loraCapPower, error) {
	p := &loraCapPower{dir: loraPowerDir()}
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return nil, err
	}
	mu, err := loraPowerMutex(p.dir)
	if err != nil {
		return nil, err
	}
	defer mu.Close()
	if p.use, err = os.OpenFile(filepath.Join(p.dir, "cap-lora-power.lock"), os.O_RDWR|os.O_CREATE|syscall.O_CLOEXEC, 0o600); err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(p.use.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		p.use.Close()
		return nil, err
	}
	fail := func(err error) (*loraCapPower, error) {
		p.releaseLocked()
		return nil, err
	}
	saved := readLoraPowerState(p.statePath())
	for _, r := range rails {
		cur, err := sysRead(r)
		if err != nil {
			return fail(fmt.Errorf("Cap power: %w", err))
		}
		if _, ok := saved[r]; !ok {
			saved[r] = cur
			if err := writeLoraPowerState(p.statePath(), saved); err != nil {
				return fail(err)
			}
		}
		if n, _ := strconv.Atoi(cur); n <= 0 {
			if err := os.WriteFile(r, []byte("1"), 0); err != nil {
				return fail(fmt.Errorf("Cap power: %w", err))
			}
			if v, err := sysRead(r); err != nil || v == "0" {
				return fail(errors.New("Cap power: " + r + " did not turn on"))
			}
		}
	}
	return p, nil
}

// release drops this user; the last one restores the recorded values.
func (p *loraCapPower) release() {
	if p == nil || p.use == nil {
		return
	}
	if mu, err := loraPowerMutex(p.dir); err == nil {
		defer mu.Close()
	}
	p.releaseLocked()
}

func (p *loraCapPower) releaseLocked() {
	if syscall.Flock(int(p.use.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
		for r, v := range readLoraPowerState(p.statePath()) {
			_ = os.WriteFile(r, []byte(v), 0)
		}
		_ = os.Remove(p.statePath())
	}
	p.use.Close()
	p.use = nil
}
