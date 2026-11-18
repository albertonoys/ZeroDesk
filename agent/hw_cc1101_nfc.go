package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// NFC: the ST25R3916 reader on the Cap CC1101 accessory. Discovery
// (NFC-F preferred, then NFC-A with a protocol lock while a tag stays),
// presence debouncing and the NDEF parsing are ported from
// CardputerZero/Cap-CC1101-NFC (MIT, src/nfc/linux_nfc_backend.cpp,
// src/nfc/nfc_worker.cpp); Type 2 identification and memory reads follow
// CardputerZero/RFID's read path. Read only: nothing is ever written to a tag.
//
// Bus: the original loads the BSP overlay spi0-spidev2-gpio22 (as root) and
// talks to /dev/spidev0.2 with kernel-managed CS2 on GPIO22. When that node
// exists it is used the same way. Otherwise (stock image, no root) the reader
// is driven through /dev/spidev0.1 with SPI_NO_CS and GPIO22 as a software
// chip select, which is how the original's earlier builds worked. The LCD
// shares SPI0, so every frame first flushes the queue with a deselected
// dummy byte and then checks the controller's message counter: if anything
// else ran while GPIO22 was low, the frame is reported as contended and the
// chip is re-initialised before the next poll.

const (
	nfcIRQGPIO       = 23
	nfcCSGPIO        = 22
	nfcSpeedHz       = 5_000_000
	nfcHistoryMax    = 50
	nfcMinCycle      = 80 * time.Millisecond
	nfcPresentCycle  = 500 * time.Millisecond
	nfcRemoveConfirm = 3
	nfcRemoveDebounc = 160 * time.Millisecond
	nfcPresentConfrm = 2
	nfcSPIStats      = "/sys/class/spi_master/spi0/statistics/messages"
	capSPIDev        = "/dev/spidev0.1"
	nfcKernelSPIDev  = "/dev/spidev0.2"
)

// ---- transports ----

type nfcKernelCS struct {
	spi  *spiDev
	irqL *gpioLine
}

func (k *nfcKernelCS) xfer(tx []byte) ([]byte, error) { return k.spi.transfer(tx) }
func (k *nfcKernelCS) irq() bool                      { v, _ := k.irqL.get(); return v }
func (k *nfcKernelCS) waitIRQ(d time.Duration) bool   { return k.irqL.waitEdge(d) }

type nfcSoftCS struct {
	spi       *spiDev
	cs, irqL  *gpioLine
	stats     *os.File
	contended atomic.Int64
}

func (s *nfcSoftCS) count() int64 {
	if s.stats == nil {
		return 0
	}
	var b [24]byte
	n, _ := s.stats.ReadAt(b[:], 0)
	v, _ := strconv.ParseInt(strings.TrimSpace(string(b[:n])), 10, 64)
	return v
}

func (s *nfcSoftCS) xfer(tx []byte) ([]byte, error) {
	// a deselected dummy byte waits out any message already queued on SPI0
	if _, err := s.spi.transfer([]byte{0}); err != nil {
		return nil, err
	}
	c0 := s.count()
	if err := s.cs.set(false); err != nil {
		return nil, err
	}
	rx, err := s.spi.transfer(tx)
	c1 := s.count()
	_ = s.cs.set(true)
	if err != nil {
		return nil, err
	}
	if s.stats != nil && c1 != c0+1 {
		s.contended.Add(1)
		return rx, errSTContended
	}
	return rx, nil
}

func (s *nfcSoftCS) irq() bool                    { v, _ := s.irqL.get(); return v }
func (s *nfcSoftCS) waitIRQ(d time.Duration) bool { return s.irqL.waitEdge(d) }

// ---- hardware session ----

type nfcHW struct {
	lock     *os.File
	power    *gpioLine
	spi      *spiDev
	cs, irq  *gpioLine
	stats    *os.File
	pinRest  []string
	soft     *nfcSoftCS
	drv      *stDriver
	chip     stChipInfo
	bus, csS string
}

var errCapBusy = errors.New("busy")

