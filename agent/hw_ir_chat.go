package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"
)

// IR Chat: short text messages over infrared. The frame format, timings,
// CRC and stream decoder are ported from CardputerZero/IR-Chat (MIT,
// src/radio/ir_chat_codec.cpp, linux_ir_backend.cpp, radio_worker.cpp) so a
// ZeroDesk window and the LCD app talk to each other: 38 kHz carrier, NEC
// style pulse-distance bits (9000/4500 leader, 560 mark, 560/1690 space),
// MSB first, frame "IR" v1 seq(2) len(1) payload(1–7 printable ASCII)
// CRC-16/CCITT-FALSE(2), then a 560 µs trailer mark. Messages live for the
// backend's session only (64 kept), like the original.

func init() { hwApps["ir-chat"] = hwIRChat }

const (
	irChatLeaderMark  = 9000
	irChatLeaderSpace = 4500
	irChatBitMark     = 560
	irChatZeroSpace   = 560
	irChatOneSpace    = 1690
	irChatVersion     = 1
	irChatMaxPayload  = 7
	irChatHeaderSize  = 6
	irChatHistory     = 64
)

func irChatCRC16(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func irChatPrintable(p []byte) bool {
	for _, c := range p {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

var errIRChatPayload = errors.New("payload")

// irChatEncode returns the alternating mark/space durations of one frame.
func irChatEncode(seq uint16, payload []byte) ([]uint32, error) {
	if len(payload) == 0 || len(payload) > irChatMaxPayload || !irChatPrintable(payload) {
		return nil, errIRChatPayload
	}
	frame := append([]byte{'I', 'R', irChatVersion, byte(seq >> 8), byte(seq), byte(len(payload))}, payload...)
	crc := irChatCRC16(frame)
	frame = append(frame, byte(crc>>8), byte(crc))
	d := make([]uint32, 0, 3+len(frame)*16)
	d = append(d, irChatLeaderMark, irChatLeaderSpace)
	for _, v := range frame {
		for bit := 7; bit >= 0; bit-- {
			d = append(d, irChatBitMark)
			if v&(1<<bit) != 0 {
				d = append(d, irChatOneSpace)
			} else {
				d = append(d, irChatZeroSpace)
			}
		}
	}
	return append(d, irChatBitMark), nil
}

func irChatPulses(d []uint32) []irPulse {
	p := make([]irPulse, len(d))
	for i, v := range d {
		p[i] = irPulse{Pulse: i%2 == 0, Us: v}
	}
	return p
}

type irChatFrame struct {
	Seq     uint16
	Payload []byte
	Samples int
}

type irChatStats struct {
	Decoded   uint64 `json:"decoded"`
	CRCErrors uint64 `json:"crc_errors"`
	Malformed uint64 `json:"malformed"`
	Noise     uint64 `json:"noise"`
}

// irChatDecoder is the original's streaming state machine.
type irChatDecoder struct {
	state    int // 0 leader, 1 leader space, 2 bit mark, 3 bit space, 4 trailer
	bytes    []byte
	cur      byte
	nbits    int
	expected int
	samples  int
	pending  *irChatFrame
	stats    irChatStats
}

func inRange(v, lo, hi uint32) bool { return v >= lo && v <= hi }

func (d *irChatDecoder) reset() {
	d.state, d.bytes, d.cur, d.nbits, d.expected, d.samples, d.pending = 0, d.bytes[:0], 0, 0, 0, 0, nil
}

func (d *irChatDecoder) begin() {
	d.reset()
	d.samples = 1
	d.state = 1
}

func (d *irChatDecoder) reject(crc bool) {
	if crc {
		d.stats.CRCErrors++
	} else {
		d.stats.Malformed++
	}
	d.reset()
}

func (d *irChatDecoder) restartFrom(s irPulse) {
	if s.Pulse && inRange(s.Us, 6300, 11700) {
		d.begin()
	}
}

func (d *irChatDecoder) feed(s irPulse) *irChatFrame {
	if s.Us == 0 {
		d.stats.Noise++
		return nil
	}
	switch d.state {
	case 0:
		if s.Pulse && inRange(s.Us, 6300, 11700) {
			d.begin()
		} else {
			d.stats.Noise++
		}
	case 1:
		d.samples++
		if !s.Pulse && inRange(s.Us, 3150, 5850) {
			d.state = 2
		} else {
			d.reject(false)
			d.restartFrom(s)
		}
	case 2:
		d.samples++
		if s.Pulse && inRange(s.Us, 300, 850) {
			d.state = 3
		} else {
			d.reject(false)
			d.restartFrom(s)
		}
	case 3:
		d.samples++
		if s.Pulse {
			d.reject(false)
			d.restartFrom(s)
			break
		}
		var one bool
		switch {
		case inRange(s.Us, 300, 950):
		case inRange(s.Us, 1100, 2400):
			one = true
		default:
			d.reject(false)
			return nil
		}
		if !d.consume(one) {
			break
		}
		if d.pending != nil {
			d.state = 4
		} else {
			d.state = 2
		}
	case 4:
		d.samples++
		if s.Pulse && inRange(s.Us, 300, 850) && d.pending != nil {
			f := d.pending
			f.Samples = d.samples
			d.stats.Decoded++
			d.reset()
			return f
		}
		d.reject(false)
		d.restartFrom(s)
	}
	return nil
}

func (d *irChatDecoder) consume(one bool) bool {
	d.cur <<= 1
	if one {
		d.cur |= 1
	}
	d.nbits++
	if d.nbits != 8 {
		return true
	}
	d.bytes = append(d.bytes, d.cur)
	d.cur, d.nbits = 0, 0
	n := len(d.bytes)
	if (n == 1 && d.bytes[0] != 'I') || (n == 2 && d.bytes[1] != 'R') || (n == 3 && d.bytes[2] != irChatVersion) {
		d.reject(false)
		return false
	}
	if n == irChatHeaderSize {
		size := int(d.bytes[5])
		if size == 0 || size > irChatMaxPayload {
			d.reject(false)
			return false
		}
		d.expected = irChatHeaderSize + size + 2
	}
	if d.expected == 0 || n < d.expected {
		return true
	}
	data := d.bytes[:n-2]
	if uint16(d.bytes[n-2])<<8|uint16(d.bytes[n-1]) != irChatCRC16(data) {
		d.reject(true)
		return false
	}
	payload := append([]byte(nil), data[irChatHeaderSize:]...)
	if !irChatPrintable(payload) {
		d.reject(false)
		return false
	}
	d.pending = &irChatFrame{Seq: uint16(d.bytes[3])<<8 | uint16(d.bytes[4]), Payload: payload}
	return true
}

// ---- the backend ----

type irChatMessage struct {
	ID       uint64 `json:"id"`
	Text     string `json:"text"`
	Outgoing bool   `json:"outgoing"`
	Status   string `json:"status,omitempty"` // sending, sent, failed (outgoing)
	Error    string `json:"error,omitempty"`
	Seq      uint16 `json:"seq"`
	Time     int64  `json:"time"` // unix ms
}

type irChatState struct {
	State    string          `json:"state"` // stopped, starting, receiving, sending, error
	Error    string          `json:"error,omitempty"`
	RX       *lircDevice     `json:"rx,omitempty"`
	TX       *lircDevice     `json:"tx,omitempty"`
	Carrier  uint32          `json:"carrier"`
	Protocol string          `json:"protocol"`
	RXCount  uint64          `json:"rx_count"`
	TXCount  uint64          `json:"tx_count"`
	Stats    irChatStats     `json:"stats"`
	Messages []irChatMessage `json:"messages"`
}

type irChatBackend struct {
	mu       sync.Mutex
	hub      *hwHub
	st       irChatState
	rx       *lircRX
	stop     chan struct{}
	sending  bool
	muteTill time.Time
	nextID   uint64
	txSeq    uint16
	sendMu   sync.Mutex
}

func hwIRChat(mux *http.ServeMux) func() {
	b := &irChatBackend{st: irChatState{State: "stopped", Carrier: irDefaultCarrier, Protocol: "IR Chat v1", Messages: []irChatMessage{}}}
	b.hub = newHWHub(b.start, b.halt)
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		hwJSON(w, http.StatusOK, b.snapshot())
	})
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		b.hub.serve(w, r, func() (string, any) { return "state", b.snapshot() })
	})
	mux.HandleFunc("POST /api/retry", func(w http.ResponseWriter, r *http.Request) {
		if b.hub.count() > 0 {
			b.halt()
			b.start()
		}
		hwJSON(w, http.StatusOK, b.snapshot())
	})
	mux.HandleFunc("POST /api/clear", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.st.Messages = []irChatMessage{}
		b.publishLocked()
		b.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/send", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Text string `json:"text"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			hwError(w, http.StatusBadRequest, "bad-request")
			return
		}
		payload := []byte(req.Text)
		if len(payload) == 0 {
			hwError(w, http.StatusBadRequest, "empty")
			return
		}
		if len(payload) > irChatMaxPayload || !irChatPrintable(payload) {
			hwError(w, http.StatusBadRequest, "too-long")
			return
		}
		m, err := b.send(payload)
		if err != "" {
			hwError(w, http.StatusServiceUnavailable, err)
			return
		}
		hwJSON(w, http.StatusOK, m)
	})
	return b.halt
}

