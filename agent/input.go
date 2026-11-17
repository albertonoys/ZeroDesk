package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	evSyn     = 0x00
	evKey     = 0x01
	synReport = 0
	maxKey    = 0x2ff
)

// keyInjector writes key events straight into the internal keyboard's evdev
// node. Every process that has that node open (the launcher and the
// foreground app) receives them exactly as if the physical key was pressed,
// so no app needs to know about ZeroDesk.
type keyInjector struct {
	match string

	mu   sync.Mutex
	f    *os.File
	name string
}

func newKeyInjector(match string) *keyInjector {
	return &keyInjector{match: strings.ToLower(match)}
}

func (k *keyInjector) open() error {
	if k.f != nil {
		return nil
	}
	nodes, _ := filepath.Glob("/sys/class/input/event*")
	for _, n := range nodes {
		b, err := os.ReadFile(filepath.Join(n, "device", "name"))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(b))
		if !strings.Contains(strings.ToLower(name), k.match) {
			continue
		}
		dev := "/dev/input/" + filepath.Base(n)
		f, err := os.OpenFile(dev, os.O_WRONLY, 0)
		if err != nil {
			return fmt.Errorf("open %s: %w", dev, err)
		}
		k.f, k.name = f, name
		return nil
	}
	return fmt.Errorf("no input device matching %q", k.match)
}

func (k *keyInjector) deviceName() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.open(); err != nil {
		return ""
	}
	return k.name
}

// struct input_event on 64-bit Linux: timeval(16) + type(2) + code(2) + value(4)
func encodeEvent(buf []byte, now time.Time, typ, code uint16, val int32) {
	binary.LittleEndian.PutUint64(buf[0:], uint64(now.Unix()))
	binary.LittleEndian.PutUint64(buf[8:], uint64(now.Nanosecond()/1000))
	binary.LittleEndian.PutUint16(buf[16:], typ)
	binary.LittleEndian.PutUint16(buf[18:], code)
	binary.LittleEndian.PutUint32(buf[20:], uint32(val))
}

func (k *keyInjector) send(code, val int) error {
	if code <= 0 || code > maxKey || val < 0 || val > 2 {
		return fmt.Errorf("invalid key event code=%d val=%d", code, val)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.open(); err != nil {
		return err
	}
	buf := make([]byte, 48)
	now := time.Now()
	encodeEvent(buf[0:24], now, evKey, uint16(code), int32(val))
	encodeEvent(buf[24:48], now, evSyn, synReport, 0)
	if _, err := k.f.Write(buf); err != nil {
		k.f.Close()
		k.f = nil
		return fmt.Errorf("write key event: %w", err)
	}
	return nil
}