// capLock takes the advisory lock shared by the Cap CC1101 backends (the NFC
// reader and the radio change the same spidev's mode).
func capLock() (*os.File, error) {
	f, err := os.Open(capSPIDev)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errCapBusy
	}
	return f, nil
}

func openNFCHW(stop *atomic.Bool) (*nfcHW, error) {
	h := &nfcHW{}
	var err error
	if h.lock, err = capLock(); err != nil {
		return nil, err
	}
	if h.power, err = capPower(); err != nil {
		h.close()
		return nil, err
	}
	if h.irq, err = requestGPIO(capGPIOChip, nfcIRQGPIO, gpioV2FlagInput|gpioV2FlagEdgeRising, false, "st25r3916-irq"); err != nil {
		h.close()
		return nil, err
	}
	var t stTransport
	if _, e := os.Stat(nfcKernelSPIDev); e == nil {
		if h.spi, err = openSPI(nfcKernelSPIDev, spiModeCPHA, nfcSpeedHz); err != nil {
			h.close()
			return nil, err
		}
		h.bus, h.csS = nfcKernelSPIDev, "CS2 (GPIO22)"
		t = &nfcKernelCS{spi: h.spi, irqL: h.irq}
	} else {
		if h.pinRest, err = pinctrlGet(nfcCSGPIO); err != nil {
			log.Printf("nfc: %v", err)
		}
		if h.cs, err = requestGPIO(capGPIOChip, nfcCSGPIO, gpioV2FlagOutput, true, "st25r3916-cs"); err != nil {
			h.close()
			return nil, err
		}
		if h.spi, err = openSPI(capSPIDev, spiModeCPHA|spiModeNoCS, nfcSpeedHz); err != nil {
			h.close()
			return nil, err
		}
		h.stats, _ = os.Open(nfcSPIStats)
		h.soft = &nfcSoftCS{spi: h.spi, cs: h.cs, irqL: h.irq, stats: h.stats}
		h.bus, h.csS, t = capSPIDev, "GPIO22", h.soft
	}
	h.drv = newSTDriver(t, stop)
	return h, nil
}

func (h *nfcHW) close() {
	if h.drv != nil && h.drv.ready {
		h.drv.shutdown()
	}
	if h.spi != nil {
		if h.soft != nil {
			_ = h.spi.setMode(0) // back to the CC1101's mode, kernel CS1
		}
		h.spi.close()
	}
	if h.stats != nil {
		h.stats.Close()
	}
	h.cs.close()
	h.irq.close()
	if h.cs != nil && len(h.pinRest) > 0 {
		if _, err := cc1101Run("pinctrl", h.pinRest...); err != nil {
			log.Printf("nfc: restore GPIO22: %v", err)
		}
	}
	h.power.close()
	if h.lock != nil {
		h.lock.Close()
	}
}

// ---- discovery (linux_nfc_backend.cpp pollDiscovery) ----

type nfcObservation struct {
	kind stPollKind
	tag  *nfcTag
}

type nfcDiscovery struct {
	probeF       bool // next protocol to probe first
	locked       int  // 0 none, 'A', 'F'
	lockedNoTag  int
	fCleanNoTag  int
	fInconclusiv int
}

func (n *nfcDiscovery) reset() { *n = nfcDiscovery{probeF: true} }

func nfcPoll(d *stDriver, f bool) nfcObservation {
	if f {
		k, t := d.pollNfcF()
		if k == stTag {
			return nfcObservation{stTag, nfcTagFromF(t)}
		}
		return nfcObservation{kind: k}
	}
	k, t := d.pollNfcA()
	if k == stTag {
		return nfcObservation{stTag, nfcTagFromA(t)}
	}
	return nfcObservation{kind: k}
}

