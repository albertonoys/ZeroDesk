package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestLoraMessageID(t *testing.T) {
	// FNV-1a 32-bit reference values
	if got := loraMessageID(""); got != 0x811c9dc5 {
		t.Fatalf("empty: %08x", got)
	}
	if got := loraMessageID("a"); got != 0xe40c292c {
		t.Fatalf("a: %08x", got)
	}
	if got := loraMessageID("foobar"); got != 0xbf9cf968 {
		t.Fatalf("foobar: %08x", got)
	}
}

func TestLoraEncodeDecode(t *testing.T) {
	p := loraEncode("hello", "pi", "")
	want := append([]byte{0x82}, "pihello"...)
	if !bytes.Equal(p, want) {
		t.Fatalf("encode: %q", p)
	}
	d := loraDecode(p)
	if d.Nickname != "pi" || d.Message != "hello" || d.HasReply {
		t.Fatalf("decode: %+v", d)
	}

	// reply reference: 0x1d + upper-case hex FNV-1a of the original text
	p = loraEncode("yes", "Bob", "foobar")
	want = append([]byte{0x83}, "Bob\x1dBF9CF968yes"...)
	if !bytes.Equal(p, want) {
		t.Fatalf("reply encode: %q", p)
	}
	d = loraDecode(p)
	if !d.HasReply || d.ReplyID != 0xbf9cf968 || d.Message != "yes" || d.Nickname != "Bob" {
		t.Fatalf("reply decode: %+v", d)
	}

	// a plain payload (old firmware, other senders) is the message itself
	d = loraDecode([]byte("raw text"))
	if d.Nickname != "" || d.Message != "raw text" {
		t.Fatalf("raw: %+v", d)
	}
	// a malformed reference stays part of the message
	d = loraDecode(append([]byte{0x81}, "x\x1dZZZZZZZZhi"...))
	if d.HasReply || d.Message != "\x1dZZZZZZZZhi" {
		t.Fatalf("bad ref: %+v", d)
	}
	// marker without room for a message is not a nickname
	d = loraDecode(append([]byte{0x82}, "pi"...))
	if d.Nickname != "" {
		t.Fatalf("short: %+v", d)
	}
}

func TestLoraLimits(t *testing.T) {
	if loraMsgMax != 116 || loraReplyMax != 107 {
		t.Fatalf("limits %d %d", loraMsgMax, loraReplyMax)
	}
	if loraEncode(strings.Repeat("x", 116), "abcdefghij", "") == nil {
		t.Fatal("116 bytes must fit")
	}
	if got := loraEncode(strings.Repeat("x", 116), "abcdefghij", ""); len(got) != loraMaxPayload {
		t.Fatalf("payload %d", len(got))
	}
	if loraEncode(strings.Repeat("x", 117), "pi", "") != nil {
		t.Fatal("117 bytes accepted")
	}
	if loraEncode(strings.Repeat("x", 108), "pi", "orig") != nil {
		t.Fatal("108-byte reply accepted")
	}
	if loraEncode("", "pi", "") != nil {
		t.Fatal("empty accepted")
	}
	// invalid nickname: sent without the nickname header, like the original
	if got := loraEncode("hi", "名字", ""); string(got) != "hi" {
		t.Fatalf("bad nick: %q", got)
	}
	for nick, ok := range map[string]bool{"pi": true, "abcdefghij": true, "abcdefghijk": false, "   ": false, "": false, "é": false} {
		if loraValidNick(nick) != ok {
			t.Errorf("nick %q", nick)
		}
	}
	if got := loraCString([]byte("ab\x00cd")); string(got) != "ab" {
		t.Fatalf("cstring %q", got)
	}
}

