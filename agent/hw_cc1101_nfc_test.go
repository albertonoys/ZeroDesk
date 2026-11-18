package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// ---- a register-level ST25R3916 with one NTAG213 in its field ----

type fakeNTAG struct {
	uid    [7]byte
	mem    []byte // 45 pages
	halted bool
}

type fakeST struct {
	regs, regB [64]byte
	irqs       uint32
	fifo       []byte
	txFrame    []byte
	tag        *fakeNTAG
	sent       [][]byte // frames sent to the tag
	xfers      int
	failAt     int
}

func (f *fakeST) irq() bool { return f.irqs != 0 }
func (f *fakeST) waitIRQ(d time.Duration) bool {
	if f.irqs != 0 {
		return true
	}
	time.Sleep(min(d, time.Millisecond))
	return false
}

func (f *fakeST) xfer(tx []byte) ([]byte, error) {
	f.xfers++
	if f.failAt > 0 && f.xfers == f.failAt {
		return nil, errSTContended
	}
	rx := make([]byte, len(tx))
	op := tx[0]
	switch {
	case op == stCmdSpaceB:
		if tx[1]&0x40 != 0 {
			rx[2] = f.regB[tx[1]&0x3F]
		} else {
			f.regB[tx[1]&0x3F] = tx[2]
		}
	case op == stCmdTestAccess:
	case op == stReadFIFO:
		n := min(len(tx)-1, len(f.fifo))
		copy(rx[1:], f.fifo[:n])
		f.fifo = f.fifo[n:]
	case op&0xC0 == 0xC0:
		f.command(op)
	case op&0xC0 == 0x80:
		f.txFrame = append([]byte(nil), tx[1:]...)
	case op&0xC0 == 0x40:
		reg := op & 0x3F
		for i := 1; i < len(tx); i++ {
			rx[i] = f.read(reg)
			reg++
		}
	default:
		reg := op & 0x3F
		for i := 1; i < len(tx); i++ {
			f.regs[reg] = tx[i]
			if reg == stRegOpControl && tx[i]&stOpOscillator != 0 {
				f.irqs |= stIRQOsc
			}
			reg++
		}
	}
	return rx, nil
}

func (f *fakeST) read(reg byte) byte {
	take := func(shift uint) byte {
		v := byte(f.irqs >> shift)
		f.irqs &^= 0xFF << shift
		return v
	}
	switch reg {
	case 0x1A:
		return take(24)
	case 0x1B:
		return take(16)
	case 0x1C:
		return take(8)
	case 0x1D:
		return take(0)
	case 0x1E:
		return byte(len(f.fifo))
	case 0x1F:
		return byte(len(f.fifo)>>8) << 6
	case stRegAuxDisplay:
		return stAuxDispOscOK | stAuxDispTxOn
	case stRegICIdentity:
		return 0x2A
	}
	return f.regs[reg]
}

func (f *fakeST) command(c byte) {
	switch c {
	case stCmdClearFIFO:
		f.fifo = nil
	case stCmdInitialFieldOn:
		f.regs[stRegOpControl] |= stOpTx
		f.irqs |= stIRQGuard
	case stCmdTxWUPA:
		f.respond([]byte{0x52})
	case stCmdTxWithCRC, stCmdTxWithoutCRC:
		n := int(uint16(f.regs[stRegTxBytes1])<<8|uint16(f.regs[stRegTxBytes1+1])) >> 3
		f.respond(f.txFrame[:min(n, len(f.txFrame))])
	}
}

func (f *fakeST) respond(frame []byte) {
	f.sent = append(f.sent, append([]byte(nil), frame...))
	f.irqs |= stIRQTxEnd
	var r []byte
	if f.tag != nil {
		r = f.tag.answer(frame)
	}
	if r == nil {
		f.irqs |= stIRQNoResp
		return
	}
	f.fifo = r
	f.irqs |= stIRQRxStart | stIRQRxEnd
}

func (t *fakeNTAG) answer(frame []byte) []byte {
	crc := []byte{0xAA, 0xBB}
	u := t.uid
	switch {
	case len(frame) == 1 && frame[0] == 0x52:
		t.halted = false
		return []byte{0x44, 0x00}
	case t.halted:
		return nil
	case bytes.Equal(frame, []byte{0x93, 0x20}):
		return []byte{0x88, u[0], u[1], u[2], 0x88 ^ u[0] ^ u[1] ^ u[2]}
	case len(frame) == 7 && frame[0] == 0x93 && frame[1] == 0x70:
		return append([]byte{0x04}, crc...)
	case bytes.Equal(frame, []byte{0x95, 0x20}):
		return []byte{u[3], u[4], u[5], u[6], u[3] ^ u[4] ^ u[5] ^ u[6]}
	case len(frame) == 7 && frame[0] == 0x95 && frame[1] == 0x70:
		return append([]byte{0x00}, crc...)
	case bytes.Equal(frame, []byte{0x60}):
		return append([]byte{0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x0F, 0x03}, crc...)
	case len(frame) == 2 && frame[0] == 0x30:
		p := int(frame[1])
		if p >= len(t.mem)/4 {
			return nil
		}
		var out []byte
		for i := range 4 {
			q := (p + i) % (len(t.mem) / 4) // READ rolls over
			out = append(out, t.mem[q*4:q*4+4]...)
		}
		return append(out, crc...)
	case bytes.Equal(frame, []byte{0x50, 0x00}):
		t.halted = true
	}
	return nil
}