// poll runs one discovery step. pollP polls NFC-F (true) or NFC-A; failed
// reports a driver error, which ends the step.
func (n *nfcDiscovery) poll(pollP func(f bool) nfcObservation, failed func() bool) nfcObservation {
	lock := func(f bool, r nfcObservation) nfcObservation {
		n.locked = 'A'
		if f {
			n.locked = 'F'
		}
		n.probeF, n.lockedNoTag, n.fCleanNoTag, n.fInconclusiv = f, 0, 0, 0
		return r
	}
	// a noisy FeliCa exchange may also be an NFC-A card: probe A once
	fallbackA := func() nfcObservation {
		if failed() {
			return nfcObservation{kind: stInconclusive}
		}
		r := pollP(false)
		if r.kind == stTag {
			return lock(false, r)
		}
		n.probeF, n.lockedNoTag, n.fCleanNoTag, n.fInconclusiv = true, 0, 0, 0
		return r
	}
	// keep the protocol while a tag stays, so reads are not interrupted
	if n.locked != 0 {
		f := n.locked == 'F'
		r := pollP(f)
		switch r.kind {
		case stTag:
			n.lockedNoTag, n.fCleanNoTag, n.fInconclusiv = 0, 0, 0
		case stInconclusive:
			n.lockedNoTag, n.fCleanNoTag = 0, 0
			if f && !failed() {
				n.fInconclusiv++
				if n.fInconclusiv >= 3 {
					n.locked = 0
					return fallbackA()
				}
			}
		case stNoTag:
			n.fInconclusiv = 0
			n.lockedNoTag++
			if n.lockedNoTag >= 3 {
				n.reset()
			}
		}
		return r
	}
	first := n.probeF
	r1 := pollP(first)
	if failed() {
		return r1
	}
	if r1.kind == stTag {
		return lock(first, r1)
	}
	// partial FeliCa evidence: stay on F for a few polls before trying A
	if first && r1.kind == stInconclusive {
		n.fInconclusiv++
		if n.fInconclusiv < 3 {
			n.probeF, n.fCleanNoTag = true, 0
			return r1
		}
		return fallbackA()
	}
	// two clean NFC-F misses before probing NFC-A (phones answer late)
	if first && r1.kind == stNoTag {
		n.fInconclusiv = 0
		n.fCleanNoTag++
		if n.fCleanNoTag < 2 {
			return r1
		}
	}
	r2 := pollP(!first)
	if r2.kind == stTag {
		return lock(!first, r2)
	}
	n.reset()
	if r1.kind == stInconclusive || r2.kind == stInconclusive {
		return nfcObservation{kind: stInconclusive}
	}
	return nfcObservation{kind: stNoTag}
}

// ---- tag snapshots ----

type nfcNDEF struct {
	Capacity int          `json:"capacity"`
	Readable bool         `json:"readable"`
	Size     int          `json:"size"`
	Records  []ndefRecord `json:"records"`
	Error    string       `json:"error,omitempty"`
}

type nfcTag struct {
	Session int64    `json:"session"`
	Present bool     `json:"present"`
	First   int64    `json:"first"`
	Last    int64    `json:"last"`
	Tech    string   `json:"tech"`
	UID     string   `json:"uid"`
	ATQA    string   `json:"atqa,omitempty"`
	SAK     *int     `json:"sak,omitempty"`
	Type    string   `json:"type"`
	PMm     string   `json:"pmm,omitempty"`
	Version string   `json:"version,omitempty"`
	Pages   int      `json:"pages,omitempty"`
	Memory  string   `json:"memory,omitempty"`
	NDEF    *nfcNDEF `json:"ndef,omitempty"`
}

func nfcHex(b []byte) string { return strings.ToUpper(hex.EncodeToString(b)) }

func nfcTagFromA(t *stNfcATag) *nfcTag {
	sak := int(t.SAK)
	tag := &nfcTag{Tech: "NFC-A", UID: nfcHex(t.UID), ATQA: nfcHex(t.ATQA[:]), SAK: &sak, Type: t.TypeName,
		Version: nfcHex(t.Version), Pages: t.Pages, Memory: nfcHex(t.Memory)}
	if t.NDEF {
		n := &nfcNDEF{Capacity: t.NDEFCap, Readable: t.NDEFRead, Size: len(t.NDEFBytes), Records: []ndefRecord{}}
		if len(t.NDEFBytes) > 0 {
			recs, err := parseNDEF(t.NDEFBytes)
			if err != nil {
				n.Error = err.Error()
			} else {
				n.Records = recs
			}
		}
		tag.NDEF = n
	}
	return tag
}

