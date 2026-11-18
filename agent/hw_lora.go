package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// LoRa: group chat over the Cap LoRa-1262's SX1262, ported from
// CardputerZero/Cap-LoRa-1262 (MIT): same radio settings (lora_sx1262.go),
// same payload format (models/lora_chat_protocol.hpp: nickname marker,
// optional reply reference, 116/107 byte limits), same nickname file
// (~/.config/Cap-LoRa-1262/nickname), 64-message history, Cap power and the
// PI4IO expander handling of src/lora/cap_lora_1262.cpp.

func init() { hwApps["cap-lora-1262"] = hwLoRa }

// ---- chat payload (lora_chat_protocol.hpp) ----

const (
	loraNickMax     = 10
	loraMsgMax      = loraMaxPayload - loraNickMax - 1 // 116
	loraReplyRefLen = 9                                // 0x1d + 8 hex digits
	loraReplyMax    = loraMsgMax - loraReplyRefLen     // 107
	loraHistory     = 64
)

// loraMessageID is the FNV-1a hash of a message's text, used to refer to
// it in replies.
func loraMessageID(text string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(text); i++ {
		h ^= uint32(text[i])
		h *= 16777619
	}
	return h
}

func loraPrintable(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func loraValidNick(s string) bool {
	return s != "" && len(s) <= loraNickMax && strings.Trim(s, " ") != "" && loraPrintable(s)
}

// loraEncode builds the radio payload: 0x80|len(nick), nick, [0x1d +
// 8 upper-case hex digits of the replied message's id], message.
func loraEncode(msg, nick, replyTo string) []byte {
	limit := loraMsgMax
	if replyTo != "" {
		limit = loraReplyMax
	}
	if msg == "" || len(msg) > limit {
		return nil
	}
	ref := 0
	if replyTo != "" {
		ref = loraReplyRefLen
	}
	if nick == "" || len(nick) > loraNickMax || !loraPrintable(nick) || len(msg)+len(nick)+1+ref > loraMaxPayload {
		return []byte(msg)
	}
	p := make([]byte, 0, 1+len(nick)+ref+len(msg))
	p = append(p, 0x80|byte(len(nick)))
	p = append(p, nick...)
	if replyTo != "" {
		p = append(p, 0x1d)
		p = append(p, fmt.Sprintf("%08X", loraMessageID(replyTo))...)
	}
	return append(p, msg...)
}

type loraDecoded struct {
	Message  string
	Nickname string
	ReplyID  uint32
	HasReply bool
}

func loraDecode(p []byte) loraDecoded {
	if len(p) > 0 {
		m := p[0]
		n := int(m & 0x7f)
		if m&0x80 != 0 && n > 0 && n <= loraNickMax && len(p) > 1+n && loraPrintable(string(p[1:1+n])) {
			d := loraDecoded{Nickname: string(p[1 : 1+n])}
			msg := p[1+n:]
			if len(msg) > loraReplyRefLen && msg[0] == 0x1d {
				var id uint32
				ok := true
				for _, c := range msg[1:loraReplyRefLen] {
					switch {
					case c >= '0' && c <= '9':
						id = id<<4 | uint32(c-'0')
					case c >= 'A' && c <= 'F':
						id = id<<4 | uint32(c-'A'+10)
					default:
						ok = false
					}
				}
				if ok {
					d.ReplyID, d.HasReply = id, true
					msg = msg[loraReplyRefLen:]
				}
			}
			d.Message = string(msg)
			return d
		}
	}
	return loraDecoded{Message: string(p)}
}

// loraCString mirrors the original treating the payload as a C string.
func loraCString(b []byte) []byte {
	for i, c := range b {
		if c == 0 {
			return b[:i]
		}
	}
	return b
}

// ---- nickname store (models/lora_nickname_store.cpp) ----

func loraNickPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return filepath.Join(home, ".config", "Cap-LoRa-1262", "nickname")
}

func loadLoraNick() string {
	if b, err := os.ReadFile(loraNickPath()); err == nil {
		line, _, _ := strings.Cut(string(b), "\n")
		if loraValidNick(line) {
			return line
		}
	}
	name := "LoRa"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	if len(name) > loraNickMax {
		name = name[:loraNickMax]
	}
	return name
}

func saveLoraNick(nick string) error {
	if !loraValidNick(nick) {
		return errors.New("invalid nickname")
	}
	p := loraNickPath()
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if st, err := os.Lstat(dir); err != nil || !st.IsDir() {
		return errors.New("invalid configuration directory")
	}
	f, err := os.CreateTemp(dir, "nickname.tmp.")
	if err != nil {
		return err
	}
	_, err = f.WriteString(nick)
	if err == nil {
		err = f.Chmod(0o600)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), p)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// ---- the Cap's PI4IO expander (cp0_lora_pi4io_controller) ----