func (b *irChatBackend) snapshot() irChatState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.copyLocked()
}

func (b *irChatBackend) copyLocked() irChatState {
	s := b.st
	s.Messages = append(make([]irChatMessage, 0, len(b.st.Messages)), b.st.Messages...)
	return s
}

func (b *irChatBackend) publishLocked() { b.hub.publish("state", b.copyLocked()) }

func (b *irChatBackend) appendLocked(m irChatMessage) irChatMessage {
	b.nextID++
	m.ID = b.nextID
	m.Time = time.Now().UnixMilli()
	b.st.Messages = append(b.st.Messages, m)
	if n := len(b.st.Messages); n > irChatHistory {
		b.st.Messages = append([]irChatMessage(nil), b.st.Messages[n-irChatHistory:]...)
	}
	return m
}

// start opens the receiver and finds the transmitter (first window).
func (b *irChatBackend) start() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stop != nil {
		return
	}
	b.st.State, b.st.Error, b.st.RX, b.st.TX = "starting", "", nil, nil
	tx, err := lircFind(lircCanSendPulse, "IR_CHAT_LIRC_TX_DEVICE")
	if err != nil {
		b.st.State, b.st.Error = "error", lircErrorKey(err)
		b.publishLocked()
		return
	}
	rx, err := openLircRX("IR_CHAT_LIRC_RX_DEVICE")
	if err != nil {
		b.st.State, b.st.Error = "error", lircErrorKey(err)
		b.publishLocked()
		return
	}
	rx.drain()
	b.rx = rx
	b.st.RX, b.st.TX = &rx.dev, &tx
	b.st.State = "receiving"
	stop := make(chan struct{})
	b.stop = stop
	b.publishLocked()
	go b.receive(rx, stop)
}

