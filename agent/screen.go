package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Frame wire format (binary WebSocket message), designed so the device does
// no compression work at all: it only copies the rows that changed and the
// browser on the computer does the decoding, scaling and drawing.
//
//	byte 0     : 2 = band of raw rows
//	bytes 1..2 : width  (uint16 LE)
//	bytes 3..4 : height (uint16 LE)
//	bytes 5..6 : first row y0 (uint16 LE)
//	bytes 7..8 : row count n  (uint16 LE)
//	byte 9     : reserved (keeps the pixel data 2-byte aligned for Uint16Array)
//	bytes 10.. : n*width RGB565 little-endian pixels
//
// The first frame a client receives is always the full screen.
const (
	frameBand   = 2
	frameHeader = 10
)

const (
	captureInterval = 66 * time.Millisecond  // ~15 fps while the picture changes
	boostInterval   = 33 * time.Millisecond  // ~30 fps right after input
	boostWindow     = 2 * time.Second        // how long input keeps the fast rate
	idleInterval    = 100 * time.Millisecond // slowest poll while nothing changes
)

type frame struct {
	seq  uint64
	data []byte // band message, shared read-only between clients
	full []byte // full-screen message for clients that are behind
}

// screenHub polls the framebuffer only while at least one client watches it,
// and publishes a frame only when the pixels changed.
type screenHub struct {
	fbPath string
	// fixed geometry for virtual framebuffers (files); nil = read from sysfs
	fixed *fbGeometry
	// kick wakes the capture loop immediately (sent after every key event)
	kick chan struct{}

	mu      sync.Mutex
	cond    *sync.Cond
	latest  frame
	clients int
	running bool
}

func newScreenHub(fbPath string) *screenHub {
	h := &screenHub{fbPath: fbPath, kick: make(chan struct{}, 1)}
	h.cond = sync.NewCond(&h.mu)
	return h
}

// newFileHub streams a virtual framebuffer file of known geometry.
func newFileHub(path string, geo fbGeometry) *screenHub {
	h := newScreenHub(path)
	h.fixed = &geo
	return h
}

// poke makes the next capture happen now and keeps a high frame rate for a
// moment, so a key press shows up on screen without waiting for the poll.
func (h *screenHub) poke() {
	select {
	case h.kick <- struct{}{}:
	default:
	}
}

type fbGeometry struct {
	width, height, stride, bpp int
}

