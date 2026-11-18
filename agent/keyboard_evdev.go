package main

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"os"
	"syscall"
	"unsafe"
)

// evdev and SMBus helpers for the Keyboard Guide backend (no cgo, ioctls via
// syscall). Shared-helper candidates: other backends may carry similar ones.

const (
	kbEvSyn   = 0x00
	kbEvKey   = 0x01
	kbEvMsc   = 0x04
	kbMscScan = 0x04
	kbKeyMax  = 0x2ff

	kbIOCRead       = 2
	kbI2CSlaveForce = 0x0706
	kbI2CSMBus      = 0x0720
	kbSMBusRead     = 1
	kbSMBusByteData = 2
)

func kbIOC(dir, typ, nr, size uintptr) uintptr {
	return dir<<30 | size<<16 | typ<<8 | nr
}

// evdev ioctls (linux/input.h)
func kbEVIOCGNAME(n int) uintptr    { return kbIOC(kbIOCRead, 'E', 0x06, uintptr(n)) }
func kbEVIOCGKEY(n int) uintptr     { return kbIOC(kbIOCRead, 'E', 0x18, uintptr(n)) }
func kbEVIOCGBIT(ev, n int) uintptr { return kbIOC(kbIOCRead, 'E', 0x20+uintptr(ev), uintptr(n)) }

// kbIoctl runs an ioctl through SyscallConn so the file keeps its
// non-blocking, poller-backed mode (Close then unblocks a pending Read).
func kbIoctl(f *os.File, req uintptr, buf []byte) (int, error) {
	rc, err := f.SyscallConn()
	if err != nil {
		return 0, err
	}
	var r uintptr
	var errno syscall.Errno
	if cerr := rc.Control(func(fd uintptr) {
		r, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(&buf[0])))
	}); cerr != nil {
		return 0, cerr
	}
	if errno != 0 {
		return 0, errno
	}
	return int(r), nil
}

func kbDeviceName(f *os.File) string {
	buf := make([]byte, 256)
	n, err := kbIoctl(f, kbEVIOCGNAME(len(buf)), buf)
	if err != nil || n <= 0 {
		return ""
	}
	if n > len(buf) {
		n = len(buf)
	}
	for i, c := range buf[:n] {
		if c == 0 {
			return string(buf[:i])
		}
	}
	return string(buf[:n])
}

// kbBits reads an EVIOCGBIT / EVIOCGKEY bitmap.
func kbBits(f *os.File, req func(int) uintptr, nbits int) ([]byte, error) {
	buf := make([]byte, (nbits+7)/8)
	if _, err := kbIoctl(f, req(len(buf)), buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func kbBitSet(b []byte, n int) bool {
	return n >= 0 && n/8 < len(b) && b[n/8]&(1<<(n%8)) != 0
}

func kbBitCount(b []byte) int {
	n := 0
	for _, x := range b {
		n += bits.OnesCount8(x)
	}
	return n
}

func kbBitList(b []byte) []uint16 {
	var out []uint16
	for i := range len(b) * 8 {
		if kbBitSet(b, i) {
			out = append(out, uint16(i))
		}
	}
	return out
}

// struct input_event: struct timeval + __u16 type + __u16 code + __s32 value
var kbEventSize = int(unsafe.Sizeof(syscall.Timeval{})) + 8

type kbInputEvent struct {
	Type, Code uint16
	Value      int32
}

func kbDecodeEvents(buf []byte, size int) []kbInputEvent {
	var out []kbInputEvent
	for i := 0; i+size <= len(buf); i += size {
		e := buf[i+size-8 : i+size]
		out = append(out, kbInputEvent{
			Type:  binary.LittleEndian.Uint16(e[0:]),
			Code:  binary.LittleEndian.Uint16(e[2:]),
			Value: int32(binary.LittleEndian.Uint32(e[4:])),
		})
	}
	return out
}

// ---- SMBus byte reads (i2c-dev) ----

type kbSMBusReq struct {
	readWrite uint8
	command   uint8
	size      uint32
	data      unsafe.Pointer // union i2c_smbus_data *
}

type kbI2C struct {
	f *os.File
}

func kbOpenI2C(dev string, addr uint8) (*kbI2C, error) {
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	// The address belongs to the m5ioe1 expander driver; like the original
	// app, only byte reads are issued, so forcing the address is safe.
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), kbI2CSlaveForce, uintptr(addr)); errno != 0 {
		f.Close()
		return nil, fmt.Errorf("I2C_SLAVE_FORCE 0x%02x: %w", addr, errno)
	}
	return &kbI2C{f: f}, nil
}

func (c *kbI2C) readByte(reg uint8) (uint8, error) {
	var data [34]byte
	req := kbSMBusReq{readWrite: kbSMBusRead, command: reg, size: kbSMBusByteData, data: unsafe.Pointer(&data[0])}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, c.f.Fd(), kbI2CSMBus, uintptr(unsafe.Pointer(&req))); errno != 0 {
		return 0, errno
	}
	return data[0], nil
}

func (c *kbI2C) close() {
	if c != nil && c.f != nil {
		c.f.Close()
	}
}
