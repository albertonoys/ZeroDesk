package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// Low-level I/O for the Cap CC1101 accessory (CC1101 radio and ST25R3916 NFC
// reader): Linux spidev and the GPIO character device (v2 uAPI) through
// ioctls, without cgo. The user needs only the spi and gpio groups.

// ---- ioctl encoding (asm-generic) ----

func cc1101IOC(dir, typ, nr, size uintptr) uintptr {
	return dir<<30 | size<<16 | typ<<8 | nr
}

const (
	cc1101IOCWrite = 1
	cc1101IOCRead  = 2
)

func cc1101Ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	for {
		_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
		if e == syscall.EINTR {
			continue
		}
		if e != 0 {
			return e
		}
		return nil
	}
}

// ---- spidev ----

const (
	spiModeCPHA  = 0x01
	spiModeCPOL  = 0x02
	spiModeNoCS  = 0x40
	spiTransfer  = 32 // sizeof(struct spi_ioc_transfer)
	spiMaxChunk  = 4096
	spiIocMagic  = 'k'
	spiIocMode32 = 5
	spiIocBits   = 3
	spiIocSpeed  = 4
)

type spiIocTransfer struct {
	txBuf, rxBuf          uint64
	length, speedHz       uint32
	delayUsecs            uint16
	bitsPerWord, csChange uint8
	txNbits, rxNbits      uint8
	wordDelayUsecs, pad   uint8
}

type spiDev struct {
	f     *os.File
	speed uint32
}

func openSPI(path string, mode, speed uint32) (*spiDev, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	s := &spiDev{f: f, speed: speed}
	if err := s.setMode(mode); err != nil {
		f.Close()
		return nil, err
	}
	bits := uint8(8)
	if err := cc1101Ioctl(int(f.Fd()), cc1101IOC(cc1101IOCWrite, spiIocMagic, spiIocBits, 1), unsafe.Pointer(&bits)); err != nil {
		f.Close()
		return nil, fmt.Errorf("spi bits: %w", err)
	}
	if err := cc1101Ioctl(int(f.Fd()), cc1101IOC(cc1101IOCWrite, spiIocMagic, spiIocSpeed, 4), unsafe.Pointer(&speed)); err != nil {
		f.Close()
		return nil, fmt.Errorf("spi speed: %w", err)
	}
	return s, nil
}

func (s *spiDev) setMode(mode uint32) error {
	if err := cc1101Ioctl(int(s.f.Fd()), cc1101IOC(cc1101IOCWrite, spiIocMagic, spiIocMode32, 4), unsafe.Pointer(&mode)); err != nil {
		return fmt.Errorf("spi mode: %w", err)
	}
	return nil
}

// transfer clocks tx out and returns what was clocked in, in one message
// (chip select stays asserted for the whole buffer).
func (s *spiDev) transfer(tx []byte) ([]byte, error) {
	if len(tx) == 0 || len(tx) > spiMaxChunk {
		return nil, errors.New("spi: bad transfer size")
	}
	rx := make([]byte, len(tx))
	t := spiIocTransfer{
		txBuf:       uint64(uintptr(unsafe.Pointer(&tx[0]))),
		rxBuf:       uint64(uintptr(unsafe.Pointer(&rx[0]))),
		length:      uint32(len(tx)),
		speedHz:     s.speed,
		bitsPerWord: 8,
	}
	req := cc1101IOC(cc1101IOCWrite, spiIocMagic, 0, spiTransfer)
	if err := cc1101Ioctl(int(s.f.Fd()), req, unsafe.Pointer(&t)); err != nil {
		return nil, fmt.Errorf("spi transfer: %w", err)
	}
	return rx, nil
}

func (s *spiDev) close() {
	if s != nil && s.f != nil {
		s.f.Close()
		s.f = nil
	}
}

// ---- GPIO character device, v2 uAPI ----

const (
	gpioV2FlagInput       = 1 << 2
	gpioV2FlagOutput      = 1 << 3
	gpioV2FlagEdgeRising  = 1 << 4
	gpioV2FlagEdgeFalling = 1 << 5
	gpioV2FlagPullUp      = 1 << 8
	gpioV2FlagPullDown    = 1 << 9
	gpioV2AttrValues      = 2
	gpioIocMagic          = 0xB4
)

type gpioV2Attribute struct {
	id, pad uint32
	value   uint64
}

type gpioV2ConfigAttribute struct {
	attr gpioV2Attribute
	mask uint64
}

type gpioV2LineConfig struct {
	flags    uint64
	numAttrs uint32
	pad      [5]uint32
	attrs    [10]gpioV2ConfigAttribute
}

