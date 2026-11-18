package main

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// jitter adds ±80 µs like the original's dummy signal generator.
func irJitter(p []irPulse, seed int64) []irPulse {
	r := rand.New(rand.NewSource(seed))
	out := make([]irPulse, len(p))
	for i, x := range p {
		out[i] = irPulse{x.Pulse, uint32(int(x.Us) + r.Intn(161) - 80)}
	}
	return out
}

func necPulses(data uint32) []irPulse {
	p := []irPulse{{true, 9000}, {false, 4500}}
	for i := range 32 {
		s := uint32(560)
		if data&(1<<i) != 0 {
			s = 1690
		}
		p = append(p, irPulse{true, 560}, irPulse{false, s})
	}
	return append(p, irPulse{true, 560})
}

func TestIRDecodeNEC(t *testing.T) {
	// leading idle gaps as rc-core reports them, then an NEC frame
	sig := append([]irPulse{{false, 16777215}, {false, 404251}}, irJitter(necPulses(0xF708FB04), 1)...)
	d := irDecodeSignal(sig)
	if d.Protocol != "NEC" || d.Address != 0x04 || d.Command != 0x08 || d.Bits != 32 || d.Confidence != 96 {
		t.Fatalf("NEC: %+v", d)
	}
	d = irDecodeSignal(irJitter(necPulses(0xBF40_1234), 2))
	if d.Protocol != "NEC Ext" || d.Address != 0x1234 || d.Command != 0x40 {
		t.Fatalf("NEC Ext: %+v", d)
	}
	bad := necPulses(0x00000000) // command check byte wrong
	if d := irDecodeSignal(bad); d.Decoded {
		t.Fatalf("invalid NEC decoded: %+v", d)
	}
}

func TestIRDecodeSony(t *testing.T) {
	data := uint32(0x15 | 1<<7) // TV power: command 21, address 1
	p := []irPulse{{true, 2400}, {false, 600}}
	for i := range 12 {
		m := uint32(600)
		if data&(1<<i) != 0 {
			m = 1200
		}
		p = append(p, irPulse{true, m}, irPulse{false, 600})
	}
	d := irDecodeSignal(irJitter(p, 3))
	if d.Protocol != "Sony SIRC" || d.Address != 1 || d.Command != 0x15 || d.Bits != 12 {
		t.Fatalf("Sony: %+v", d)
	}
}

func TestIRDecodeRC5AndRC6(t *testing.T) {
	// the original's Manchester reader: 14 pulse-then-gap halves
	var p []irPulse
	for range 14 {
		p = append(p, irPulse{true, 889}, irPulse{false, 889})
	}
	if d := irDecodeSignal(p); d.Protocol != "RC5" || d.Address != 0x1f || d.Command != 0x3f {
		t.Fatalf("RC5: %+v", d)
	}
	p = []irPulse{{true, 2666}, {false, 889}}
	for i := range 20 {
		h := uint32(889)
		if i == 3 {
			h = 1778 // trailer bit is twice as long
		}
		p = append(p, irPulse{true, h}, irPulse{false, h})
	}
	if d := irDecodeSignal(p); d.Protocol != "RC6" || d.Bits != 20 {
		t.Fatalf("RC6: %+v", d)
	}
	if d := irDecodeSignal([]irPulse{{true, 300}, {false, 300}}); d.Decoded || d.Protocol != "Unknown" {
		t.Fatalf("noise: %+v", d)
	}
}

func TestIRFileFormat(t *testing.T) {
	// a file as the LCD app writes it (no remote key)
	orig := "M5CardputerZero IR Remote signal v1\nname=TV\\nPower\ncreated_unix_sec=1763400000\ncarrier_hz=38000\n" +
		"duty_cycle_percent=33\nsource_driver=gpio_ir_recv\nsource_device=/dev/lirc1\nraw_us=+9000,-4500,+560,x,-0,+560\n"
	f, sig, ok := irParseFile("1763400000_TV_Power.ir", []byte(orig))
	if !ok || f.Name != "TV\nPower" || f.Remote != "" || f.Created != 1763400000 || f.Driver != "gpio_ir_recv" || len(sig.Pulses) != 4 || f.Duration != 9000+4500+560+560 {
		t.Fatalf("parse: %+v %+v", f, sig)
	}
	f.Remote = "Living Room"
	f2, sig2, ok := irParseFile(f.ID, []byte(irFormatFile(f, sig)))
	if !ok || f2.Remote != "Living Room" || f2.Name != f.Name || irRawText(sig2.Pulses) != "+9000,-4500,+560,+560" {
		t.Fatalf("round trip: %+v %s", f2, irRawText(sig2.Pulses))
	}
	if _, _, ok := irParseFile("x.ir", []byte("not a signal\nraw_us=+1\n")); ok {
		t.Fatal("bad header accepted")
	}
	if got := irSanitize(" .My TV: Power!. "); got != "My_TV_Power" {
		t.Fatalf("sanitize %q", got)
	}
	if irSanitize("电视") != "signal" || !irValidID("1_a.IR") || irValidID("../x.ir") || irValidID(".x.ir") {
		t.Fatal("ids")
	}
	dir := t.TempDir()
	p1 := irUniquePath(dir, "a", 5)
	os.WriteFile(p1, nil, 0o644)
	if filepath.Base(p1) != "5_a.ir" || filepath.Base(irUniquePath(dir, "a", 5)) != "5_a_001.ir" {
		t.Fatal("unique path")
	}
}

func TestIRCapture(t *testing.T) {
	var c irCapture
	now := time.Now()
	words := []uint32{lircSpace | 0xffffff, lircPulse | 9000, lircSpace | 4500, lircPulse | 560}
	if done, _ := c.feed(words, now); done || len(c.pulses) != 3 || !c.pulses[0].Pulse {
		t.Fatalf("capture %+v", c.pulses)
	}
	if done, err := c.feed([]uint32{lircTimeout | 100000}, now); !done || err != "" {
		t.Fatal("timeout report must end the capture")
	}
	var e irCapture
	if done, _ := e.feed([]uint32{lircTimeout | 100000}, now); done {
		t.Fatal("timeout before any mark must keep waiting")
	}
	if done, err := e.feed([]uint32{lircOverflow}, now); !done || err != "overflow" {
		t.Fatal("overflow")
	}
	if s := irSigned([]irPulse{{true, 5}, {false, 7}}); s[0] != 5 || s[1] != -7 {
		t.Fatal("signed")
	}
	if !strings.Contains(irFormatFile(irFile{Name: "a"}, irSignal{Pulses: []irPulse{{true, 1}}}), "raw_us=+1\n") {
		t.Fatal("format")
	}
}
