package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Pairing: a browser asks to pair, then the person holding the device
// presses OK (Enter) on its keyboard. While waiting, ZeroDesk grabs the
// keyboard (EVIOCGRAB) so that press is not also delivered to the launcher
// or the foreground app. Someone on the same Wi-Fi without the device in
// hand cannot log in.

const (
	pairTimeout = 60 * time.Second
	keyEnter    = 28
	eviocgrab   = 0x40044590 // _IOW('E', 0x90, int)
)

type pairState struct {
	mu       sync.Mutex
	id       string
	code     string
	approved bool
	denied   bool
	// unavailable: the keyboard could not be grabbed, pairing cannot work
	unavailable bool
	expires     time.Time
	cancel      context.CancelFunc
}

func randomCode() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%04d", int(binary.BigEndian.Uint16(b))%10000)
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func findKeyboardNode(match string) (string, error) {
	nodes, _ := filepath.Glob("/sys/class/input/event*")
	for _, n := range nodes {
		b, err := os.ReadFile(filepath.Join(n, "device", "name"))
		if err == nil && strings.Contains(strings.ToLower(string(b)), match) {
			return "/dev/input/" + filepath.Base(n), nil
		}
	}
	return "", fmt.Errorf("no input device matching %q", match)
}

// waitForAnswer grabs the keyboard and returns true on Enter (OK), false on
// any other key, or (false, nil) when ctx ends. While it waits it keeps repainting the
// prompt, because the app underneath may redraw over it.
func waitForAnswer(ctx context.Context, match string, overlay *fbOverlay) (bool, error) {
	dev, err := findKeyboardNode(match)
	if err != nil {
		return false, err
	}
	f, err := os.OpenFile(dev, os.O_RDONLY, 0)
	if err != nil {
		return false, fmt.Errorf("open %s: %w", dev, err)
	}
	defer f.Close()
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgrab, 1); errno != 0 {
		return false, fmt.Errorf("grab keyboard: %w", errno)
	}
	defer syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgrab, 0) //nolint:errcheck

	done := make(chan bool, 1)
	go func() {
		buf := make([]byte, 24*16)
		for {
			n, err := f.Read(buf)
			if err != nil {
				done <- false
				return
			}
			for i := 0; i+24 <= n; i += 24 {
				typ := binary.LittleEndian.Uint16(buf[i+16:])
				code := binary.LittleEndian.Uint16(buf[i+18:])
				val := int32(binary.LittleEndian.Uint32(buf[i+20:]))
				// OK allows; any other key press denies
				if typ == evKey && val == 1 {
					done <- code == keyEnter
					return
				}
			}
		}
	}()
	repaint := time.NewTicker(150 * time.Millisecond)
	defer repaint.Stop()
	for {
		if overlay != nil {
			_ = overlay.paint()
		}
		select {
		case ok := <-done:
			return ok, nil
		case <-ctx.Done():
			f.Close() // unblocks the reader goroutine
			return false, nil
		case <-repaint.C:
		}
	}
}

func (s *server) handlePairStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Lang string `json:"lang"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body)
	lang := "en"
	if body.Lang == "zh" {
		lang = "zh"
	}

	p := &s.pair
	p.mu.Lock()
	// A new request replaces any pending one: one prompt on the device, one code.
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), pairTimeout)
	id, code := randomID(), randomCode()
	p.id, p.code, p.approved, p.denied, p.unavailable, p.expires, p.cancel = id, code, false, false, false, time.Now().Add(pairTimeout), cancel
	p.mu.Unlock()

	go s.runPairing(ctx, cancel, id, code, lang)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "code": code, "expiresIn": int(pairTimeout.Seconds())})
}

// pairLock serialises device prompts so a replaced request has fully
// restored the screen before the next one paints.
var pairLock sync.Mutex

func (s *server) runPairing(ctx context.Context, cancel context.CancelFunc, id, code, lang string) {
	defer cancel()
	pairLock.Lock()
	defer pairLock.Unlock()
	if ctx.Err() != nil {
		return
	}
	// Wake the LCD only if the launcher's screensaver blanked it. Apps keep
	// the screen on, so a blank screen means the launcher is in front, where
	// Shift does nothing; inside apps a key press could trigger something.
	if screenIsOff() {
		if err := s.keys.send(42, 1); err == nil {
			_ = s.keys.send(42, 0)
			time.Sleep(250 * time.Millisecond)
		}
	}
	overlay, err := newFBOverlay(s.fbPath, code, lang)
	if err != nil {
		log.Printf("pair: overlay: %v", err)
	}
	ok, err := waitForAnswer(ctx, s.kbdMatch, overlay)
	if overlay != nil {
		_ = overlay.restore()
	}
	if err != nil {
		log.Printf("pair: %v", err)
	}
	p := &s.pair
	p.mu.Lock()
	if p.id == id {
		p.approved = ok
		p.unavailable = err != nil
		p.denied = !ok && err == nil && ctx.Err() == nil
		if !ok {
			p.cancel = nil
		}
	}
	p.mu.Unlock()
	switch {
	case ok:
		log.Printf("pair: approved on device")
	case err == nil && ctx.Err() == nil:
		log.Printf("pair: denied on device")
	}
}

// handlePairStatus is polled by the browser; once approved it sets the
// session cookie exactly once.
func (s *server) handlePairStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	p := &s.pair
	p.mu.Lock()
	defer p.mu.Unlock()
	if id == "" || id != p.id {
		writeJSON(w, http.StatusOK, map[string]string{"state": "expired"})
		return
	}
	switch {
	case p.approved:
		p.id, p.approved, p.cancel = "", false, nil
		s.setSessionCookie(w)
		writeJSON(w, http.StatusOK, map[string]string{"state": "approved"})
	case p.denied:
		writeJSON(w, http.StatusOK, map[string]string{"state": "denied"})
	case p.unavailable:
		writeJSON(w, http.StatusOK, map[string]string{"state": "unavailable"})
	case p.cancel == nil || time.Now().After(p.expires):
		writeJSON(w, http.StatusOK, map[string]string{"state": "expired"})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"state": "waiting"})
	}
}

func (s *server) handlePairCancel(w http.ResponseWriter, r *http.Request) {
	p := &s.pair
	p.mu.Lock()
	if p.cancel != nil && r.URL.Query().Get("id") == p.id {
		p.cancel()
		p.cancel = nil
	}
	p.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) setSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    s.token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   60 * 60 * 24 * 365,
	})
}

func screenIsOff() bool {
	m, _ := filepath.Glob("/sys/class/backlight/*/brightness")
	for _, p := range m {
		if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) == "0" {
			return true
		}
	}
	return false
}
