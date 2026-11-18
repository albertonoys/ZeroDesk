package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SubG Chat: text chat over the Cap CC1101's 868 MHz 2-FSK link, compatible
// with CardputerZero/Cap-CC1101-SubG-Chat (MIT): same radio profile and
// frames (cc1101_radio.go), the same acknowledgement / retry / duplicate
// handling (src/radio/radio_worker.cpp), the same nickname file
// (~/.config/M5CardputerZero-Cap-CC1101-SubG-Chat/nickname) and, like the
// original, the last 64 messages kept for the session only.

const (
	chatHistoryMax    = 64
	chatAttempts      = 5
	chatAckTimeout    = 1800 * time.Millisecond
	chatAckTurnaround = 240 * time.Millisecond
	chatRxSlice       = 40 * time.Millisecond
	chatRecentTokens  = 64
	chatSPISpeed      = 500_000
	chatConfigDir     = "M5CardputerZero-Cap-CC1101-SubG-Chat"
)

type chatMessage struct {
	ID       int64   `json:"id"`
	Text     string  `json:"text"`
	Sender   string  `json:"sender,omitempty"`
	Outgoing bool    `json:"outgoing"`
	Time     int64   `json:"time"`
	RSSI     float64 `json:"rssi,omitempty"`
	LQI      int     `json:"lqi,omitempty"`
	Status   string  `json:"status"` // received, sending, delivered, failed
	Legacy   bool    `json:"legacy,omitempty"`
	Error    string  `json:"error,omitempty"`
}

type chatRadio struct {
	State     string  `json:"state"` // off, starting, listening, sending, absent, busy, error
	Detail    string  `json:"detail,omitempty"`
	Version   *int    `json:"version,omitempty"`
	PartNum   *int    `json:"partnum,omitempty"`
	Frequency float64 `json:"frequency"`
	Bitrate   float64 `json:"bitrate"`
	Deviation float64 `json:"deviation"`
	Bandwidth float64 `json:"bandwidth"`
	Power     int     `json:"power"`
	Sync      string  `json:"sync"`
	Bus       string  `json:"bus"`
	Received  int     `json:"received"`
	CRCErrors int     `json:"crc_errors"`
}

type chatBackend struct {
	mu       sync.Mutex
	hub      *hwHub
	w        *cc1101Worker
	radio    chatRadio
	nickname string
	messages []*chatMessage
	outbox   []int64
	nextID   int64
	token    uint32
	recent   []uint32
}

func chatNicknamePath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return filepath.Join(home, ".config", chatConfigDir, "nickname")
}

func chatValidName(s string) bool {
	return s != "" && len(s) <= chatMaxName && strings.TrimSpace(s) != "" && chatPrintable(s)
}

func chatLoadNickname() string {
	if b, err := os.ReadFile(chatNicknamePath()); err == nil {
		if n := strings.SplitN(string(b), "\n", 2)[0]; chatValidName(n) {
			return n
		}
	}
	name := "CC1101"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	return name[:min(len(name), chatMaxName)]
}

func chatSaveNickname(name string) error {
	p := chatNicknamePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "nickname.tmp.*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(name); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	_ = os.Chmod(tmp.Name(), 0o644)
	return os.Rename(tmp.Name(), p)
}

func chatRandomToken() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	t := binary.BigEndian.Uint32(b[:]) & 0xFFFFFF
	if t == 0 {
		t = 1
	}
	return t
}

