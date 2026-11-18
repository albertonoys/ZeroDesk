package main

import (
	"syscall"
	"time"
	"unsafe"
)

// waitEdge waits up to d for an edge event on a requested GPIO line and
// drains the queue. It reports whether an event arrived.
func (g *gpioLine) waitEdge(d time.Duration) bool {
	type pollFd struct {
		fd             int32
		events, revent int16
	}
	pfd := pollFd{fd: int32(g.fd), events: 0x1}
	ts := syscall.NsecToTimespec(int64(d))
	n, _, e := syscall.Syscall6(syscall.SYS_PPOLL, uintptr(unsafe.Pointer(&pfd)), 1, uintptr(unsafe.Pointer(&ts)), 0, 0, 0)
	if e != 0 || n == 0 || pfd.revent&0x1 == 0 {
		return false
	}
	var buf [48 * 16]byte // struct gpio_v2_line_event is 48 bytes
	_, _ = syscall.Read(g.fd, buf[:])
	return true
}