type gpioV2LineRequest struct {
	offsets         [64]uint32
	consumer        [32]byte
	config          gpioV2LineConfig
	numLines        uint32
	eventBufferSize uint32
	pad             [5]uint32
	fd              int32
}

type gpioV2LineValues struct {
	bits, mask uint64
}

// gpioLine is one requested line of /dev/gpiochipN.
type gpioLine struct {
	fd     int
	offset int
}

func requestGPIO(chip string, offset int, flags uint64, value bool, consumer string) (*gpioLine, error) {
	f, err := os.OpenFile(chip, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var req gpioV2LineRequest
	req.offsets[0] = uint32(offset)
	copy(req.consumer[:len(req.consumer)-1], consumer)
	req.numLines = 1
	req.config.flags = flags
	if flags&gpioV2FlagOutput != 0 {
		req.config.numAttrs = 1
		req.config.attrs[0].attr.id = gpioV2AttrValues
		if value {
			req.config.attrs[0].attr.value = 1
		}
		req.config.attrs[0].mask = 1
	}
	if flags&(gpioV2FlagEdgeRising|gpioV2FlagEdgeFalling) != 0 {
		req.eventBufferSize = 16
	}
	if err := cc1101Ioctl(int(f.Fd()), cc1101IOC(cc1101IOCRead|cc1101IOCWrite, gpioIocMagic, 0x07, unsafe.Sizeof(req)), unsafe.Pointer(&req)); err != nil {
		if errors.Is(err, syscall.EBUSY) {
			return nil, fmt.Errorf("GPIO%d is in use by another program", offset)
		}
		return nil, fmt.Errorf("GPIO%d: %w", offset, err)
	}
	return &gpioLine{fd: int(req.fd), offset: offset}, nil
}

func (g *gpioLine) set(v bool) error {
	lv := gpioV2LineValues{mask: 1}
	if v {
		lv.bits = 1
	}
	return cc1101Ioctl(g.fd, cc1101IOC(cc1101IOCRead|cc1101IOCWrite, gpioIocMagic, 0x0F, unsafe.Sizeof(lv)), unsafe.Pointer(&lv))
}

func (g *gpioLine) get() (bool, error) {
	lv := gpioV2LineValues{mask: 1}
	err := cc1101Ioctl(g.fd, cc1101IOC(cc1101IOCRead|cc1101IOCWrite, gpioIocMagic, 0x0E, unsafe.Sizeof(lv)), unsafe.Pointer(&lv))
	return lv.bits&1 != 0, err
}

func (g *gpioLine) close() {
	if g != nil && g.fd >= 0 {
		syscall.Close(g.fd)
		g.fd = -1
	}
}

// ---- Cap power and pin multiplexing ----

const (
	capLEDDir     = "/sys/class/leds"
	capExt5V      = "ext_5v_out"
	capUSBGPIOFun = "ext_usb_gpio_fun"
	capGPIOChip   = "/dev/gpiochip0"
	capPowerGPIO  = 26
)

func capLED(name string) (int, error) {
	s, err := sysRead(capLEDDir + "/" + name + "/brightness")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

func capSetLED(name string, on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	return os.WriteFile(capLEDDir+"/"+name+"/brightness", []byte(v), 0)
}

// capPower switches the Cap's supplies on the way the original apps do:
// HAT P0/P1 routed to GPIO (ext_usb_gpio_fun = 0), GPIO26 POWER_EN high and
// EXT5V on. Both LED-class files are writable by the gpio group. Like the
// originals, the supplies are left on afterwards: the Cap shares SPI with the
// LCD and cutting its I/O supply can freeze the panel. It returns the
// requested POWER_EN line, which the caller releases when done (the BSP keeps
// the output level).
func capPower() (*gpioLine, error) {
	if _, err := os.Stat(capLEDDir + "/" + capExt5V + "/brightness"); err != nil {
		return nil, errors.New("Cap power control (ext_5v_out) not found")
	}
	if v, err := capLED(capUSBGPIOFun); err == nil && v != 0 {
		if err := capSetLED(capUSBGPIOFun, false); err != nil {
			return nil, fmt.Errorf("HAT GPIO routing: %w", err)
		}
	}
	pwr, err := requestGPIO(capGPIOChip, capPowerGPIO, gpioV2FlagOutput, true, "cap-power")
	if err != nil {
		return nil, err
	}
	if v, err := capLED(capExt5V); err != nil || v == 0 {
		if err := capSetLED(capExt5V, true); err != nil {
			pwr.close()
			return nil, fmt.Errorf("EXT5V: %w", err)
		}
		// The CC1101 original waits 1.5 s, the NFC one 100 ms, after
		// switching the rail on.
		time.Sleep(300 * time.Millisecond)
	}
	return pwr, nil
}

// pinctrlGet returns the `pinctrl set` arguments that restore a pin's current
// function (pinctrl uses /dev/gpiomem, group gpio).
func pinctrlGet(pin int) ([]string, error) {
	out, err := cc1101Run("pinctrl", "get", strconv.Itoa(pin))
	if err != nil {
		return nil, err
	}
	return parsePinctrl(out, pin)
}

// parsePinctrl turns a `pinctrl get N` line such as "14: a5    -- | hi // ..."
// or "26: op dh pu | hi // ..." into the arguments of `pinctrl set`.
func parsePinctrl(out string, pin int) ([]string, error) {
	line := strings.TrimSpace(strings.SplitN(out, "//", 2)[0])
	f := strings.Fields(line)
	if len(f) < 3 || f[0] != strconv.Itoa(pin)+":" {
		return nil, fmt.Errorf("pinctrl: unexpected %q", line)
	}
	args := []string{"set", strconv.Itoa(pin), f[1]}
	i := 2
	if f[1] == "op" {
		lvl := f[2]
		if lvl == "--" {
			lvl = "dl"
			if len(f) > 5 && f[5] == "hi" {
				lvl = "dh"
			}
		}
		args = append(args, lvl)
		i = 3
	}
	if i < len(f) && (f[i] == "pu" || f[i] == "pd" || f[i] == "pn") {
		args = append(args, f[i])
	}
	return args, nil
}

func cc1101Run(name string, args ...string) (string, error) {
	path, err := lookPathSystem(name)
	if err != nil {
		return "", err
	}
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	p, err := os.StartProcess(path, append([]string{name}, args...), &os.ProcAttr{Files: []*os.File{nil, w, w}})
	w.Close()
	if err != nil {
		r.Close()
		return "", err
	}
	done := make(chan struct{})
	timer := time.AfterFunc(3*time.Second, func() { _ = p.Kill() })
	var out []byte
	go func() {
		buf := make([]byte, 512)
		for {
			n, err := r.Read(buf)
			out = append(out, buf[:n]...)
			if err != nil {
				break
			}
		}
		close(done)
	}()
	st, err := p.Wait()
	timer.Stop()
	<-done
	r.Close()
	if err != nil {
		return "", err
	}
	if !st.Success() {
		return string(out), fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func lookPathSystem(name string) (string, error) {
	for _, d := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		if st, err := os.Stat(d + "/" + name); err == nil && !st.IsDir() {
			return d + "/" + name, nil
		}
	}
	return "", fmt.Errorf("%s not found", name)
}

// ---- worker lifecycle ----

// cc1101Worker runs one hardware session at a time: start (first window)
// launches run once the previous session has released the hardware, halt
// (last window) asks it to finish. Commands wake the session's waits.
type cc1101Worker struct {
	mu     sync.Mutex
	want   bool
	done   chan struct{}
	stopCh chan struct{}
	stop   atomic.Bool
	cmd    chan string
	run    func()
}

func newCC1101Worker(run func()) *cc1101Worker {
	return &cc1101Worker{cmd: make(chan string, 8), run: run}
}

func (w *cc1101Worker) start() {
	w.mu.Lock()
	prev := w.done
	w.want = true
	w.mu.Unlock()
	go func() {
		if prev != nil {
			<-prev
		}
		w.mu.Lock()
		if !w.want || w.done != prev {
			w.mu.Unlock()
			return
		}
		done := make(chan struct{})
		w.done, w.stopCh = done, make(chan struct{})
		w.stop.Store(false)
		for len(w.cmd) > 0 {
			<-w.cmd
		}
		w.mu.Unlock()
		w.run()
		close(done)
	}()
}

func (w *cc1101Worker) halt() {
	w.mu.Lock()
	w.want = false
	w.stop.Store(true)
	if w.stopCh != nil {
		select {
		case <-w.stopCh:
		default:
			close(w.stopCh)
		}
	}
	w.mu.Unlock()
}

// join halts and waits for the running session to finish.
func (w *cc1101Worker) join() {
	w.halt()
	w.mu.Lock()
	d := w.done
	w.mu.Unlock()
	if d != nil {
		<-d
	}
}

func (w *cc1101Worker) send(c string) {
	select {
	case w.cmd <- c:
	default:
	}
}

// wait sleeps up to d and returns the command that woke it, "stop" when the
// session is halted, or "" on timeout.
func (w *cc1101Worker) wait(d time.Duration) string {
	w.mu.Lock()
	stop := w.stopCh
	w.mu.Unlock()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case c := <-w.cmd:
		return c
	case <-stop:
		return "stop"
	case <-t.C:
		return ""
	}
}