func hwCC1101Chat(mux *http.ServeMux) func() {
	b := &chatBackend{nickname: chatLoadNickname(), token: chatRandomToken(), radio: chatRadio{
		State: "off", Frequency: ccFreqMHz, Bitrate: ccBitrate, Deviation: ccDeviation, Bandwidth: ccBandwidth,
		Power: ccPowerDBm, Sync: "0x12AD", Bus: capSPIDev,
	}}
	b.w = newCC1101Worker(b.run)
	b.hub = newHWHub(b.w.start, b.w.halt)
	snapshot := func() any {
		b.mu.Lock()
		defer b.mu.Unlock()
		msgs := make([]chatMessage, len(b.messages))
		for i, m := range b.messages {
			msgs[i] = *m
		}
		return map[string]any{"radio": b.radio, "nickname": b.nickname, "messages": msgs}
	}
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		hwJSON(w, http.StatusOK, snapshot())
	})
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		b.hub.serve(w, r, func() (string, any) { return "snapshot", snapshot() })
	})
	mux.HandleFunc("POST /api/send", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			hwError(w, http.StatusBadRequest, "bad request")
			return
		}
		if req.Text == "" || len(req.Text) > chatMaxMessage || !chatPrintable(req.Text) || strings.TrimSpace(req.Text) == "" {
			hwError(w, http.StatusBadRequest, "message must be 1 to 56 printable ASCII characters")
			return
		}
		b.mu.Lock()
		st := b.radio.State
		if st != "listening" && st != "sending" {
			b.mu.Unlock()
			hwError(w, http.StatusConflict, "radio not ready")
			return
		}
		b.nextID++
		m := &chatMessage{ID: b.nextID, Text: req.Text, Outgoing: true, Time: time.Now().UnixMilli(), Status: "sending"}
		b.appendLocked(m)
		b.outbox = append(b.outbox, m.ID)
		b.mu.Unlock()
		b.hub.publish("message", *m)
		b.w.send("send")
		hwJSON(w, http.StatusAccepted, map[string]int64{"id": m.ID})
	})
	mux.HandleFunc("POST /api/nickname", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || !chatValidName(req.Name) {
			hwError(w, http.StatusBadRequest, "nickname must be 1 to 10 printable ASCII characters")
			return
		}
		if err := chatSaveNickname(req.Name); err != nil {
			hwError(w, http.StatusInternalServerError, err.Error())
			return
		}
		b.mu.Lock()
		b.nickname = req.Name
		b.mu.Unlock()
		b.hub.publish("nickname", req.Name)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/retry", func(w http.ResponseWriter, r *http.Request) {
		b.w.send("retry")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/clear", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		kept := []*chatMessage{}
		for _, m := range b.messages {
			if m.Status == "sending" {
				kept = append(kept, m)
			}
		}
		b.messages = kept
		b.mu.Unlock()
		b.hub.publish("messages", snapshot().(map[string]any)["messages"])
		w.WriteHeader(http.StatusNoContent)
	})
	return b.w.join
}

func (b *chatBackend) appendLocked(m *chatMessage) {
	b.messages = append(b.messages, m)
	if len(b.messages) > chatHistoryMax {
		b.messages = b.messages[len(b.messages)-chatHistoryMax:]
	}
}

func (b *chatBackend) setRadio(fn func(r *chatRadio)) {
	b.mu.Lock()
	fn(&b.radio)
	r := b.radio
	b.mu.Unlock()
	b.hub.publish("radio", r)
}

func (b *chatBackend) update(id int64, fn func(m *chatMessage)) {
	b.mu.Lock()
	var out *chatMessage
	for _, m := range b.messages {
		if m.ID == id {
			fn(m)
			c := *m
			out = &c
		}
	}
	b.mu.Unlock()
	if out != nil {
		b.hub.publish("message", *out)
	}
}

// ---- hardware session ----

type chatHW struct {
	lock     *os.File
	power    *gpioLine
	spi      *spiDev
	rfsw, cs *gpioLine
	restore  [][]string
	radio    *ccRadio
}

func openChatHW(b *chatBackend) (*chatHW, error) {
	h := &chatHW{}
	var err error
	if h.lock, err = capLock(); err != nil {
		return nil, err
	}
	if h.power, err = capPower(); err != nil {
		h.close()
		return nil, err
	}
	for _, pin := range []int{nfcCSGPIO, ccRFSwitch} {
		if args, err := pinctrlGet(pin); err == nil {
			h.restore = append(h.restore, args)
		} else {
			log.Printf("chat: %v", err)
		}
	}
	// keep the ST25R3916 deselected while the radio owns the bus
	if h.cs, err = requestGPIO(capGPIOChip, nfcCSGPIO, gpioV2FlagOutput, true, "st25r3916-cs"); err != nil {
		h.close()
		return nil, err
	}
	if h.spi, err = openSPI(capSPIDev, 0, chatSPISpeed); err != nil {
		h.close()
		return nil, err
	}
	h.radio = &ccRadio{spi: h.spi, stop: &b.w.stop}
	return h, nil
}