func testNDEFMessage() []byte {
	b, _ := hex.DecodeString("9101085402656e68656c6c6f51010c55046578616d706c652e636f6d")
	return b
}

func newFakeNTAG() *fakeNTAG {
	t := &fakeNTAG{uid: [7]byte{0x04, 0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6}, mem: make([]byte, 45*4)}
	copy(t.mem, t.uid[:3])
	copy(t.mem[4:], t.uid[3:])
	copy(t.mem[12:], []byte{0xE1, 0x10, 0x12, 0x00})
	msg := testNDEFMessage()
	tlv := append([]byte{0x03, byte(len(msg))}, msg...)
	copy(t.mem[16:], append(tlv, 0xFE))
	return t
}

func TestST25R3916ReadsNTAG(t *testing.T) {
	f := &fakeST{tag: newFakeNTAG()}
	var stop atomic.Bool
	d := newSTDriver(f, &stop)
	chip, err := d.initialize()
	if err != nil {
		t.Fatal(err)
	}
	if chip.Identity != 0x2A || chip.Revision != 2 {
		t.Fatalf("chip %+v", chip)
	}
	if err := d.startDiscovery(true); err != nil {
		t.Fatal(err)
	}
	disc := &nfcDiscovery{}
	disc.reset()
	pres := &nfcPresence{}
	var got *nfcTag
	now := time.Now()
	for i := 0; i < 8 && got == nil; i++ {
		obs := disc.poll(func(fl bool) nfcObservation { return nfcPoll(d, fl) }, func() bool { return d.err != nil })
		if d.err != nil {
			t.Fatal(d.err)
		}
		got = pres.observe(obs, now).presented
	}
	if got == nil {
		t.Fatal("tag never presented")
	}
	if got.UID != "04A1B2C3D4E5F6" || got.ATQA != "4400" || *got.SAK != 0 || got.Type != "NTAG213" || got.Tech != "NFC-A" {
		t.Fatalf("tag %+v", got)
	}
	if got.Pages != 45 || len(got.Memory) != 45*8 {
		t.Fatalf("memory pages %d len %d", got.Pages, len(got.Memory))
	}
	if got.NDEF == nil || got.NDEF.Capacity != 144 || !got.NDEF.Readable || len(got.NDEF.Records) != 2 {
		t.Fatalf("ndef %+v", got.NDEF)
	}
	if r := got.NDEF.Records; r[0].Value != "hello" || r[0].Lang != "en" || r[1].Value != "https://example.com" {
		t.Fatalf("records %+v", r)
	}
	// read only: every frame sent to the tag is a reader command, never a write
	allowed := map[byte]bool{0x52: true, 0x93: true, 0x95: true, 0x97: true, 0x60: true, 0x30: true, 0x50: true, 0x00: true}
	for _, fr := range f.sent {
		if !allowed[fr[0]] {
			t.Fatalf("unexpected frame % X", fr)
		}
	}
	// a cached tag is not re-read on the next poll
	n := len(f.sent)
	disc.poll(func(fl bool) nfcObservation { return nfcPoll(d, fl) }, func() bool { return false })
	for _, fr := range f.sent[n:] {
		if fr[0] == 0x30 || fr[0] == 0x60 {
			t.Fatalf("re-read cached tag: % X", fr)
		}
	}
}

func TestST25R3916NoTagAndContention(t *testing.T) {
	f := &fakeST{}
	var stop atomic.Bool
	d := newSTDriver(f, &stop)
	if _, err := d.initialize(); err != nil {
		t.Fatal(err)
	}
	_ = d.startDiscovery(false)
	if k, _ := d.pollNfcA(); k != stNoTag || d.err != nil {
		t.Fatalf("kind %v err %v", k, d.err)
	}
	f.failAt = f.xfers + 3
	if k, _ := d.pollNfcA(); k == stTag || !errors.Is(d.err, errSTContended) {
		t.Fatalf("contention not reported: %v", d.err)
	}
	// an absent reader reads as zeros
	z := &fakeST{}
	zd := newSTDriver(stZero{z}, &stop)
	var idErr *stIdentityError
	if _, err := zd.initialize(); !errors.As(err, &idErr) || idErr.id != 0 {
		t.Fatalf("absent: %v", err)
	}
}

type stZero struct{ *fakeST }

func (z stZero) xfer(tx []byte) ([]byte, error) { return make([]byte, len(tx)), nil }