func nfcTagFromF(t *stNfcFTag) *nfcTag {
	return &nfcTag{Tech: "NFC-F", UID: nfcHex(t.IDm[:]), PMm: nfcHex(t.PMm[:]), Type: "FeliCa (Type 3)"}
}

func nfcSameIdentity(a, b *nfcTag) bool { return a.Tech == b.Tech && a.UID == b.UID }

func nfcSameSnapshot(a, b *nfcTag) bool {
	ja, _ := json.Marshal(nfcTag{Tech: a.Tech, UID: a.UID, ATQA: a.ATQA, SAK: a.SAK, Type: a.Type, PMm: a.PMm, Version: a.Version, Pages: a.Pages, Memory: a.Memory, NDEF: a.NDEF})
	jb, _ := json.Marshal(nfcTag{Tech: b.Tech, UID: b.UID, ATQA: b.ATQA, SAK: b.SAK, Type: b.Type, PMm: b.PMm, Version: b.Version, Pages: b.Pages, Memory: b.Memory, NDEF: b.NDEF})
	return string(ja) == string(jb)
}

// nfcPresence debounces observations into presented / updated / removed
// sessions (nfc_worker.cpp handleDiscoveryResult).
type nfcPresence struct {
	present     *nfcTag
	candidate   *nfcTag
	candConf    int
	noTagCount  int
	firstNoTag  time.Time
	nextSession int64
}

type nfcPresenceEvent struct {
	removed, presented, updated *nfcTag
}

func (p *nfcPresence) observe(o nfcObservation, now time.Time) nfcPresenceEvent {
	var ev nfcPresenceEvent
	switch o.kind {
	case stInconclusive:
		p.noTagCount = 0
		return ev
	case stNoTag:
		p.candidate, p.candConf = nil, 0
		if p.present != nil {
			if p.noTagCount == 0 {
				p.firstNoTag = now
			}
			p.noTagCount++
			if p.noTagCount >= nfcRemoveConfirm && now.Sub(p.firstNoTag) >= nfcRemoveDebounc {
				ev.removed = p.clear()
			}
		}
		return ev
	}
	t := o.tag
	p.noTagCount = 0
	if p.present != nil && nfcSameIdentity(p.present, t) {
		p.candidate, p.candConf = nil, 0
		p.present.Last = now.UnixMilli()
		if !nfcSameSnapshot(p.present, t) {
			t.Session, t.Present, t.First, t.Last = p.present.Session, true, p.present.First, now.UnixMilli()
			p.present = t
			ev.updated = t
		}
		return ev
	}
	if p.candidate != nil && nfcSameIdentity(p.candidate, t) {
		p.candConf++
	} else {
		p.candConf = 1
	}
	p.candidate = t
	if p.candConf < nfcPresentConfrm {
		return ev
	}
	if p.present != nil {
		ev.removed = p.clear()
	}
	p.nextSession++
	t.Session, t.Present, t.First, t.Last = p.nextSession, true, now.UnixMilli(), now.UnixMilli()
	p.present, p.candidate, p.candConf = t, nil, 0
	ev.presented = t
	return ev
}

func (p *nfcPresence) clear() *nfcTag {
	r := p.present
	if r != nil {
		r.Present = false
	}
	p.present, p.candidate, p.candConf, p.noTagCount = nil, nil, 0, 0
	return r
}

// ---- backend ----

type nfcReader struct {
	State      string      `json:"state"` // off, starting, scanning, paused, absent, busy, error
	Detail     string      `json:"detail,omitempty"`
	Scanning   bool        `json:"scanning"`
	Chip       *stChipInfo `json:"chip,omitempty"`
	Bus        string      `json:"bus,omitempty"`
	CS         string      `json:"cs,omitempty"`
	IRQ        string      `json:"irq,omitempty"`
	Polls      int64       `json:"polls"`
	Recoveries int64       `json:"recoveries"`
	Present    int64       `json:"present"`
}