func (b *irChatBackend) halt() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stop != nil {
		close(b.stop)
		b.stop = nil
	}
	if b.rx != nil {
		b.rx.close()
		b.rx = nil
	}
	if b.st.State != "error" {
		b.st.State = "stopped"
	}
}

func (b *irChatBackend) receive(rx *lircRX, stop chan struct{}) {
	var dec irChatDecoder
	buf := make([]byte, 512)
	for {
		words, err := rx.read(buf, time.Time{})
		b.mu.Lock()
		select {
		case <-stop:
			b.mu.Unlock()
			return
		default:
		}
		// our own transmission (and its tail) is not read back, as in the
		// original, which stops receiving while it sends
		mute := b.sending || time.Now().Before(b.muteTill)
		for _, w := range words {
			if mute {
				dec.reset()
				continue
			}
			kind, us := mode2Sample(w)
			switch kind {
			case 'p', 's':
				if f := dec.feed(irPulse{Pulse: kind == 'p', Us: us}); f != nil {
					b.st.RXCount++
					b.appendLocked(irChatMessage{Text: string(f.Payload), Seq: f.Seq})
					b.publishLocked()
				}
			case 't', 'o':
				dec.reset()
			}
		}
		if b.st.Stats != dec.stats {
			b.st.Stats = dec.stats
		}
		if err != nil && !os.IsTimeout(err) {
			b.st.State, b.st.Error = "error", "io"
			b.publishLocked()
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()
	}
}

func (b *irChatBackend) send(payload []byte) (irChatMessage, string) {
	b.sendMu.Lock()
	defer b.sendMu.Unlock()
	b.mu.Lock()
	if b.stop == nil || b.st.State == "error" {
		b.mu.Unlock()
		return irChatMessage{}, "not-ready"
	}
	b.txSeq++
	seq := b.txSeq
	m := b.appendLocked(irChatMessage{Text: string(payload), Outgoing: true, Status: "sending", Seq: seq})
	b.sending = true
	b.st.State = "sending"
	b.publishLocked()
	b.mu.Unlock()

	d, _ := irChatEncode(seq, payload)
	_, _, err := lircTransmit(irChatPulses(d), irDefaultCarrier, irDefaultDuty, "IR_CHAT_LIRC_TX_DEVICE")

	b.mu.Lock()
	defer b.mu.Unlock()
	b.sending = false
	b.muteTill = time.Now().Add(150 * time.Millisecond)
	if b.stop != nil {
		b.st.State = "receiving"
	}
	for i := range b.st.Messages {
		if b.st.Messages[i].ID == m.ID {
			if err != nil {
				b.st.Messages[i].Status, b.st.Messages[i].Error = "failed", lircErrorKey(err)
			} else {
				b.st.Messages[i].Status = "sent"
				b.st.TXCount++
			}
			m = b.st.Messages[i]
		}
	}
	b.publishLocked()
	return m, ""
}