// attachRF takes RF_SW0 (GPIO14, the mini UART's TX pin by default) once a
// CC1101 has answered, and selects the 868 MHz antenna path.
func (h *chatHW) attachRF() error {
	var err error
	if h.rfsw, err = requestGPIO(capGPIOChip, ccRFSwitch, gpioV2FlagOutput, true, "cc1101-rf-sw0"); err != nil {
		return err
	}
	h.radio.rfsw = h.rfsw
	return h.radio.antenna()
}

func (h *chatHW) close() {
	if h.radio != nil && h.radio.version != 0 {
		h.radio.powerDown()
	}
	h.spi.close()
	h.rfsw.close()
	h.cs.close()
	for _, args := range h.restore {
		if args[1] == "14" && h.rfsw == nil {
			continue // never taken
		}
		if _, err := cc1101Run("pinctrl", args...); err != nil {
			log.Printf("chat: restore pin: %v", err)
		}
	}
	h.power.close()
	if h.lock != nil {
		h.lock.Close()
	}
}

func (b *chatBackend) run() {
	for !b.w.stop.Load() {
		b.setRadio(func(r *chatRadio) { r.State, r.Detail = "starting", "" })
		hw, err := openChatHW(b)
		if err == nil {
			if err = hw.radio.begin(); err == nil {
				if err = hw.attachRF(); err == nil {
					err = hw.radio.startReceive()
				}
			}
			if err != nil {
				hw.close()
			}
		}
		if err != nil {
			if errors.Is(err, errCCCancelled) {
				break
			}
			st, detail, retry := "error", err.Error(), time.Duration(0)
			switch {
			case errors.Is(err, errCapBusy):
				st, detail, retry = "busy", "", 5*time.Second
			case errors.Is(err, errCCAbsent), errors.Is(err, os.ErrNotExist):
				st, detail, retry = "absent", "", 5*time.Second
			}
			log.Printf("chat: %s: %v", st, err)
			b.failOutbox("radio not ready")
			b.setRadio(func(r *chatRadio) { r.State, r.Detail, r.Version, r.PartNum = st, detail, nil, nil })
			for !b.w.stop.Load() {
				d := 24 * time.Hour
				if retry > 0 {
					d = retry
				}
				if c := b.w.wait(d); c == "retry" || (c == "" && retry > 0) {
					break
				}
			}
			continue
		}
		v, p := int(hw.radio.version), int(hw.radio.part)
		log.Printf("chat: CC1101 PARTNUM=0x%02X VERSION=0x%02X, listening on %.3f MHz", p, v, ccFreqMHz)
		b.setRadio(func(r *chatRadio) { r.State, r.Detail, r.Version, r.PartNum = "listening", "", &v, &p })
		err = b.loop(hw.radio)
		hw.close()
		if err != nil && !errors.Is(err, errCCCancelled) {
			log.Printf("chat: radio error: %v", err)
			b.failOutbox("radio error")
			b.setRadio(func(r *chatRadio) { r.State, r.Detail = "error", err.Error() })
			for !b.w.stop.Load() && b.w.wait(24*time.Hour) != "retry" {
			}
		}
	}
	b.failOutbox("radio stopped")
	b.setRadio(func(r *chatRadio) { r.State = "off" })
}

func (b *chatBackend) failOutbox(why string) {
	b.mu.Lock()
	ids := b.outbox
	b.outbox = nil
	b.mu.Unlock()
	for _, id := range ids {
		b.update(id, func(m *chatMessage) { m.Status, m.Error = "failed", why })
	}
}

func (b *chatBackend) loop(r *ccRadio) error {
	for !b.w.stop.Load() {
		select {
		case c := <-b.w.cmd:
			if c == "retry" {
				return nil
			}
		default:
		}
		b.mu.Lock()
		var id int64
		if len(b.outbox) > 0 {
			id, b.outbox = b.outbox[0], b.outbox[1:]
		}
		b.mu.Unlock()
		if id != 0 {
			if err := b.send(r, id); err != nil {
				return err
			}
			continue
		}
		p, err := r.receive(chatRxSlice)
		if err != nil {
			return err
		}
		if p != nil {
			if _, err := b.process(r, p, 0); err != nil {
				return err
			}
		}
	}
	return errCCCancelled
}