type nfcBackend struct {
	mu      sync.Mutex
	hub     *hwHub
	w       *cc1101Worker
	reader  nfcReader
	tags    []*nfcTag // newest first
	session int64
}

func hwCC1101NFC(mux *http.ServeMux) func() {
	b := &nfcBackend{reader: nfcReader{State: "off", Scanning: true}}
	b.w = newCC1101Worker(b.run)
	b.hub = newHWHub(b.w.start, b.w.halt)
	snapshot := func() any {
		b.mu.Lock()
		defer b.mu.Unlock()
		return map[string]any{"reader": b.reader, "tags": b.tags}
	}
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		hwJSON(w, http.StatusOK, snapshot())
	})
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		b.hub.serve(w, r, func() (string, any) { return "snapshot", snapshot() })
	})
	mux.HandleFunc("POST /api/scan", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			On bool `json:"on"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
			hwError(w, http.StatusBadRequest, "bad request")
			return
		}
		b.mu.Lock()
		b.reader.Scanning = req.On
		b.mu.Unlock()
		b.w.send("scan")
		b.publishReader()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/retry", func(w http.ResponseWriter, r *http.Request) {
		b.w.send("retry")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/clear", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		kept := []*nfcTag{}
		for _, t := range b.tags {
			if t.Present {
				kept = append(kept, t)
			}
		}
		b.tags = kept
		b.mu.Unlock()
		b.hub.publish("tags", kept)
		w.WriteHeader(http.StatusNoContent)
	})
	return b.w.join
}

func (b *nfcBackend) setReader(fn func(r *nfcReader)) {
	b.mu.Lock()
	fn(&b.reader)
	b.mu.Unlock()
	b.publishReader()
}

func (b *nfcBackend) publishReader() {
	b.mu.Lock()
	r := b.reader
	b.mu.Unlock()
	b.hub.publish("reader", r)
}

func (b *nfcBackend) publishTag(t *nfcTag) {
	if t == nil {
		return
	}
	c := *t
	b.mu.Lock()
	found := false
	for i, x := range b.tags {
		if x.Session == c.Session {
			b.tags[i] = &c
			found = true
		}
	}
	if !found {
		b.tags = append([]*nfcTag{&c}, b.tags...)
		if len(b.tags) > nfcHistoryMax {
			b.tags = b.tags[:nfcHistoryMax]
		}
	}
	b.mu.Unlock()
	b.hub.publish("tag", c)
}

func nfcOpenError(err error) (string, string) {
	var idErr *stIdentityError
	switch {
	case errors.Is(err, errCapBusy):
		return "busy", ""
	case errors.As(err, &idErr):
		if idErr.id == 0x00 || idErr.id == 0xFF {
			return "absent", ""
		}
		return "error", err.Error()
	case errors.Is(err, os.ErrNotExist):
		return "absent", err.Error()
	}
	return "error", err.Error()
}

func (b *nfcBackend) run() {
	// the session numbering continues across window sessions
	pres := &nfcPresence{nextSession: b.session}
	defer func() { b.session = pres.nextSession }()
	contended := 0
	for !b.w.stop.Load() {
		b.setReader(func(r *nfcReader) { r.State, r.Detail = "starting", "" })
		hw, err := openNFCHW(&b.w.stop)
		var chip stChipInfo
		if err == nil {
			chip, err = hw.drv.initialize()
			if err != nil {
				hw.close()
			}
		}
		if err != nil {
			if errors.Is(err, errSTCancelled) {
				break
			}
			if errors.Is(err, errSTContended) && contended < 5 {
				contended++ // LCD traffic during the probe: just probe again
				b.w.wait(100 * time.Millisecond)
				continue
			}
			contended = 0
			st, detail := nfcOpenError(err)
			log.Printf("nfc: %s %v", st, err)
			b.setReader(func(r *nfcReader) { r.State, r.Detail, r.Chip = st, detail, nil })
			// wait for Retry (busy and absent also re-check every few seconds)
			retry := 0 * time.Second
			if st == "busy" || st == "absent" {
				retry = 5 * time.Second
			}
			for !b.w.stop.Load() {
				d := 24 * time.Hour
				if retry > 0 {
					d = retry
				}
				c := b.w.wait(d)
				if c == "retry" || (c == "" && retry > 0) {
					break
				}
			}
			continue
		}
		hw.chip, contended = chip, 0
		log.Printf("nfc: ST25R3916 identity 0x%02X rev %d on %s (CS %s)", chip.Identity, chip.Revision, hw.bus, hw.csS)
		b.setReader(func(r *nfcReader) {
			r.Chip, r.Bus, r.CS, r.IRQ, r.Detail = &chip, hw.bus, hw.csS, fmt.Sprintf("GPIO%d", nfcIRQGPIO), ""
		})
		failed := b.scan(hw, pres)
		if ev := pres.clear(); ev != nil {
			b.publishTag(ev)
		}
		hw.close()
		if failed != "" {
			b.setReader(func(r *nfcReader) { r.State, r.Detail, r.Present = "error", failed, 0 })
			for !b.w.stop.Load() && b.w.wait(24*time.Hour) != "retry" {
			}
		}
	}
	b.setReader(func(r *nfcReader) { r.State, r.Present = "off", 0 })
}

// scan runs discovery until stopped; it returns an error text when the
// reader failed and needs a manual retry.
func (b *nfcBackend) scan(hw *nfcHW, pres *nfcPresence) string {
	d := hw.drv
	disc := &nfcDiscovery{}
	disc.reset()
	active := false
	for !b.w.stop.Load() {
		b.mu.Lock()
		want := b.reader.Scanning
		b.mu.Unlock()
		if !want {
			if active {
				d.fieldOff()
				active = false
				if ev := pres.clear(); ev != nil {
					b.publishTag(ev)
				}
			}
			b.setReader(func(r *nfcReader) { r.State, r.Present = "paused", 0 })
			if c := b.w.wait(24 * time.Hour); c == "retry" {
				return ""
			}
			continue
		}
		if !active {
			if err := d.startDiscovery(true); err != nil {
				if errors.Is(err, errSTCancelled) {
					return ""
				}
				if !b.recover(hw, err) {
					return err.Error()
				}
				continue
			}
			active = true
			disc.reset()
			b.setReader(func(r *nfcReader) { r.State = "scanning" })
		}
		began := time.Now()
		obs := disc.poll(func(f bool) nfcObservation { return nfcPoll(d, f) }, func() bool { return d.err != nil })
		if err := d.err; err != nil {
			if errors.Is(err, errSTCancelled) {
				return ""
			}
			obs = nfcObservation{kind: stInconclusive}
			if !b.recover(hw, err) {
				return err.Error()
			}
			active = false
		}
		b.mu.Lock()
		b.reader.Polls++
		b.mu.Unlock()
		ev := pres.observe(obs, time.Now())
		b.publishTag(ev.removed)
		b.publishTag(ev.presented)
		b.publishTag(ev.updated)
		if ev.removed != nil || ev.presented != nil {
			var cur int64
			if pres.present != nil {
				cur = pres.present.Session
			}
			b.setReader(func(r *nfcReader) { r.Present = cur })
		}
		cycle := nfcMinCycle
		if pres.present != nil {
			cycle = nfcPresentCycle
		}
		if left := cycle - time.Since(began); left > 0 {
			if c := b.w.wait(left); c == "retry" {
				return ""
			}
		}
	}
	return ""
}

// recover re-initialises the chip after a contended frame or a lost
// exchange. It gives up after repeated failures in a row.
func (b *nfcBackend) recover(hw *nfcHW, cause error) bool {
	for attempt := 0; attempt < 3 && !b.w.stop.Load(); attempt++ {
		b.mu.Lock()
		b.reader.Recoveries++
		b.mu.Unlock()
		if _, err := hw.drv.initialize(); err == nil {
			return true
		} else if errors.Is(err, errSTCancelled) {
			return true
		} else {
			cause = err
		}
	}
	log.Printf("nfc: reader lost: %v", cause)
	return b.w.stop.Load()
}

func init() { hwApps["cap-cc1101-nfc"] = hwCC1101NFC }