// The original drives P0 of the Cap's expander at 0x43 high (bit 0 of
// registers 0x01/0x02/0x03 as output/polarity/config) and restores only
// those bits on exit. A missing expander is not fatal.
type loraPI4IO struct {
	saved  [3]uint8 // 0x01, 0x02, 0x03
	ok     bool
	status string
}

func (p *loraPI4IO) init() {
	c, err := openLoraI2C("/dev/i2c-1", 0x43)
	if err != nil {
		p.status = "I2C open failed"
		return
	}
	defer c.close()
	if id, err := c.readReg(0x01); err != nil || id == 0 {
		p.status = "0x43 not found"
		return
	}
	for i, reg := range []uint8{0x01, 0x02, 0x03} {
		v, err := c.readReg(reg)
		if err != nil {
			p.status = "Snapshot failed"
			return
		}
		p.saved[i] = v
	}
	writes := []struct{ reg, v uint8 }{
		{0x02, p.saved[1] &^ 1},
		{0x01, p.saved[0] | 1},
		{0x03, p.saved[2] &^ 1},
	}
	p.ok = true // from here on, restore what was touched
	for _, w := range writes {
		if err := c.writeReg(w.reg, w.v); err != nil {
			p.restore()
			p.status = "Write failed"
			return
		}
	}
	p.status = "Init OK"
}

func (p *loraPI4IO) restore() {
	if !p.ok {
		return
	}
	c, err := openLoraI2C("/dev/i2c-1", 0x43)
	if err != nil {
		p.status = "Restore failed"
		return
	}
	defer c.close()
	for i, reg := range []uint8{0x01, 0x02, 0x03} {
		if cur, err := c.readReg(reg); err == nil {
			_ = c.writeReg(reg, cur&^1|p.saved[i]&1)
		}
	}
	p.ok, p.status = false, "State restored"
}

// ---- the backend ----

type loraMessage struct {
	ID          int     `json:"id"`
	Text        string  `json:"text"`
	Outgoing    bool    `json:"outgoing"`
	Sender      string  `json:"sender,omitempty"`
	RSSI        float64 `json:"rssi"`
	SNR         float64 `json:"snr"`
	Time        int64   `json:"time"` // unix ms
	Delivery    string  `json:"delivery"`
	ReplyText   string  `json:"reply_text,omitempty"`
	ReplySender string  `json:"reply_sender,omitempty"`
	ReplyMiss   bool    `json:"reply_missing,omitempty"`
}

type loraRadioInfo struct {
	State     string  `json:"state"` // off, starting, receiving, sending, error
	Error     string  `json:"error,omitempty"`
	Missing   bool    `json:"missing,omitempty"`
	Chip      string  `json:"chip,omitempty"`
	SPI       string  `json:"spi"`
	PI4IO     string  `json:"pi4io"`
	RSSI      float64 `json:"rssi"`
	SNR       float64 `json:"snr"`
	RX        uint64  `json:"rx"`
	TX        uint64  `json:"tx"`
	CRCErrors uint64  `json:"crc_errors"`
	Retry     int64   `json:"retry_at,omitempty"` // unix ms of the next automatic attempt
}

type loraConfig struct {
	FrequencyMHz float64 `json:"frequency_mhz"`
	BandwidthKHz float64 `json:"bandwidth_khz"`
	SF           int     `json:"sf"`
	CR           int     `json:"cr"`
	PowerDBm     int     `json:"power_dbm"`
	Preamble     int     `json:"preamble"`
	SyncWord     int     `json:"sync_word"`
	TCXO         float64 `json:"tcxo_v"`
	CurrentLimit float64 `json:"current_limit_ma"`
	PayloadLimit int     `json:"payload_limit"`
	ReplyLimit   int     `json:"reply_limit"`
	NickLimit    int     `json:"nick_limit"`
	SPISpeed     int     `json:"spi_speed_hz"`
}

type loraState struct {
	Radio    loraRadioInfo `json:"radio"`
	Config   loraConfig    `json:"config"`
	Nickname string        `json:"nickname"`
	Messages []loraMessage `json:"messages"`
}

type loraSendReq struct {
	payload []byte
	id      int
}

type loraBackend struct {
	mu       sync.Mutex
	hub      *hwHub
	radio    loraRadioInfo
	nick     string
	messages []loraMessage
	nextID   int
	running  bool
	stop     chan struct{}
	done     chan struct{}
	sendQ    chan loraSendReq
	retryNow chan struct{}
	idle     *time.Timer
}

