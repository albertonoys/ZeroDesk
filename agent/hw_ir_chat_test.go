package main

import "testing"

func irChatFeed(d *irChatDecoder, durs []uint32) (frames []*irChatFrame) {
	for i, v := range durs {
		if f := d.feed(irPulse{Pulse: i%2 == 0, Us: v}); f != nil {
			frames = append(frames, f)
		}
	}
	return
}

func TestIRChatCRC(t *testing.T) {
	if c := irChatCRC16([]byte("123456789")); c != 0x29b1 {
		t.Fatalf("CRC-16/CCITT-FALSE check value: %#x", c)
	}
}

func TestIRChatRoundTrip(t *testing.T) {
	d, err := irChatEncode(0x1234, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 3+(6+5+2)*16 || d[0] != 9000 || d[1] != 4500 || d[len(d)-1] != 560 {
		t.Fatalf("waveform shape: %d", len(d))
	}
	var dec irChatDecoder
	fs := irChatFeed(&dec, append([]uint32{1200, 800}, d...)) // noise first
	if len(fs) != 1 || string(fs[0].Payload) != "hello" || fs[0].Seq != 0x1234 || fs[0].Samples != len(d) {
		t.Fatalf("decode: %+v", fs)
	}
	// every printable byte, three sequences, max length
	for c := byte(0x20); c <= 0x7e; c++ {
		for _, seq := range []uint16{0, 1, 0xffff} {
			p := []byte{c, c, c, c, c, c, c}
			d, _ := irChatEncode(seq, p)
			fs := irChatFeed(&dec, d)
			if len(fs) != 1 || string(fs[0].Payload) != string(p) || fs[0].Seq != seq {
				t.Fatalf("byte %#x seq %d", c, seq)
			}
		}
	}
	// airtime of the longest frame stays under the original's 250 ms budget
	d, _ = irChatEncode(1, []byte("~~~~~~~"))
	var sum uint32
	for _, v := range d {
		sum += v
	}
	if len(d) > 243 || sum > 250000 {
		t.Fatalf("frame %d events %d us", len(d), sum)
	}
}

func TestIRChatRejects(t *testing.T) {
	for _, p := range []string{"", "12345678", "a\nb"} {
		if _, err := irChatEncode(1, []byte(p)); err == nil {
			t.Fatalf("payload %q accepted", p)
		}
	}
	d, _ := irChatEncode(7, []byte("bad"))
	i := 2 + (6+3)*16 + 1 // first CRC bit space
	if d[i] == irChatZeroSpace {
		d[i] = irChatOneSpace
	} else {
		d[i] = irChatZeroSpace
	}
	var dec irChatDecoder
	if fs := irChatFeed(&dec, d); len(fs) != 0 || dec.stats.CRCErrors != 1 {
		t.Fatalf("corrupt frame: %d frames, %+v", len(fs), dec.stats)
	}
	good, _ := irChatEncode(8, []byte("ok"))
	if fs := irChatFeed(&dec, good); len(fs) != 1 || string(fs[0].Payload) != "ok" {
		t.Fatal("decoder must recover after a CRC error")
	}
	// an NEC remote code is not a chat frame
	var n []uint32
	for _, p := range necPulses(0xF708FB04) {
		n = append(n, p.Us)
	}
	if fs := irChatFeed(&dec, n); len(fs) != 0 || dec.stats.Malformed == 0 {
		t.Fatal("NEC code accepted as chat")
	}
}
