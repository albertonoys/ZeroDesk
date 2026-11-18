package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Linux LIRC character devices (linux/lirc.h, rc-core) for the IR apps.
// Receiver and transmitter are picked by capability, not by node number, as
// CardputerZero/IR-Remote and IR-Chat do (on cp0: lirc0 = gpio-ir-tx on
// GPIO12, lirc1 = gpio_ir_recv on GPIO13). The nodes belong to group video.

const (
	lircModePulse = 0x2
	lircModeMode2 = 0x4

	lircCanSendPulse      = 0x2
	lircCanSetSendCarrier = 0x100
	lircCanSetSendDuty    = 0x200
	lircCanRecMode2       = 0x40000
	lircCanSetRecTimeout  = 0x10000000

	lircGetFeatures          = 0x80046900 // _IOR('i', 0x00, __u32)
	lircSetSendMode          = 0x40046911 // _IOW('i', 0x11, __u32)
	lircSetRecMode           = 0x40046912
	lircSetSendCarrier       = 0x40046913
	lircSetSendDutyCycle     = 0x40046915
	lircSetRecTimeout        = 0x40046918
	lircSetRecTimeoutReports = 0x40046919

	lircValueMask = 0x00ffffff
	lircSpace     = 0x00000000
	lircPulse     = 0x01000000
	lircTimeout   = 0x03000000
	lircOverflow  = 0x04000000

	irDefaultCarrier = 38000
	irDefaultDuty    = 33
)

// irPulse is one mark (Pulse) or gap.
type irPulse struct {
	Pulse bool
	Us    uint32
}

type lircDevice struct {
	Path     string `json:"path"`
	RC       string `json:"rc,omitempty"`
	Driver   string `json:"driver,omitempty"`
	Features uint32 `json:"features"`
}

func lircIoctl(fd uintptr, req uintptr, v *uint32) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(v))); e != 0 {
		return e
	}
	return nil
}

func lircSet(fd uintptr, req uintptr, v uint32) error { return lircIoctl(fd, req, &v) }

func lircFeatures(path string) (uint32, error) {
	var fd int
	var err error
	for _, mode := range []int{syscall.O_RDWR, syscall.O_RDONLY, syscall.O_WRONLY} {
		if fd, err = syscall.Open(path, mode|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0); err == nil {
			break
		}
	}
	if err != nil {
		return 0, err
	}
	defer syscall.Close(fd)
	var f uint32
	return f, lircIoctl(uintptr(fd), lircGetFeatures, &f)
}

// rcDriver mirrors the original: DRV_NAME from uevent, then drv_name, then name.
func rcDriver(rc string) string {
	if b, err := os.ReadFile(filepath.Join(rc, "uevent")); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(l, "DRV_NAME="); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	for _, f := range []string{"drv_name", "name"} {
		if s, err := sysRead(filepath.Join(rc, f)); err == nil && s != "" {
			return s
		}
	}
	return ""
}

// lircDevices lists every LIRC node with its rc sysfs path and features.
func lircDevices() ([]lircDevice, error) {
	var out []lircDevice
	seen := map[string]bool{}
	rcs, _ := filepath.Glob("/sys/class/rc/rc*")
	sort.Strings(rcs)
	var lastErr error
	add := func(path, rc string) {
		if seen[path] {
			return
		}
		seen[path] = true
		f, err := lircFeatures(path)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", path, err)
			return
		}
		d := lircDevice{Path: path, RC: rc, Features: f}
		if rc != "" {
			d.Driver = rcDriver(rc)
		}
		out = append(out, d)
	}
	for _, rc := range rcs {
		nodes, _ := filepath.Glob(rc + "/lirc*")
		sort.Strings(nodes)
		for _, n := range nodes {
			add("/dev/"+filepath.Base(n), rc)
		}
	}
	nodes, _ := filepath.Glob("/dev/lirc*")
	sort.Strings(nodes)
	for _, n := range nodes {
		add(n, "")
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

var (
	errNoReceiver    = errors.New("no-receiver")
	errNoTransmitter = errors.New("no-transmitter")
)

// lircFind returns the first node with the capability. env overrides the
// path (strict, like the original's IR_*_LIRC_*_DEVICE variables).
func lircFind(capability uint32, envs ...string) (lircDevice, error) {
	notFound := errNoReceiver
	if capability == lircCanSendPulse {
		notFound = errNoTransmitter
	}
	for _, e := range envs {
		if p := os.Getenv(e); p != "" {
			f, err := lircFeatures(p)
			if err != nil {
				return lircDevice{}, err
			}
			if f&capability == 0 {
				return lircDevice{}, notFound
			}
			return lircDevice{Path: p, Features: f}, nil
		}
	}
	devs, err := lircDevices()
	for _, d := range devs {
		if d.Features&capability != 0 {
			return d, nil
		}
	}
	if err != nil {
		return lircDevice{}, err
	}
	return lircDevice{}, notFound
}

// lircErrorKey maps open errors to keys the front ends translate.
func lircErrorKey(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errNoReceiver):
		return "no-receiver"
	case errors.Is(err, errNoTransmitter):
		return "no-transmitter"
	case errors.Is(err, os.ErrPermission), errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return "permission"
	case errors.Is(err, syscall.EBUSY):
		return "busy"
	}
	return "io"
}

