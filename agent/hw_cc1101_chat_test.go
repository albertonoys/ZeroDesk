package main

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"
)

// a CC1101 at the SPI level: config registers, strobes, status reads, FIFOs
type fakeCC struct {
	regs     [0x40]byte
	marc     byte
	rx, tx   []byte
	sent     [][]byte
	strobes  []byte
	version  byte
	patable0 byte
}

func (f *fakeCC) transfer(tx []byte) ([]byte, error) {
	rx := make([]byte, len(tx))
	a := tx[0]
	reg := a & 0x3F
	if len(tx) == 1 && reg >= 0x30 { // strobe
		f.strobes = append(f.strobes, a)
		switch a {
		case ccSRES:
			f.regs = [0x40]byte{}
			f.marc = ccStateIDLE
		case ccSIDLE:
			f.marc = ccStateIDLE
		case ccSRX:
			f.marc = ccStateRX
		case ccSFSTXON:
			f.marc = ccStateFSTX
		case ccSTX:
			f.sent = append(f.sent, f.tx)
			f.tx = nil
			f.marc = ccStateIDLE
		case ccSFTX:
			f.tx = nil
		}
		return rx, nil
	}
	read, burst := a&ccRead != 0, a&ccBurst != 0
	switch {
	case read && reg == ccFIFO:
		for i := 1; i < len(tx); i++ {
			if len(f.rx) > 0 {
				rx[i], f.rx = f.rx[0], f.rx[1:]
			}
		}
	case read && burst && reg >= 0x30 && reg < 0x3E:
		switch reg {
		case ccVERSION:
			rx[1] = f.version
		case ccMARCSTATE:
			rx[1] = f.marc
		case ccRXBYTES:
			rx[1] = byte(len(f.rx))
		case ccTXBYTES:
			rx[1] = byte(len(f.tx))
		}
	case read:
		copy(rx[1:], f.regs[reg:])
	case reg == ccFIFO:
		f.tx = append(f.tx, tx[1:]...)
	case reg == ccPATABLE:
		f.patable0 = tx[1]
	default:
		copy(f.regs[reg:], tx[1:])
	}
	return rx, nil
}

func TestCC1101ProfileAndPackets(t *testing.T) {
	f := &fakeCC{version: 0x14}
	r := &ccRadio{spi: f}
	if err := r.begin(); err != nil {
		t.Fatal(err)
	}
	if r.relaxed || r.version != 0x14 {
		t.Fatalf("version %02X relaxed %v", r.version, r.relaxed)
	}
	want := map[byte]byte{0x04: 0x12, 0x05: 0xAD, 0x07: 0x64, 0x08: 0x05, 0x0D: 0x21, 0x0E: 0x62, 0x0F: 0x76, 0x10: 0xF6,
		0x11: 0x83, 0x12: 0x02, 0x15: 0x40, 0x17: 0x3C, 0x18: 0x14, 0x00: ccGDO2Ant868}
	for reg, v := range want {
		if f.regs[reg] != v {
			t.Fatalf("reg %02X = %02X want %02X", reg, f.regs[reg], v)
		}
	}
	if f.patable0 != 0xC2 {
		t.Fatalf("PATABLE %02X", f.patable0)
	}
	if err := r.startReceive(); err != nil || f.marc != ccStateRX || f.regs[ccIOCFG0] != 0x01 || f.regs[ccFIFOTHR] != 0x0F {
		t.Fatalf("rx: %v marc %02X", err, f.marc)
	}
	// a received frame: length, payload, RSSI, LQI|CRC_OK
	frame, _ := chatEncodeData(0x123456, []byte("hi"), "pi")
	f.rx = append(append([]byte{byte(len(frame))}, frame...), 0xEC, 0x80|0x2A)
	p, err := r.receive(50 * time.Millisecond)
	if err != nil || p == nil || !bytes.Equal(p.data, frame) || !p.crc || p.lqi != 0x2A || p.rssi != -84 {
		t.Fatalf("receive %v %+v", err, p)
	}
	if p, err := r.receive(20 * time.Millisecond); p != nil || err != nil {
		t.Fatal("phantom packet")
	}
	// transmit loads length + payload and strobes STX
	start := time.Now()
	if err := r.transmit(chatEncodeAck(0x123456)); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 || !bytes.Equal(f.sent[0], []byte{5, 0xC3, 0xA1, 0x12, 0x34, 0x56}) {
		t.Fatalf("sent % X", f.sent)
	}
	if time.Since(start) < ccAirtime(5) {
		t.Fatal("did not wait for the airtime")
	}
	if (&ccRadio{spi: &fakeCC{}}).begin() != errCCAbsent {
		t.Fatal("absent chip not reported")
	}
}

func TestChatProtocol(t *testing.T) {
	f, err := chatEncodeData(0x123456, []byte("hi"), "pi")
	if err != nil || hex.EncodeToString(f) != "c3d1123456827069"+"6869" {
		t.Fatalf("%v % X", err, f)
	}
	d := chatDecode(f)
	if d.kind != chatData || d.token != 0x123456 || d.sender != "pi" || string(d.payload) != "hi" {
		t.Fatalf("%+v", d)
	}
	// the name is dropped when it does not fit next to a long message
	long := bytes.Repeat([]byte{'x'}, 56)
	f, _ = chatEncodeData(1, long, "pi")
	if d := chatDecode(f); d.sender != "" || len(d.payload) != 56 || len(f) != ccMaxPayload {
		t.Fatalf("long: %+v", d)
	}
	if d := chatDecode(chatEncodeAck(7)); d.kind != chatAck || d.token != 7 {
		t.Fatal("ack")
	}
	if d := chatDecode([]byte("hello")); d.kind != chatLegacy {
		t.Fatal("legacy")
	}
	for _, bad := range [][]byte{{0xC3, 0xD1, 0, 0, 0, 'a'}, {0xC3, 0xA1, 0, 0, 1, 9}, {0xC3, 0xD1, 0, 0, 1}} {
		if chatDecode(bad).kind != chatMalformed {
			t.Fatalf("% X not malformed", bad)
		}
	}
	if _, err := chatEncodeData(0, []byte("x"), ""); err == nil {
		t.Fatal("zero token accepted")
	}
	if chatText([]byte("a\x01b")) != "a.b" || ccRSSI(0x80) != -138 || ccRSSI(0x7F) != -10.5 {
		t.Fatal("text or RSSI")
	}
	if ccFreqWord(868) != 0x216276 || ccMCSM1(0x30) != 0x3C {
		t.Fatal("frequency word or MCSM1")
	}
	for a := 1; a < 5; a++ {
		if b := chatBackoff(0xABCDEF, a); b < 80*time.Millisecond || b > 200*time.Millisecond {
			t.Fatalf("backoff %v", b)
		}
	}
	if !chatValidName("Alice") || chatValidName("   ") || chatValidName("toolongname") || chatValidName("名字") {
		t.Fatal("nickname validation")
	}
}