func TestSX1262Maths(t *testing.T) {
	if got := loraFreqRaw(868.0); got != 0x36400000 {
		t.Fatalf("frf %08x", got)
	}
	if got := loraFreqRaw(915.0); got != 0x39300000 {
		t.Fatalf("frf 915 %08x", got)
	}
	if got := loraSyncWordBytes(0x34); got != [2]byte{0x34, 0x44} {
		t.Fatalf("sync 0x34 % x", got)
	}
	if got := loraSyncWordBytes(0x12); got != [2]byte{0x14, 0x24} {
		t.Fatalf("sync 0x12 % x", got)
	}
	if loraLDRO(12, 125) != 1 || loraLDRO(11, 125) != 1 || loraLDRO(10, 125) != 0 || loraLDRO(12, 500) != 0 {
		t.Fatal("ldro")
	}
	for khz, code := range map[float64]byte{7.8: 0, 10.4: 8, 15.6: 1, 20.8: 9, 31.25: 2, 41.7: 0x0a, 62.5: 3, 125: 4, 250: 5, 500: 6} {
		if c, ok := loraBWCode(khz); !ok || c != code {
			t.Errorf("bw %v -> %x", khz, c)
		}
	}
	if c, ok := loraTCXOCode(3.0); !ok || c != 6 {
		t.Fatal("tcxo")
	}
	if got := loraImageCal(868); got != [2]byte{0xd7, 0xdb} {
		t.Fatal("image cal")
	}
	// SF12/125 kHz/4:5, 20-symbol preamble, 20-byte payload: 24.25+28 symbols of 32.768 ms
	toa := loraTimeOnAir(20, 12, 125, 5, 20)
	if d := toa - 1712128*time.Microsecond; d < -time.Millisecond || d > time.Millisecond {
		t.Fatalf("toa %v", toa)
	}
	if toa := loraTimeOnAir(127, 12, 125, 5, 20); toa < 5*time.Second || toa > 6*time.Second {
		t.Fatalf("toa 127 %v", toa)
	}
	if rssi, snr := loraPacketStatus(174, 0xf6); rssi != -87 || snr != -2.5 {
		t.Fatalf("packet status %v %v", rssi, snr)
	}
	if loraStatusError(0x00) == nil || loraStatusError(0xff) == nil || loraStatusError(0x26) == nil || loraStatusError(0x22) != nil || loraStatusError(0x2c) != nil {
		t.Fatal("status")
	}
}

func TestLoraKernelLayouts(t *testing.T) {
	if s := unsafe.Sizeof(loraGPIOLineRequest{}); s != 592 {
		t.Fatalf("gpio_v2_line_request %d", s)
	}
	if s := unsafe.Sizeof(loraGPIOLineConfig{}); s != 272 {
		t.Fatalf("gpio_v2_line_config %d", s)
	}
	if s := unsafe.Sizeof(loraSPITransfer{}); s != 32 {
		t.Fatalf("spi_ioc_transfer %d", s)
	}
	if s := unsafe.Sizeof(loraI2CMsg{}); s != 16 {
		t.Fatalf("i2c_msg %d", s)
	}
	if o := unsafe.Offsetof(loraTermios{}.Cc); o != 17 {
		t.Fatalf("termios c_cc at %d", o)
	}
	raw := loraRawTermios(loraTermios{Cflag: 0x0d | 0x30 | 0x100, Lflag: 0x8a3b, Iflag: 0x500})
	if raw.Cflag&0x100f != loraB115200 || raw.Cflag&0x30 != 0x30 || raw.Cflag&0x100 != 0 || raw.Lflag&0x2 != 0 || raw.Iflag != 0 || raw.Cc[6] != 1 {
		t.Fatalf("raw termios %+v", raw)
	}
}

func TestLoraCapPowerSharing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	ext, grove := dir+"/ext", dir+"/grove"
	if err := os.WriteFile(ext, []byte("0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grove, []byte("255\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	read := func(p string) string { v, _ := sysRead(p); return v }

	lora, err := loraPowerOn(ext, grove)
	if err != nil {
		t.Fatal(err)
	}
	if read(ext) != "1" || read(grove) != "255" {
		t.Fatalf("on: ext=%s grove=%s", read(ext), read(grove))
	}
	gps, err := loraPowerOn(ext) // second user must not block or re-record
	if err != nil {
		t.Fatal(err)
	}
	lora.release()
	if read(ext) != "1" {
		t.Fatal("rail turned off while the GPS still uses it")
	}
	gps.release()
	if read(ext) != "0" || read(grove) != "255" {
		t.Fatalf("restore: ext=%s grove=%s", read(ext), read(grove))
	}
	if _, err := os.Stat(dir + "/zerodesk/cap-lora-power.state"); !os.IsNotExist(err) {
		t.Fatal("state file left behind")
	}
}