func hwLoRa(mux *http.ServeMux) func() {
	b := &loraBackend{nick: loadLoraNick(), radio: loraRadioInfo{State: "off", SPI: loraSPIDefault, PI4IO: "Not checked"}}
	b.hub = newHWHub(b.subscribed, b.unsubscribed)
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		hwJSON(w, http.StatusOK, b.snapshot())
	})
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		b.hub.serve(w, r, func() (string, any) { return "state", b.snapshot() })
	})
	mux.HandleFunc("POST /api/send", b.handleSend)
	mux.HandleFunc("POST /api/nickname", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Nickname string `json:"nickname"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req) != nil || !loraValidNick(req.Nickname) {
			hwError(w, http.StatusBadRequest, "nickname")
			return
		}
		if err := saveLoraNick(req.Nickname); err != nil {
			hwError(w, http.StatusInternalServerError, "save")
			return
		}
		b.mu.Lock()
		b.nick = req.Nickname
		b.mu.Unlock()
		b.changed()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/retry", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		q := b.retryNow
		b.mu.Unlock()
		if q != nil {
			select {
			case q <- struct{}{}:
			default:
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return b.shutdown
}

func (b *loraBackend) snapshot() loraState {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, err := os.Stat(loraNickPath()); err == nil && s.Mode().IsRegular() {
		// the LCD app may have changed it
		if n := loadLoraNick(); n != b.nick {
			b.nick = n
		}
	}
	return loraState{
		Radio: b.radio,
		Config: loraConfig{
			FrequencyMHz: loraFrequencyMHz, BandwidthKHz: loraBandwidthKHz, SF: loraSF, CR: loraCR,
			PowerDBm: loraPowerDBm, Preamble: loraPreamble, SyncWord: loraSyncWord, TCXO: loraTCXOVolts,
			CurrentLimit: loraCurrentLimit, PayloadLimit: loraMsgMax, ReplyLimit: loraReplyMax,
			NickLimit: loraNickMax, SPISpeed: loraSPISpeed,
		},
		Nickname: b.nick,
		Messages: append(make([]loraMessage, 0, len(b.messages)), b.messages...),
	}
}

func (b *loraBackend) changed() { b.hub.publish("state", b.snapshot()) }

func (b *loraBackend) handleSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text    string `json:"text"`
		ReplyTo int    `json:"reply_to"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil || req.Text == "" || !utf8.ValidString(req.Text) {
		hwError(w, http.StatusBadRequest, "empty")
		return
	}
	b.mu.Lock()
	if b.radio.State != "receiving" || b.sendQ == nil {
		st := b.radio.State
		b.mu.Unlock()
		if st == "sending" {
			hwError(w, http.StatusConflict, "busy")
		} else {
			hwError(w, http.StatusServiceUnavailable, "unavailable")
		}
		return
	}
	var replyText, replySender string
	if req.ReplyTo != 0 {
		var target *loraMessage
		for i := range b.messages {
			if b.messages[i].ID == req.ReplyTo {
				target = &b.messages[i]
			}
		}
		if target == nil || (target.Delivery != "received" && target.Delivery != "sent") {
			b.mu.Unlock()
			hwError(w, http.StatusConflict, "reply")
			return
		}
		replyText = target.Text
		replySender = target.Sender
		if target.Outgoing {
			replySender = b.nick
		}
		if replySender == "" {
			replySender = "Unknown"
		}
	}
	payload := loraEncode(req.Text, b.nick, replyText)
	if payload == nil {
		b.mu.Unlock()
		hwError(w, http.StatusBadRequest, "too-long")
		return
	}
	id := b.appendLocked(loraMessage{Text: req.Text, Outgoing: true, Delivery: "pending", ReplyText: replyText, ReplySender: replySender})
	b.radio.State = "sending"
	q := b.sendQ
	b.mu.Unlock()
	q <- loraSendReq{payload: payload, id: id}
	b.changed()
	hwJSON(w, http.StatusAccepted, map[string]int{"id": id})
}

func (b *loraBackend) appendLocked(m loraMessage) int {
	b.nextID++
	m.ID = b.nextID
	m.Time = time.Now().UnixMilli()
	if len(b.messages) >= loraHistory {
		b.messages = append(b.messages[:0], b.messages[1:]...)
	}
	b.messages = append(b.messages, m)
	return m.ID
}