func readSysInt(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

func (h *screenHub) geometry() (fbGeometry, error) {
	if h.fixed != nil {
		return *h.fixed, nil
	}
	name := strings.TrimPrefix(h.fbPath, "/dev/")
	base := "/sys/class/graphics/" + name + "/"
	b, err := os.ReadFile(base + "virtual_size")
	if err != nil {
		return fbGeometry{}, fmt.Errorf("read virtual_size: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(string(b)), ",")
	if len(parts) != 2 {
		return fbGeometry{}, fmt.Errorf("unexpected virtual_size %q", b)
	}
	w, _ := strconv.Atoi(parts[0])
	hgt, _ := strconv.Atoi(parts[1])
	stride, err := readSysInt(base + "stride")
	if err != nil {
		return fbGeometry{}, fmt.Errorf("read stride: %w", err)
	}
	bpp, err := readSysInt(base + "bits_per_pixel")
	if err != nil {
		return fbGeometry{}, fmt.Errorf("read bpp: %w", err)
	}
	if bpp != 16 {
		return fbGeometry{}, fmt.Errorf("unsupported bpp %d (only RGB565)", bpp)
	}
	return fbGeometry{w, hgt, stride, bpp}, nil
}

func (h *screenHub) subscribe() {
	h.mu.Lock()
	h.clients++
	start := !h.running
	h.running = true
	h.mu.Unlock()
	if start {
		go h.captureLoop()
	}
}

func (h *screenHub) unsubscribe() {
	h.mu.Lock()
	h.clients--
	h.mu.Unlock()
}

func (h *screenHub) captureLoop() {
	defer func() {
		h.mu.Lock()
		h.running = false
		h.cond.Broadcast()
		h.mu.Unlock()
	}()
	geo, err := h.geometry()
	if err != nil {
		log.Printf("screen: %v", err)
		return
	}
	f, err := os.Open(h.fbPath)
	if err != nil {
		log.Printf("screen: open fb: %v", err)
		return
	}
	defer f.Close()

	rowBytes := geo.width * 2
	raw := make([]byte, geo.stride*geo.height)
	cur := make([]byte, rowBytes*geo.height)
	prev := make([]byte, len(cur))
	var seq uint64
	interval := captureInterval
	var boostUntil time.Time
	timer := time.NewTimer(0)
	defer timer.Stop()

	encode := func(y0, n int) []byte {
		msg := make([]byte, frameHeader+n*rowBytes)
		msg[0] = frameBand
		binary.LittleEndian.PutUint16(msg[1:], uint16(geo.width))
		binary.LittleEndian.PutUint16(msg[3:], uint16(geo.height))
		binary.LittleEndian.PutUint16(msg[5:], uint16(y0))
		binary.LittleEndian.PutUint16(msg[7:], uint16(n))
		copy(msg[frameHeader:], cur[y0*rowBytes:(y0+n)*rowBytes])
		return msg
	}

	for {
		h.mu.Lock()
		if h.clients <= 0 {
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()

		if _, err := f.ReadAt(raw, 0); err != nil {
			log.Printf("screen: read fb: %v", err)
			return
		}
		top, bottom := -1, -1
		for y := 0; y < geo.height; y++ {
			src := raw[y*geo.stride : y*geo.stride+rowBytes]
			dst := cur[y*rowBytes : (y+1)*rowBytes]
			copy(dst, src)
			if seq == 0 || !bytes.Equal(dst, prev[y*rowBytes:(y+1)*rowBytes]) {
				if top < 0 {
					top = y
				}
				bottom = y
			}
		}
		if top >= 0 {
			copy(prev, cur)
			seq++
			fr := frame{seq: seq, data: encode(top, bottom-top+1), full: encode(0, geo.height)}
			h.mu.Lock()
			h.latest = fr
			h.cond.Broadcast()
			h.mu.Unlock()
			interval = captureInterval
		} else if interval < idleInterval {
			// back off gradually while the screen is static
			interval += 10 * time.Millisecond
		}
		if time.Now().Before(boostUntil) {
			interval = boostInterval
		}
		timer.Reset(interval)
		select {
		case <-timer.C:
		case <-h.kick:
			if !timer.Stop() {
				<-timer.C
			}
			boostUntil = time.Now().Add(boostWindow)
			// give the app a few ms to draw the result of the key press
			time.Sleep(8 * time.Millisecond)
		}
	}
}

// waitFrame blocks until a frame newer than seq exists, or ctx ends.
func (h *screenHub) waitFrame(ctx context.Context, seq uint64) (frame, bool) {
	stop := context.AfterFunc(ctx, func() {
		h.mu.Lock()
		h.cond.Broadcast()
		h.mu.Unlock()
	})
	defer stop()
	h.mu.Lock()
	defer h.mu.Unlock()
	for h.latest.seq <= seq {
		if ctx.Err() != nil || !h.running {
			return frame{}, false
		}
		h.cond.Wait()
	}
	return h.latest, true
}

type inputMsg struct {
	Type string `json:"t"` // "key"
	Code int    `json:"c"` // Linux KEY_* code
	Val  int    `json:"v"` // 1 down, 0 up, 2 repeat
}

func (s *server) handleScreenWS(w http.ResponseWriter, r *http.Request) {
	streamScreen(w, r, s.screen, s.keys.send)
}

// streamScreen serves one screen hub over a WebSocket: binary frames out,
// key events in. sendKey delivers a key to wherever this screen gets input.
func streamScreen(w http.ResponseWriter, r *http.Request, hub *screenHub, sendKey func(code, val int) error) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(4096)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	hub.subscribe()
	defer hub.unsubscribe()

	// reader: keyboard events from the browser
	go func() {
		defer cancel()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var m inputMsg
			if json.Unmarshal(data, &m) != nil || m.Type != "key" {
				continue
			}
			if err := sendKey(m.Code, m.Val); err != nil {
				log.Printf("input: %v", err)
				_ = c.Write(ctx, websocket.MessageText, []byte(`{"error":"input"}`))
				continue
			}
			hub.poke()
		}
	}()

	var seq uint64
	for {
		fr, ok := hub.waitFrame(ctx, seq)
		if !ok {
			// capture loop may have stopped on a transient error; retry once
			if ctx.Err() != nil {
				return
			}
			time.Sleep(time.Second)
			hub.subscribe()
			hub.unsubscribe()
			continue
		}
		msg := fr.data
		if seq == 0 || fr.seq != seq+1 {
			msg = fr.full // first frame, or frames were skipped
		}
		seq = fr.seq
		wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
		err := c.Write(wctx, websocket.MessageBinary, msg)
		wcancel()
		if err != nil {
			return
		}
	}
}
