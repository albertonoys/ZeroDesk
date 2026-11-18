package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// IR Remote: learn, store and replay infrared remote-control signals on the
// LIRC receiver/transmitter. Ported from CardputerZero/IR-Remote (MIT,
// src/models/ir_service_model.cpp, ir_protocol_decoder.cpp,
// ir_recording_files_model.cpp): same capture rules, the same protocol
// decoder (NEC, NEC Ext, Sony SIRC, RC5, RC6) and the same ".ir" files in
// $HOME/.config/IR-Remote/signals, so both apps share one library. Buttons
// are grouped into remotes with an extra "remote=" key the original ignores.

func init() { hwApps["ir-remote"] = hwIRRemote }

const (
	irFileHeader      = "M5CardputerZero IR Remote signal v1"
	irCaptureTimeout  = 15 * time.Second
	irCaptureSilence  = 220 * time.Millisecond
	irMaxCapturePulse = 4096
)

// ---- .ir files ----

type irSignal struct {
	Pulses  []irPulse
	Carrier uint32
	Duty    uint32
	Driver  string
	Device  string
}

type irFile struct {
	ID       string `json:"id"` // file name inside the signals directory
	Name     string `json:"name"`
	Remote   string `json:"remote"`
	Created  int64  `json:"created"`
	Carrier  uint32 `json:"carrier"`
	Duty     uint32 `json:"duty"`
	Count    int    `json:"pulses"`
	Duration uint64 `json:"duration_us"`
	Driver   string `json:"driver,omitempty"`
	Device   string `json:"device,omitempty"`
	irDecode
	mtime time.Time
}

func irSignalsDir() string {
	if d := os.Getenv("IR_REMOTE_RECORDINGS_DIR"); d != "" {
		return filepath.Clean(d)
	}
	if d := os.Getenv("IR_REMOTE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "signals")
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Join(h, ".config/IR-Remote/signals")
	}
	return "/tmp/IR-Remote/signals"
}

func irEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`).Replace(s)
}

func irUnescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// irParseU64 is strtoull with the original's fallback on no digits.
func irParseU64(s string, fallback uint64) uint64 {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return fallback
	}
	v, err := strconv.ParseUint(s[:i], 10, 64)
	if err != nil {
		return fallback
	}
	return v
}

func irParseRaw(raw string) []irPulse {
	var out []irPulse
	for _, tok := range strings.Split(raw, ",") {
		if tok == "" {
			continue
		}
		pulse := tok[0] != '-'
		if tok[0] == '+' || tok[0] == '-' {
			tok = tok[1:]
		}
		if d := irParseU64(tok, 0); d > 0 && d <= 0xffffffff {
			out = append(out, irPulse{Pulse: pulse, Us: uint32(d)})
		}
	}
	return out
}

func irRawText(p []irPulse) string {
	var b strings.Builder
	for i, x := range p {
		if i > 0 {
			b.WriteByte(',')
		}
		if x.Pulse {
			b.WriteByte('+')
		} else {
			b.WriteByte('-')
		}
		b.WriteString(strconv.FormatUint(uint64(x.Us), 10))
	}
	return b.String()
}

// irParseFile reads a ".ir" file body; base is its file name.
func irParseFile(base string, data []byte) (irFile, irSignal, bool) {
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	if !sc.Scan() || strings.TrimRight(sc.Text(), "\r") != irFileHeader {
		return irFile{}, irSignal{}, false
	}
	f := irFile{ID: base, Name: strings.TrimSuffix(base, filepath.Ext(base)), Carrier: irDefaultCarrier, Duty: irDefaultDuty}
	sig := irSignal{Carrier: irDefaultCarrier, Duty: irDefaultDuty}
	raw := ""
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "name":
			f.Name = irUnescape(v)
		case "remote":
			f.Remote = irUnescape(v)
		case "created_unix_sec":
			f.Created = int64(irParseU64(v, 0))
		case "carrier_hz":
			f.Carrier = uint32(irParseU64(v, uint64(f.Carrier)))
		case "duty_cycle_percent":
			f.Duty = uint32(irParseU64(v, uint64(f.Duty)))
		case "source_driver":
			f.Driver = irUnescape(v)
		case "source_device":
			f.Device = irUnescape(v)
		case "raw_us":
			raw = v
		}
	}
	sig.Pulses = irParseRaw(raw)
	if len(sig.Pulses) == 0 {
		return irFile{}, irSignal{}, false
	}
	sig.Carrier, sig.Duty, sig.Driver, sig.Device = f.Carrier, f.Duty, f.Driver, f.Device
	f.Count = len(sig.Pulses)
	for _, p := range sig.Pulses {
		f.Duration += uint64(p.Us)
	}
	f.irDecode = irDecodeSignal(sig.Pulses)
	return f, sig, true
}

func irFormatFile(f irFile, sig irSignal) string {
	var b strings.Builder
	b.WriteString(irFileHeader + "\n")
	b.WriteString("name=" + irEscape(f.Name) + "\n")
	if f.Remote != "" {
		b.WriteString("remote=" + irEscape(f.Remote) + "\n")
	}
	fmt.Fprintf(&b, "created_unix_sec=%d\ncarrier_hz=%d\nduty_cycle_percent=%d\n", f.Created, sig.Carrier, sig.Duty)
	b.WriteString("source_driver=" + irEscape(sig.Driver) + "\n")
	b.WriteString("source_device=" + irEscape(sig.Device) + "\n")
	b.WriteString("raw_us=" + irRawText(sig.Pulses) + "\n")
	return b.String()
}

// irSanitize is the original's file-name rule.
func irSanitize(name string) string {
	var b strings.Builder
	for _, c := range []byte(name) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			b.WriteByte(c)
		case c == ' ' || c == '.':
			b.WriteByte('_')
		}
	}
	s := strings.Trim(b.String(), "_")
	if s == "" {
		return "signal"
	}
	return s
}

func irUniquePath(dir, base string, now int64) string {
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("%d_%s.ir", now, base)
		if i > 0 {
			name = fmt.Sprintf("%d_%s_%03d.ir", now, base, i)
		}
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%d_%s.ir", now, base))
}

func irWriteFile(path, body string) error {
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---- protocol decoder (original ir_protocol_decoder.cpp) ----

type irDecode struct {
	Decoded    bool   `json:"decoded"`
	Protocol   string `json:"protocol"`
	Address    uint32 `json:"address"`
	Command    uint32 `json:"command"`
	Bits       uint32 `json:"bits"`
	Confidence uint32 `json:"confidence"`
}

func irCloseTo(v, target, pct uint32) bool {
	tol := target * pct / 100
	return v >= target-tol && v <= target+tol
}

// irNormalize merges neighbours of the same polarity and drops zero
// durations. Leading gaps are dropped too: rc-core reports the idle gap
// before the first mark, which is not part of the code.
func irNormalize(in []irPulse) []irPulse {
	var out []irPulse
	for _, p := range in {
		if p.Us == 0 || (len(out) == 0 && !p.Pulse) {
			continue
		}
		if len(out) > 0 && out[len(out)-1].Pulse == p.Pulse {
			out[len(out)-1].Us += p.Us
		} else {
			out = append(out, p)
		}
	}
	return out
}

func irDecodeNEC(p []irPulse) (irDecode, bool) {
	if len(p) < 66 || !p[0].Pulse || p[1].Pulse || !irCloseTo(p[0].Us, 9000, 28) || !irCloseTo(p[1].Us, 4500, 28) {
		return irDecode{}, false
	}
	var data uint32
	for i := range 32 {
		m, s := p[2+i*2], p[3+i*2]
		if !m.Pulse || s.Pulse || !irCloseTo(m.Us, 560, 28) {
			return irDecode{}, false
		}
		switch {
		case irCloseTo(s.Us, 560, 28):
		case irCloseTo(s.Us, 1690, 28):
			data |= 1 << i
		default:
			return irDecode{}, false
		}
	}
	a, ai, c, ci := uint8(data), uint8(data>>8), uint8(data>>16), uint8(data>>24)
	if c^ci != 0xff {
		return irDecode{}, false
	}
	if a^ai == 0xff {
		return irDecode{true, "NEC", uint32(a), uint32(c), 32, 96}, true
	}
	return irDecode{true, "NEC Ext", uint32(a) | uint32(ai)<<8, uint32(c), 32, 88}, true
}

func irDecodeSony(p []irPulse) (irDecode, bool) {
	if len(p) < 25 || !p[0].Pulse || p[1].Pulse || !irCloseTo(p[0].Us, 2400, 28) || !irCloseTo(p[1].Us, 600, 28) {
		return irDecode{}, false
	}
	var data, bits uint32
	for i := 2; i+1 < len(p) && bits < 20; i += 2 {
		m, s := p[i], p[i+1]
		if !m.Pulse || s.Pulse || !irCloseTo(s.Us, 600, 28) {
			break
		}
		switch {
		case irCloseTo(m.Us, 600, 28):
		case irCloseTo(m.Us, 1200, 28):
			data |= 1 << bits
		default:
			return irDecode{}, false
		}
		bits++
	}
	if bits != 12 && bits != 15 && bits != 20 {
		return irDecode{}, false
	}
	conf := uint32(88)
	if bits == 12 {
		conf = 82
	}
	return irDecode{true, "Sony SIRC", data >> 7, data & 0x7f, bits, conf}, true
}

func irManchester(firstPulse bool, first uint32, secondPulse bool, second uint32) int {
	if firstPulse == secondPulse || !irCloseTo(first, 889, 34) || !irCloseTo(second, 889, 34) {
		return -1
	}
	if firstPulse && !secondPulse {
		return 1
	}
	return 0
}

func irDecodeRC5(p []irPulse) (irDecode, bool) {
	if len(p) < 28 {
		return irDecode{}, false
	}
	var bits []int
	for i := 0; i+1 < len(p) && len(bits) < 14; i += 2 {
		b := irManchester(p[i].Pulse, p[i].Us, p[i+1].Pulse, p[i+1].Us)
		if b < 0 {
			return irDecode{}, false
		}
		bits = append(bits, b)
	}
	if len(bits) != 14 || bits[0] != 1 {
		return irDecode{}, false
	}
	var addr uint32
	cmd := uint32(0)
	if bits[1] == 0 {
		cmd = 0x40
	}
	for _, b := range bits[3:8] {
		addr = addr<<1 | uint32(b)
	}
	for _, b := range bits[8:14] {
		cmd = cmd<<1 | uint32(b)
	}
	return irDecode{true, "RC5", addr, cmd, 14, 70}, true
}

func irDecodeRC6(p []irPulse) (irDecode, bool) {
	if len(p) < 40 || !p[0].Pulse || p[1].Pulse || !irCloseTo(p[0].Us, 2666, 34) || !irCloseTo(p[1].Us, 889, 34) {
		return irDecode{}, false
	}
	var bits []int
	for i := 2; i+1 < len(p) && len(bits) < 24; i += 2 {
		a, b := p[i].Us, p[i+1].Us
		if len(bits) == 3 {
			a, b = a/2, b/2
		}
		bit := irManchester(!p[i].Pulse, a, !p[i+1].Pulse, b)
		if bit < 0 {
			break
		}
		bits = append(bits, bit)
	}
	if len(bits) < 20 {
		return irDecode{}, false
	}
	var mode, addr, cmd uint32
	for _, b := range bits[:3] {
		mode = mode<<1 | uint32(b)
	}
	for _, b := range bits[4:12] {
		addr = addr<<1 | uint32(b)
	}
	for _, b := range bits[12:20] {
		cmd = cmd<<1 | uint32(b)
	}
	name := "RC6"
	if mode != 0 {
		name = fmt.Sprintf("RC6 mode %d", mode)
	}
	return irDecode{true, name, addr, cmd, uint32(len(bits)), 65}, true
}

func irDecodeSignal(pulses []irPulse) irDecode {
	p := irNormalize(pulses)
	if len(p) > 0 {
		for _, dec := range []func([]irPulse) (irDecode, bool){irDecodeNEC, irDecodeSony, irDecodeRC5, irDecodeRC6} {
			if d, ok := dec(p); ok {
				return d
			}
		}
	}
	return irDecode{Protocol: "Unknown"}
}

// ---- capture ----

// irCapture collects MODE2 samples like the original: the capture ends on
// the kernel's timeout report or after 220 ms of silence once something was
// received, fails on overflow, and gives up after irCaptureTimeout.
type irCapture struct {
	pulses []irPulse
	last   time.Time
}

// feed returns done=true when the capture is complete (or failed: err).
func (c *irCapture) feed(words []uint32, now time.Time) (done bool, err string) {
	for _, w := range words {
		kind, us := mode2Sample(w)
		switch kind {
		case 'p', 's':
			if us == 0 || (len(c.pulses) == 0 && kind == 's') {
				continue // idle gap before the first mark
			}
			c.pulses = append(c.pulses, irPulse{Pulse: kind == 'p', Us: us})
			c.last = now
			if len(c.pulses) >= irMaxCapturePulse {
				return true, ""
			}
		case 't':
			if len(c.pulses) > 0 {
				return true, ""
			}
		case 'o':
			return true, "overflow"
		}
	}
	return false, ""
}

// ---- the backend ----

type irLearnState struct {
	State   string    `json:"state"` // idle, waiting, captured, error
	Error   string    `json:"error,omitempty"`
	Started int64     `json:"started,omitempty"` // unix ms
	Timeout int64     `json:"timeout_ms"`
	Raw     []int64   `json:"raw,omitempty"` // +mark / -gap durations in µs
	Decode  *irDecode `json:"decode,omitempty"`
	Count   int       `json:"pulses,omitempty"`
	Total   uint64    `json:"duration_us,omitempty"`
	Driver  string    `json:"driver,omitempty"`
	Device  string    `json:"device,omitempty"`
}

type irRemoteBackend struct {
	mu       sync.Mutex
	hub      *hwHub
	dir      string
	learn    irLearnState
	pending  *irSignal
	cancel   chan struct{}
	watch    chan struct{}
	dirStamp string
	sendMu   sync.Mutex
}

func hwIRRemote(mux *http.ServeMux) func() {
	b := &irRemoteBackend{dir: irSignalsDir(), learn: irLearnState{State: "idle", Timeout: irCaptureTimeout.Milliseconds()}}
	b.hub = newHWHub(b.startWatch, b.stopAll)
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		hwJSON(w, http.StatusOK, b.state())
	})
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		b.hub.serve(w, r, func() (string, any) { return "state", b.state() })
	})
	mux.HandleFunc("GET /api/signals/{id}", func(w http.ResponseWriter, r *http.Request) {
		f, sig, err := b.load(r.PathValue("id"))
		if err != nil {
			hwError(w, http.StatusNotFound, "not-found")
			return
		}
		hwJSON(w, http.StatusOK, map[string]any{"signal": f, "raw": irSigned(sig.Pulses)})
	})
	mux.HandleFunc("POST /api/signals/{id}/send", func(w http.ResponseWriter, r *http.Request) {
		_, sig, err := b.load(r.PathValue("id"))
		if err != nil {
			hwError(w, http.StatusNotFound, "not-found")
			return
		}
		b.send(w, sig)
	})
	mux.HandleFunc("PATCH /api/signals/{id}", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name   *string `json:"name"`
			Remote *string `json:"remote"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			hwError(w, http.StatusBadRequest, "bad-request")
			return
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		f, sig, err := b.load(r.PathValue("id"))
		if err != nil {
			hwError(w, http.StatusNotFound, "not-found")
			return
		}
		if req.Name != nil {
			if f.Name = irCleanLabel(*req.Name); f.Name == "" {
				hwError(w, http.StatusBadRequest, "name")
				return
			}
		}
		if req.Remote != nil {
			f.Remote = irCleanLabel(*req.Remote)
		}
		if err := irWriteFile(filepath.Join(b.dir, f.ID), irFormatFile(f, sig)); err != nil {
			hwError(w, http.StatusInternalServerError, "save")
			return
		}
		b.changed()
		hwJSON(w, http.StatusOK, b.listLocked())
	})
	mux.HandleFunc("DELETE /api/signals/{id}", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		id := r.PathValue("id")
		if !irValidID(id) || os.Remove(filepath.Join(b.dir, id)) != nil {
			hwError(w, http.StatusNotFound, "not-found")
			return
		}
		b.changed()
		hwJSON(w, http.StatusOK, b.listLocked())
	})
	// rename or delete a whole remote (every file carrying it)
	mux.HandleFunc("POST /api/remotes", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			From   string `json:"from"`
			To     string `json:"to"`
			Delete bool   `json:"delete"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.From == "" {
			hwError(w, http.StatusBadRequest, "bad-request")
			return
		}
		to := irCleanLabel(req.To)
		if !req.Delete && to == "" {
			hwError(w, http.StatusBadRequest, "name")
			return
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		for _, f := range b.listLocked() {
			if f.Remote != req.From {
				continue
			}
			path := filepath.Join(b.dir, f.ID)
			if req.Delete {
				_ = os.Remove(path)
				continue
			}
			if ff, sig, err := b.load(f.ID); err == nil {
				ff.Remote = to
				_ = irWriteFile(path, irFormatFile(ff, sig))
			}
		}
		b.changed()
		hwJSON(w, http.StatusOK, b.listLocked())
	})
	mux.HandleFunc("POST /api/learn", func(w http.ResponseWriter, r *http.Request) {
		b.startLearn()
		hwJSON(w, http.StatusOK, b.state())
	})
	mux.HandleFunc("POST /api/learn/cancel", func(w http.ResponseWriter, r *http.Request) {
		b.cancelLearn()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/learn/save", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name, Remote string }
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			hwError(w, http.StatusBadRequest, "bad-request")
			return
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.pending == nil {
			hwError(w, http.StatusConflict, "no-capture")
			return
		}
		name := irCleanLabel(req.Name)
		if name == "" {
			name = "IR Signal"
		}
		if err := os.MkdirAll(b.dir, 0o755); err != nil {
			hwError(w, http.StatusInternalServerError, "save")
			return
		}
		now := time.Now().Unix()
		path := irUniquePath(b.dir, irSanitize(name), now)
		f := irFile{ID: filepath.Base(path), Name: name, Remote: irCleanLabel(req.Remote), Created: now}
		if err := irWriteFile(path, irFormatFile(f, *b.pending)); err != nil {
			hwError(w, http.StatusInternalServerError, "save")
			return
		}
		b.pending = nil
		b.learn = irLearnState{State: "idle", Timeout: irCaptureTimeout.Milliseconds()}
		b.changed()
		hwJSON(w, http.StatusOK, map[string]any{"id": f.ID, "signals": b.listLocked()})
	})
	return b.stopAll
}

func irCleanLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 48 {
		s = string(r[:48])
	}
	return s
}

func irValidID(id string) bool {
	return id != "" && !strings.ContainsAny(id, `/\`) && !strings.HasPrefix(id, ".") && strings.EqualFold(filepath.Ext(id), ".ir")
}

// irSigned returns durations with the sign marking gaps (+mark, -gap).
func irSigned(p []irPulse) []int64 {
	out := make([]int64, len(p))
	for i, x := range p {
		out[i] = int64(x.Us)
		if !x.Pulse {
			out[i] = -out[i]
		}
	}
	return out
}

func (b *irRemoteBackend) load(id string) (irFile, irSignal, error) {
	if !irValidID(id) {
		return irFile{}, irSignal{}, os.ErrNotExist
	}
	data, err := os.ReadFile(filepath.Join(b.dir, id))
	if err != nil {
		return irFile{}, irSignal{}, err
	}
	f, sig, ok := irParseFile(id, data)
	if !ok {
		return irFile{}, irSignal{}, os.ErrInvalid
	}
	return f, sig, nil
}

func (b *irRemoteBackend) listLocked() []irFile {
	ents, _ := os.ReadDir(b.dir)
	out := []irFile{}
	for _, e := range ents {
		if !e.Type().IsRegular() || !irValidID(e.Name()) {
			continue
		}
		f, _, err := b.load(e.Name())
		if err != nil {
			continue
		}
		if fi, err := e.Info(); err == nil {
			f.mtime = fi.ModTime()
		}
		if f.Created == 0 {
			f.Created = f.mtime.Unix()
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created < out[j].Created
		}
		return out[i].ID < out[j].ID
	})
	return out
}

type irDevState struct {
	Path   string `json:"path"`
	Driver string `json:"driver,omitempty"`
}

func (b *irRemoteBackend) state() map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := map[string]any{"dir": b.dir, "signals": b.listLocked(), "learn": b.learn}
	if d, err := lircFind(lircCanRecMode2, "IR_REMOTE_LIRC_RX_DEVICE"); err == nil {
		st["rx"] = irDevState{d.Path, d.Driver}
	} else {
		st["rx_error"] = lircErrorKey(err)
	}
	if d, err := lircFind(lircCanSendPulse, "IR_REMOTE_LIRC_TX_DEVICE"); err == nil {
		st["tx"] = irDevState{d.Path, d.Driver}
	} else {
		st["tx_error"] = lircErrorKey(err)
	}
	return st
}

func (b *irRemoteBackend) send(w http.ResponseWriter, sig irSignal) {
	b.sendMu.Lock()
	defer b.sendMu.Unlock()
	b.mu.Lock()
	busy := b.learn.State == "waiting"
	b.mu.Unlock()
	if busy {
		hwError(w, http.StatusConflict, "busy")
		return
	}
	_, n, err := lircTransmit(sig.Pulses, sig.Carrier, sig.Duty, "IR_REMOTE_LIRC_TX_DEVICE")
	if err != nil {
		hwError(w, http.StatusServiceUnavailable, lircErrorKey(err))
		return
	}
	hwJSON(w, http.StatusOK, map[string]any{"events": n})
}

// changed tells subscribers the library changed (b.mu held).
func (b *irRemoteBackend) changed() {
	b.dirStamp = b.stampLocked()
	b.hub.publish("signals", b.listLocked())
}

func (b *irRemoteBackend) stampLocked() string {
	ents, _ := os.ReadDir(b.dir)
	var s strings.Builder
	for _, e := range ents {
		if fi, err := e.Info(); err == nil {
			fmt.Fprintf(&s, "%s:%d:%d;", e.Name(), fi.ModTime().UnixNano(), fi.Size())
		}
	}
	return s.String()
}

// startWatch notices files written by the LCD app while a window is open.
func (b *irRemoteBackend) startWatch() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.watch != nil {
		return
	}
	stop := make(chan struct{})
	b.watch = stop
	b.dirStamp = b.stampLocked()
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				b.mu.Lock()
				if s := b.stampLocked(); s != b.dirStamp {
					b.changed()
				}
				b.mu.Unlock()
			}
		}
	}()
}

func (b *irRemoteBackend) stopAll() {
	b.cancelLearn()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.watch != nil {
		close(b.watch)
		b.watch = nil
	}
}

func (b *irRemoteBackend) setLearn(s irLearnState) {
	s.Timeout = irCaptureTimeout.Milliseconds()
	b.learn = s
	b.hub.publish("learn", s)
}

func (b *irRemoteBackend) cancelLearn() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		close(b.cancel)
		b.cancel = nil
	}
	b.pending = nil
	b.learn = irLearnState{State: "idle", Timeout: irCaptureTimeout.Milliseconds()}
}

func (b *irRemoteBackend) startLearn() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		close(b.cancel)
	}
	b.pending = nil
	rx, err := openLircRX("IR_REMOTE_LIRC_RX_DEVICE")
	if err != nil {
		b.cancel = nil
		b.setLearn(irLearnState{State: "error", Error: lircErrorKey(err)})
		return
	}
	cancel := make(chan struct{})
	b.cancel = cancel
	start := time.Now()
	b.setLearn(irLearnState{State: "waiting", Started: start.UnixMilli()})
	go func() {
		<-cancel
		rx.close() // unblocks the reader
	}()
	go b.capture(rx, cancel, start)
}

func (b *irRemoteBackend) capture(rx *lircRX, cancel chan struct{}, start time.Time) {
	var c irCapture
	buf := make([]byte, 1024)
	result, errKey := false, ""
	for {
		now := time.Now()
		deadline := start.Add(irCaptureTimeout)
		if len(c.pulses) > 0 && c.last.Add(irCaptureSilence).Before(deadline) {
			deadline = c.last.Add(irCaptureSilence)
		}
		if !now.Before(deadline) {
			if len(c.pulses) > 0 {
				result = true
			} else {
				errKey = "timeout"
			}
			break
		}
		words, err := rx.read(buf, deadline)
		done, ferr := c.feed(words, time.Now())
		if done {
			result, errKey = ferr == "", ferr
			break
		}
		if err != nil && !os.IsTimeout(err) {
			select {
			case <-cancel:
				return // cancelled: the state was already reset
			default:
			}
			errKey = "io"
			break
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != cancel {
		return
	}
	close(cancel)
	b.cancel = nil
	if !result {
		b.setLearn(irLearnState{State: "error", Error: errKey})
		return
	}
	sig := irSignal{Pulses: irNormalize(c.pulses), Carrier: irDefaultCarrier, Duty: irDefaultDuty, Driver: rx.dev.Driver, Device: rx.dev.Path}
	if sig.Driver == "" {
		sig.Driver = "lirc"
	}
	b.pending = &sig
	d := irDecodeSignal(sig.Pulses)
	var total uint64
	for _, p := range sig.Pulses {
		total += uint64(p.Us)
	}
	s := irLearnState{State: "captured", Started: start.UnixMilli(), Decode: &d, Count: len(sig.Pulses), Total: total, Driver: sig.Driver, Device: sig.Device, Raw: irSigned(sig.Pulses)}
	b.setLearn(s)
}