func TestNfcDiscoveryPrefersFeliCaThenLocks(t *testing.T) {
	var calls []string
	script := map[bool][]stPollKind{true: {stNoTag, stNoTag, stNoTag}, false: {stTag, stTag, stNoTag, stNoTag, stNoTag}}
	pollP := func(f bool) nfcObservation {
		name := "A"
		if f {
			name = "F"
		}
		calls = append(calls, name)
		k := stNoTag
		if q := script[f]; len(q) > 0 {
			k, script[f] = q[0], q[1:]
		}
		if k == stTag {
			return nfcObservation{stTag, &nfcTag{Tech: "NFC-" + name, UID: "01"}}
		}
		return nfcObservation{kind: k}
	}
	n := &nfcDiscovery{}
	n.reset()
	ok := func() bool { return false }
	kinds := []stPollKind{}
	for range 6 {
		kinds = append(kinds, n.poll(pollP, ok).kind)
	}
	want := "F F A A A A A"
	if got := join(calls); got != want {
		t.Fatalf("calls %q want %q", got, want)
	}
	if kinds[1] != stTag || n.locked != 0 {
		t.Fatalf("kinds %v locked %c", kinds, n.locked)
	}
}

func join(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += " "
		}
		out += v
	}
	return out
}

func TestNfcPresenceDebounce(t *testing.T) {
	p := &nfcPresence{}
	t0 := time.Unix(1000, 0)
	tag := func() *nfcTag { return &nfcTag{Tech: "NFC-A", UID: "AA"} }
	if ev := p.observe(nfcObservation{stTag, tag()}, t0); ev.presented != nil {
		t.Fatal("presented after one observation")
	}
	ev := p.observe(nfcObservation{stTag, tag()}, t0)
	if ev.presented == nil || ev.presented.Session != 1 || !ev.presented.Present {
		t.Fatalf("not presented: %+v", ev)
	}
	p.observe(nfcObservation{kind: stInconclusive}, t0)
	for i := range 3 {
		if ev := p.observe(nfcObservation{kind: stNoTag}, t0.Add(time.Duration(i)*50*time.Millisecond)); ev.removed != nil {
			t.Fatal("removed before the debounce time")
		}
	}
	ev = p.observe(nfcObservation{kind: stNoTag}, t0.Add(200*time.Millisecond))
	if ev.removed == nil || ev.removed.Present {
		t.Fatalf("not removed: %+v", ev)
	}
}

func TestNDEFParser(t *testing.T) {
	recs, err := parseNDEF(testNDEFMessage())
	if err != nil || len(recs) != 2 || recs[0].Kind != "text" || recs[1].Kind != "uri" {
		t.Fatalf("%v %+v", err, recs)
	}
	mime, _ := hex.DecodeString("d20a0b746578742f7663617264424547494e3a5643415244")
	if r, err := parseNDEF(mime); err != nil || r[0].Kind != "mime" || r[0].Type != "text/vcard" || r[0].Value != "BEGIN:VCARD" {
		t.Fatalf("mime %v %+v", err, r)
	}
	for _, bad := range []string{"", "11010154", "b10101540061", "910101540051"} {
		b, _ := hex.DecodeString(bad)
		if _, err := parseNDEF(b); err == nil {
			t.Fatalf("%s parsed", bad)
		}
	}
}

func TestType2Helpers(t *testing.T) {
	mem := newFakeNTAG().mem
	ok, ro, capacity, msg := stType2NDEF(mem)
	if !ok || !ro || capacity != 144 || !bytes.Equal(msg, testNDEFMessage()) {
		t.Fatalf("%v %v %d % X", ok, ro, capacity, msg)
	}
	for v, want := range map[string]int{"0004040201000f03": 45, "0004040201001103": 135, "0004040201001303": 231, "0004030101000b03": 20} {
		b, _ := hex.DecodeString(v)
		if n, _ := stType2Pages(b); n != want {
			t.Fatalf("%s: %d", v, n)
		}
	}
	if stNoResponseTimer(4*time.Millisecond, false) != 848 || stBCC([]byte{0x88, 0x04, 0xA1, 0xB2, 0x88 ^ 0x04 ^ 0xA1 ^ 0xB2}) != 0 {
		t.Fatal("timer or BCC")
	}
	res, _ := hex.DecodeString("120101020304050607080a0b0c0d0e0f1011")
	if tag, ok := stParseSensfRes(res); !ok || nfcHex(tag.IDm[:]) != "0102030405060708" || nfcHex(tag.PMm[:]) != "0A0B0C0D0E0F1011" {
		t.Fatal("SENSF_RES")
	}
}

func TestParsePinctrl(t *testing.T) {
	cases := map[string]string{
		"14: a5    -- | hi // GPIO14 = TXD1":   "set 14 a5",
		"22: ip    -- | lo // GPIO22 = input":  "set 22 ip",
		"26: op -- -- | hi // GPIO26 = output": "set 26 op dh",
		"24: op dl pd | lo // GPIO24 = output": "set 24 op dl pd",
	}
	for in, want := range cases {
		var pin int
		_, _ = fmtSscan(in, &pin)
		args, err := parsePinctrl(in, pin)
		if err != nil || join(args) != want {
			t.Fatalf("%q: %v %v", in, args, err)
		}
	}
}

func fmtSscan(s string, pin *int) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	*pin = n
	return 1, nil
}