// lircRX is an open receiver in MODE2.
type lircRX struct {
	dev lircDevice
	f   *os.File
}

func openLircRX(envs ...string) (*lircRX, error) {
	d, err := lircFind(lircCanRecMode2, envs...)
	if err != nil {
		return nil, err
	}
	// os.File registers the character device with the runtime poller, so
	// Read blocks without a thread and Close/deadlines interrupt it.
	f, err := os.OpenFile(d.Path, os.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	rx := &lircRX{dev: d, f: f}
	err = rx.control(func(fd uintptr) error {
		if err := lircSet(fd, lircSetRecMode, lircModeMode2); err != nil {
			return fmt.Errorf("LIRC_SET_REC_MODE: %w", err)
		}
		if d.Features&lircCanSetRecTimeout != 0 && lircSet(fd, lircSetRecTimeout, 100000) == nil {
			_ = lircSet(fd, lircSetRecTimeoutReports, 1)
		}
		return nil
	})
	if err != nil {
		f.Close()
		return nil, err
	}
	return rx, nil
}

func (r *lircRX) control(fn func(fd uintptr) error) error {
	rc, err := r.f.SyscallConn()
	if err != nil {
		return err
	}
	var ierr error
	if err := rc.Control(func(fd uintptr) { ierr = fn(fd) }); err != nil {
		return err
	}
	return ierr
}

// drain discards queued samples (the original does this before receiving
// so its own transmission is not read back).
func (r *lircRX) drain() {
	buf := make([]byte, 512)
	_ = r.f.SetReadDeadline(time.Now())
	for {
		if _, err := r.f.Read(buf); err != nil {
			break
		}
	}
	_ = r.f.SetReadDeadline(time.Time{})
}

// read returns raw MODE2 words; it blocks until data, the deadline or Close.
func (r *lircRX) read(buf []byte, deadline time.Time) ([]uint32, error) {
	_ = r.f.SetReadDeadline(deadline)
	n, err := r.f.Read(buf)
	if n%4 != 0 {
		return nil, errors.New("unaligned MODE2 read")
	}
	words := make([]uint32, n/4)
	for i := range words {
		words[i] = *(*uint32)(unsafe.Pointer(&buf[i*4]))
	}
	return words, err
}

func (r *lircRX) close() { r.f.Close() }

// lircTransmit sends pulse/space durations on the transmitter (opened per
// send and closed again, so nothing stays keyed). Same rules as the
// original: drop leading spaces, merge equal neighbours, end on a pulse.
func lircTransmit(pulses []irPulse, carrier, duty uint32, envs ...string) (lircDevice, int, error) {
	var vals []uint32
	last := false
	for _, p := range pulses {
		if p.Us == 0 || (len(vals) == 0 && !p.Pulse) {
			continue
		}
		if p.Us > lircValueMask {
			return lircDevice{}, 0, errors.New("duration")
		}
		if len(vals) > 0 && p.Pulse == last {
			if vals[len(vals)-1]+p.Us > lircValueMask {
				return lircDevice{}, 0, errors.New("duration")
			}
			vals[len(vals)-1] += p.Us
			continue
		}
		vals = append(vals, p.Us)
		last = p.Pulse
	}
	if len(vals)%2 == 0 && len(vals) > 0 {
		vals = vals[:len(vals)-1]
	}
	if len(vals) == 0 {
		return lircDevice{}, 0, errors.New("empty")
	}
	d, err := lircFind(lircCanSendPulse, envs...)
	if err != nil {
		return d, 0, err
	}
	fd, err := syscall.Open(d.Path, syscall.O_WRONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return d, 0, err
	}
	defer syscall.Close(fd)
	if err := lircSet(uintptr(fd), lircSetSendMode, lircModePulse); err != nil {
		return d, 0, fmt.Errorf("LIRC_SET_SEND_MODE: %w", err)
	}
	if d.Features&lircCanSetSendCarrier != 0 && carrier > 0 {
		_ = lircSet(uintptr(fd), lircSetSendCarrier, carrier)
	}
	if d.Features&lircCanSetSendDuty != 0 && duty > 0 && duty < 100 {
		_ = lircSet(uintptr(fd), lircSetSendDutyCycle, duty)
	}
	buf := make([]byte, len(vals)*4)
	for i, v := range vals {
		*(*uint32)(unsafe.Pointer(&buf[i*4])) = v
	}
	for {
		n, err := syscall.Write(fd, buf)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return d, 0, err
		}
		if n != len(buf) {
			return d, 0, fmt.Errorf("short write %d/%d", n, len(buf))
		}
		return d, len(vals), nil
	}
}

// mode2Sample splits a MODE2 word into a duration sample. kind: 'p' pulse,
// 's' space, 't' timeout, 'o' overflow, 0 other (frequency etc.).
func mode2Sample(w uint32) (kind byte, us uint32) {
	us = w & lircValueMask
	switch w &^ lircValueMask {
	case lircPulse:
		return 'p', us
	case lircSpace:
		return 's', us
	case lircTimeout:
		return 't', us
	case lircOverflow:
		return 'o', us
	}
	return 0, us
}