func (b *chatBackend) send(r *ccRadio, id int64) (err error) {
	defer func() {
		if err != nil {
			why := "radio error"
			if errors.Is(err, errCCCancelled) {
				why = "radio stopped"
			}
			b.update(id, func(m *chatMessage) { m.Status, m.Error = "failed", why })
		}
	}()
	b.mu.Lock()
	var text string
	for _, m := range b.messages {
		if m.ID == id {
			text = m.Text
		}
	}
	name := b.nickname
	token := b.token
	b.token = (b.token + 1) & 0xFFFFFF
	if b.token == 0 {
		b.token = 1
	}
	b.mu.Unlock()
	if text == "" {
		return nil
	}
	frame, err := chatEncodeData(token, []byte(text), name)
	if err != nil {
		b.update(id, func(m *chatMessage) { m.Status, m.Error = "failed", err.Error() })
		return nil
	}
	b.setRadio(func(x *chatRadio) { x.State = "sending" })
	defer b.setRadio(func(x *chatRadio) {
		if x.State == "sending" {
			x.State = "listening"
		}
	})
	for attempt := 1; attempt <= chatAttempts; attempt++ {
		log.Printf("chat: send id=%d token=%06X attempt %d/%d", id, token, attempt, chatAttempts)
		if err := r.idle(); err != nil {
			return err
		}
		if err := r.transmit(frame); err != nil {
			return err
		}
		if err := r.startReceive(); err != nil {
			return err
		}
		end := time.Now().Add(chatAckTimeout)
		for time.Now().Before(end) {
			p, err := r.receive(min(chatRxSlice, max(time.Until(end), time.Millisecond)))
			if err != nil {
				return err
			}
			if p == nil {
				continue
			}
			acked, err := b.process(r, p, token)
			if err != nil {
				return err
			}
			if acked {
				b.update(id, func(m *chatMessage) { m.Status = "delivered" })
				return nil
			}
		}
		if attempt < chatAttempts {
			if err := r.sleep(chatBackoff(token, attempt)); err != nil {
				return err
			}
		}
	}
	b.update(id, func(m *chatMessage) { m.Status, m.Error = "failed", "no acknowledgement" })
	return r.startReceive()
}

// process handles one received packet; it reports whether it acknowledged
// the frame with token want.
func (b *chatBackend) process(r *ccRadio, p *ccPacket, want uint32) (bool, error) {
	b.setRadio(func(x *chatRadio) { x.Received, x.CRCErrors = r.received, r.crcBad })
	if !p.crc {
		return false, nil
	}
	f := chatDecode(p.data)
	switch f.kind {
	case chatMalformed:
		return false, nil
	case chatAck:
		return want != 0 && f.token == want, nil
	case chatLegacy:
		b.addReceived(f, p, true)
		return false, nil
	}
	dup := false
	b.mu.Lock()
	for _, t := range b.recent {
		if t == f.token {
			dup = true
		}
	}
	if !dup {
		b.recent = append(b.recent, f.token)
		if len(b.recent) > chatRecentTokens {
			b.recent = b.recent[1:]
		}
	}
	b.mu.Unlock()
	if !dup {
		b.addReceived(f, p, false)
	}
	// acknowledge, after the turnaround the sender needs to switch to RX
	if err := r.sleep(chatAckTurnaround); err != nil {
		return false, err
	}
	if err := r.idle(); err != nil {
		return false, err
	}
	if err := r.transmit(chatEncodeAck(f.token)); err != nil {
		return false, err
	}
	return false, r.startReceive()
}

func (b *chatBackend) addReceived(f chatFrame, p *ccPacket, legacy bool) {
	b.mu.Lock()
	b.nextID++
	m := &chatMessage{ID: b.nextID, Text: chatText(f.payload), Sender: f.sender, Time: time.Now().UnixMilli(),
		RSSI: p.rssi, LQI: int(p.lqi), Status: "received", Legacy: legacy}
	b.appendLocked(m)
	b.mu.Unlock()
	b.hub.publish("message", *m)
}

func init() { hwApps["cap-cc1101-subg-chat"] = hwCC1101Chat }
