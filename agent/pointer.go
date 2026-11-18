package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Pointer input for CardputerZero apps. The device has no touch screen, but
// cp0_lvgl (the UI layer the launcher and the official apps share) creates an
// LVGL pointer device when LV_LINUX_MOUSE_DEVICE names an evdev source. We
// point it at a FIFO and write absolute touch events into it, so a click or a
// drag in the browser reaches the app as a touch on its screen.
//
// No EVIOCGABS works on a FIFO, so LVGL leaves coordinates uncalibrated and
// takes them as screen pixels (0..319, 0..169).

const (
	evAbs    = 0x03
	absX     = 0x00
	absY     = 0x01
	btnLeft  = 0x110
	btnTouch = 0x14a
)

type pointerSink struct {
	mu   sync.Mutex
	path string
	f    *os.File
	down bool
	w, h int
}

// newPointerSink creates (if needed) and opens the FIFO at path.
// O_RDWR so opening never blocks; O_NONBLOCK so a reader that is not reading
// (an app that ignores the pointer) can never stall the agent.
func newPointerSink(path string, w, h int) (*pointerSink, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if st, err := os.Stat(path); err != nil || st.Mode()&os.ModeNamedPipe == 0 {
		_ = os.Remove(path)
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			return nil, fmt.Errorf("pointer pipe: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("pointer pipe: %w", err)
	}
	return &pointerSink{path: path, f: f, w: w, h: h}, nil
}

// send moves the pointer to (x, y) in screen pixels, pressed or not.
func (p *pointerSink) send(x, y int, down bool) error {
	if p == nil {
		return errors.New("no pointer")
	}
	x = max(0, min(p.w-1, x))
	y = max(0, min(p.h-1, y))
	p.mu.Lock()
	defer p.mu.Unlock()
	if !down && !p.down {
		return nil // hover: LVGL only needs the pointer while it is pressed
	}
	now := time.Now()
	buf := make([]byte, 0, 6*24)
	ev := func(typ, code uint16, val int32) {
		b := make([]byte, 24)
		encodeEvent(b, now, typ, code, val)
		buf = append(buf, b...)
	}
	ev(evAbs, absX, int32(x))
	ev(evAbs, absY, int32(y))
	if down != p.down {
		v := int32(0)
		if down {
			v = 1
		}
		ev(evKey, btnTouch, v)
		ev(evKey, btnLeft, v)
	}
	ev(evSyn, synReport, 0)
	p.down = down
	_, err := p.f.Write(buf)
	if errors.Is(err, syscall.EAGAIN) {
		return nil // nobody is reading; drop it
	}
	return err
}

func (p *pointerSink) close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.f != nil {
		p.f.Close()
		p.f = nil
	}
}