func (b *loraBackend) setDelivery(id int, d string) {
	b.mu.Lock()
	for i := range b.messages {
		if b.messages[i].ID == id {
			b.messages[i].Delivery = d
		}
	}
	b.mu.Unlock()
}

// ---- radio lifecycle: on with the first window, off a little after the
// last one closes (an EventSource reconnect does not reset the radio) ----

const loraIdleOff = 5 * time.Second

func (b *loraBackend) subscribed() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.idle != nil {
		b.idle.Stop()
		b.idle = nil
	}
	if b.running {
		return
	}
	b.running = true
	b.stop, b.done = make(chan struct{}), make(chan struct{})
	b.sendQ = make(chan loraSendReq, 1)
	b.retryNow = make(chan struct{}, 1)
	go b.worker(b.stop, b.done, b.sendQ, b.retryNow)
}

func (b *loraBackend) unsubscribed() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.idle != nil {
		b.idle.Stop()
	}
	b.idle = time.AfterFunc(loraIdleOff, func() {
		if b.hub.count() == 0 {
			b.shutdown()
		}
	})
}

func (b *loraBackend) shutdown() {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return
	}
	b.running = false
	stop, done := b.stop, b.done
	b.mu.Unlock()
	close(stop)
	<-done
}

func (b *loraBackend) setRadio(f func(r *loraRadioInfo)) {
	b.mu.Lock()
	f(&b.radio)
	b.mu.Unlock()
	b.changed()
}

// loraHardware is everything the original's InitHard opens.
type loraHardware struct {
	power *loraCapPower
	pi4io loraPI4IO
	chip  *sx1262
}

func (h *loraHardware) close() {
	if h.chip != nil {
		h.chip.sleep()
		h.chip.close()
		h.chip = nil
	}
	h.pi4io.restore()
	h.power.release()
	h.power = nil
}

func openLoraHardware() (*loraHardware, error) {
	h := &loraHardware{}
	var err error
	if h.power, err = loraPowerOn(loraExt5V, loraGrove5V); err != nil {
		return h, err
	}
	time.Sleep(150 * time.Millisecond) // rail settle, as the original
	h.pi4io.init()
	spiPath := loraSPIDefault
	if p := os.Getenv("LORA_SPI_DEV"); p != "" {
		spiPath = p
	}
	chip := &sx1262{}
	if chip.spi, err = openLoraSPI(spiPath, loraSPISpeed); err != nil {
		h.close()
		return nil, err
	}
	if chip.reset, err = requestLoraGPIO(loraGPIOChip, []int{loraPinReset}, true, 1, "zerodesk-lora"); err != nil {
		chip.close()
		h.close()
		return nil, err
	}
	if chip.inputs, err = requestLoraGPIO(loraGPIOChip, []int{loraPinBusy, loraPinIRQ}, false, 0, "zerodesk-lora"); err != nil {
		chip.close()
		h.close()
		return nil, err
	}
	h.chip = chip
	return h, nil
}

func loraErrorText(err error) string {
	var en syscall.Errno
	switch {
	case errors.Is(err, errNoSX1262):
		return "SX1262 not found"
	case errors.As(err, &en) && (en == syscall.EACCES || en == syscall.EPERM):
		return "Permission denied: " + err.Error()
	case errors.As(err, &en) && en == syscall.EBUSY:
		return "Device busy: " + err.Error()
	}
	return err.Error()
}

func (b *loraBackend) worker(stop, done chan struct{}, sendQ chan loraSendReq, retryNow chan struct{}) {
	defer close(done)
	backoff := 3 * time.Second
	for {
		b.setRadio(func(r *loraRadioInfo) { r.State, r.Error, r.Missing, r.Retry = "starting", "", false, 0 })
		h, err := openLoraHardware()
		if err == nil {
			b.setRadio(func(r *loraRadioInfo) { r.PI4IO, r.SPI = h.pi4io.status, h.chip.spi.path })
			err = h.chip.begin()
			if err == nil {
				err = h.chip.startReceive()
			}
		}
		if err == nil {
			backoff = 3 * time.Second
			b.setRadio(func(r *loraRadioInfo) { r.State, r.Chip = "receiving", h.chip.version })
			err = b.serve(h.chip, stop, sendQ)
			h.close()
			if err == nil { // stopped
				b.setRadio(func(r *loraRadioInfo) { r.State, r.PI4IO = "off", h.pi4io.status })
				return
			}
		} else if h != nil {
			h.close()
		}
		// failed: report, release everything, retry later (the original
		// retries every few seconds; back off when the Cap is absent)
		retryAt := time.Now().Add(backoff)
		select {
		case <-sendQ: // never transmit a request from before the failure
		default:
		}
		b.failPending()
		b.setRadio(func(r *loraRadioInfo) {
			r.State, r.Error, r.Missing, r.Retry = "error", loraErrorText(err), errors.Is(err, errNoSX1262), retryAt.UnixMilli()
			if h != nil {
				r.PI4IO = h.pi4io.status
			}
		})
		t := time.NewTimer(backoff)
		backoff = min(backoff*2, 30*time.Second)
		select {
		case <-stop:
			t.Stop()
			b.setRadio(func(r *loraRadioInfo) { r.State, r.Retry = "off", 0 })
			return
		case <-retryNow:
			t.Stop()
			backoff = 3 * time.Second
		case <-t.C:
		}
	}
}

func (b *loraBackend) failPending() {
	b.mu.Lock()
	for i := range b.messages {
		if b.messages[i].Delivery == "pending" {
			b.messages[i].Delivery = "failed"
		}
	}
	b.mu.Unlock()
}

// serve runs the receive/transmit loop until stop (nil) or a radio error.
func (b *loraBackend) serve(r *sx1262, stop chan struct{}, sendQ chan loraSendReq) error {
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	lastPoll := time.Now()
	for {
		select {
		case <-stop:
			return nil
		case req := <-sendQ:
			if err := b.transmit(r, req, stop); err != nil {
				return err
			}
		case <-tick.C:
			// DIO1 carries RX_DONE; poll the IRQ register now and then as well
			if !r.dio1() && time.Since(lastPoll) < time.Second {
				continue
			}
			lastPoll = time.Now()
			irq, err := r.irqStatus()
			if err != nil {
				return err
			}
			if irq&sxIrqRxDone == 0 {
				continue
			}
			p, err := r.readPacket(irq)
			if err != nil {
				return err
			}
			if err := r.startReceive(); err != nil {
				return err
			}
			b.received(p)
		}
	}
}

func (b *loraBackend) received(p loraPacket) {
	b.mu.Lock()
	if p.crcError {
		b.radio.CRCErrors++
		b.mu.Unlock()
		b.changed()
		return
	}
	data := p.data
	if len(data) > loraMaxPayload {
		data = data[:loraMaxPayload]
	}
	data = loraCString(data)
	b.radio.RX++
	b.radio.RSSI, b.radio.SNR = p.rssi, p.snr
	if len(data) > 0 {
		d := loraDecode(data)
		m := loraMessage{Text: strings.ToValidUTF8(d.Message, "�"), Sender: d.Nickname, RSSI: p.rssi, SNR: p.snr, Delivery: "received"}
		if d.HasReply {
			m.ReplySender, m.ReplyMiss = "Unknown", true
			for i := len(b.messages) - 1; i >= 0; i-- {
				t := b.messages[i]
				if loraMessageID(t.Text) == d.ReplyID {
					m.ReplyText, m.ReplyMiss = t.Text, false
					m.ReplySender = t.Sender
					if t.Outgoing {
						m.ReplySender = b.nick
					}
					if m.ReplySender == "" {
						m.ReplySender = "Unknown"
					}
					break
				}
			}
		}
		b.appendLocked(m)
	}
	b.mu.Unlock()
	b.changed()
}

func (b *loraBackend) transmit(r *sx1262, req loraSendReq, stop chan struct{}) error {
	finish := func(ok bool) {
		d := "sent"
		if !ok {
			d = "failed"
		}
		b.setDelivery(req.id, d)
		b.setRadio(func(ri *loraRadioInfo) {
			ri.State = "receiving"
			if ok {
				ri.TX++
			}
		})
	}
	if err := r.startTransmit(req.payload); err != nil {
		finish(false)
		_ = r.standby()
		return r.startReceive()
	}
	deadline := time.Now().Add(loraTimeOnAir(len(req.payload), loraSF, loraBandwidthKHz, loraCR, loraPreamble)*3/2 + 2*time.Second)
	for {
		select {
		case <-stop:
			_ = r.standby() // never leave the transmitter keyed
			finish(false)
			return nil
		case <-time.After(20 * time.Millisecond):
		}
		if !r.dio1() && time.Now().Before(deadline) {
			continue
		}
		irq, err := r.irqStatus()
		if err != nil {
			_ = r.standby()
			finish(false)
			return err
		}
		if irq&sxIrqTxDone != 0 {
			err := r.finishTransmit()
			finish(err == nil)
			if err != nil {
				return err
			}
			return r.startReceive()
		}
		if time.Now().After(deadline) {
			_ = r.standby()
			finish(false)
			return r.startReceive()
		}
	}
}
