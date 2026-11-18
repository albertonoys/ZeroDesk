package main

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// event bytes as the kernel writes them (64-bit timeval)
func kbTestEvent(typ, code uint16, val int32) []byte {
	b := make([]byte, 24)
	binary.LittleEndian.PutUint64(b[0:], 1700000000)
	binary.LittleEndian.PutUint64(b[8:], 123456)
	binary.LittleEndian.PutUint16(b[16:], typ)
	binary.LittleEndian.PutUint16(b[18:], code)
	binary.LittleEndian.PutUint32(b[20:], uint32(val))
	return b
}

func TestKbDecodeEvents(t *testing.T) {
	var buf []byte
	buf = append(buf, kbTestEvent(kbEvMsc, kbMscScan, 0x20)...)
	buf = append(buf, kbTestEvent(kbEvKey, 30, 1)...)
	buf = append(buf, kbTestEvent(kbEvSyn, 0, 0)...)
	buf = append(buf, 1, 2, 3) // partial trailing event is ignored
	got := kbDecodeEvents(buf, 24)
	want := []kbInputEvent{{kbEvMsc, kbMscScan, 0x20}, {kbEvKey, 30, 1}, {kbEvSyn, 0, 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if neg := kbDecodeEvents(kbTestEvent(kbEvKey, 1, -1), 24); neg[0].Value != -1 {
		t.Fatalf("value sign lost: %+v", neg)
	}
}

func TestKbDecoderPairsScanWithKey(t *testing.T) {
	d := newKbDecoder()
	var got []kbRaw
	for _, e := range []kbInputEvent{
		{kbEvMsc, kbMscScan, 0x20}, {kbEvKey, 30, 1}, // A down, scan r2c0
		{kbEvMsc, kbMscScan, 0x41}, {kbEvKey, kcLeftShift, 1}, {kbEvSyn, 0, 0}, // two keys in one frame
		{kbEvMsc, kbMscScan, 0x43}, {kbEvSyn, 0, 0}, // sym tap: scan only
		{kbEvKey, 103, 1}, {kbEvSyn, 0, 0}, // key without scan
		{kbEvKey, 30, 2},                                                         // repeat
		{kbEvMsc, kbMscScan, 0x42}, {kbEvMsc, kbMscScan, 0x20}, {kbEvKey, 30, 0}, // dangling scan then key
	} {
		got = append(got, d.feed(e)...)
	}
	want := []kbRaw{
		{0x20, 30, 1}, {0x41, kcLeftShift, 1}, {0x43, 0, -1}, {-1, 103, 1}, {-1, 30, 2},
		{0x42, 0, -1}, {0x20, 30, 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// The device tree of the shipping image, as read from
// /sys/bus/i2c/devices/1-0034/of_node/linux,keymap.
func TestParseKbDTKeymap(t *testing.T) {
	var b []byte
	for r, row := range kbBuiltinLayers[0] {
		for c, code := range row {
			if code != 0 {
				b = binary.BigEndian.AppendUint32(b, uint32(r)<<24|uint32(c)<<16|uint32(code))
			}
		}
	}
	tab, ok := parseKbDTKeymap(b, 5, 10)
	if !ok {
		t.Fatal("parse failed")
	}
	for r := range 5 {
		if !reflect.DeepEqual(tab[r], kbBuiltinLayers[0][r][:]) {
			t.Fatalf("row %d: %v", r, tab[r])
		}
	}
	if _, ok := parseKbDTKeymap(b[:5], 5, 10); ok {
		t.Fatal("truncated property accepted")
	}
	if _, ok := parseKbDTKeymap([]byte{5, 0, 0, 1}, 5, 10); ok {
		t.Fatal("row out of range accepted")
	}
}

func TestKbRowShiftAndKeyID(t *testing.T) {
	for cols, want := range map[int]uint{1: 0, 2: 1, 8: 3, 10: 4, 16: 4, 17: 5} {
		if got := kbRowShift(cols); got != want {
			t.Errorf("cols %d: shift %d want %d", cols, got, want)
		}
	}
	l := kbBuiltinLayout()
	cases := []struct {
		scan int
		code uint16
		want string
	}{
		{0x41, 0, "r4c1"},  // shift-button-code
		{0x42, 0, "r4c2"},  // fn-button-code
		{0x43, 0, "r4c3"},  // sym-button-code
		{0x20, 30, "r2c0"}, // A
		{-1, 30, "r2c0"},   // A without scan
		{-1, 103, "r2c3"},  // fn layer: up is on F
		{-1, 26, "r4c4"},   // sym layer: ! is on 1
		{-1, 999, ""},
		{0x7f, 0, ""}, // outside the matrix
	}
	for _, c := range cases {
		if got := l.keyID(c.scan, c.code); got != c.want {
			t.Errorf("keyID(%#x, %d) = %q want %q", c.scan, c.code, got, c.want)
		}
	}
}

func TestKbCaption(t *testing.T) {
	cases := []struct {
		code  uint16
		shift bool
		label string
		text  string
		icon  string
	}{
		{30, false, "a", "a", ""},
		{30, true, "A", "A", ""},
		{2, false, "1", "1", ""},
		{2, true, "!", "!", ""},
		{11, false, "0", "0", ""},
		{11, true, ")", ")", ""},
		{26, false, "!", "!", ""}, // sym 1
		{94, true, ")", ")", ""},  // sym 0, shift ignored
		{70, false, "_", "_", ""}, // sym E
		{90, false, ",", ",", ""}, // sym V
		{57, false, "space", " ", ""},
		{28, false, "OK", "", "return"},
		{14, false, "del", "", "delete_left"},
		{111, false, "del", "", "delete_right"},
		{59, false, "F1", "", ""},
		{68, false, "F10", "", ""},
		{88, false, "F12", "", ""},
		{103, false, "up", "", "arrowtriangle_up_fill"},
		{114, false, "vol−", "", "speaker_1_fill"},
		{0, false, "", "", ""},
	}
	for _, c := range cases {
		got := kbCaption(c.code, c.shift, kbBuiltinChars)
		if got.Code != c.code || got.Label != c.label || got.Text != c.text || got.Icon != c.icon {
			t.Errorf("kbCaption(%d, %v) = %+v", c.code, c.shift, got)
		}
	}
}

func TestKbLayoutLayers(t *testing.T) {
	l := kbBuiltinLayout()
	if len(l.Keys) != 46 {
		t.Fatalf("%d keys, want 46", len(l.Keys))
	}
	k := l.Keys["r2c3"] // F
	if k.Base.Label != "f" || k.Shift.Label != "F" || k.Sym.Code != 0 || k.Fn.Label != "up" {
		t.Fatalf("F key: %+v", k)
	}
	if k := l.Keys["r4c1"]; k.Sym.Label != "shift" || k.Fn.Label != "shift" {
		t.Fatalf("shift key must keep its cap in every layer: %+v", k)
	}
	if k := l.Keys["r1c0"]; k.Sym.Label != "~" || k.Fn.Icon != "playpause_fill" {
		t.Fatalf("Q key: %+v", k)
	}
}

func TestParseKbCharMap(t *testing.T) {
	m := parseKbCharMap("# -- Row 4\nkeycode 26 = exclam            # 0xb7 -> !\n\nkeycode 89 = bar\nkeycode x = at\nkeycode 30 = a\nkeycode 31 = Return\n")
	want := map[uint16]string{26: "!", 89: "|", 30: "a"}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got %v", m)
	}
}

func TestKbReadLayoutFromDeviceTree(t *testing.T) {
	dir := t.TempDir()
	u32 := func(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
	write := func(name string, b []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("keypad,num-rows", u32(5))
	write("keypad,num-columns", u32(10))
	for i, name := range []string{"linux,keymap", "linux,keymap1", "linux,keymap2"} {
		var b []byte
		for r, row := range kbBuiltinLayers[i] {
			for c, code := range row {
				b = append(b, u32(uint32(r)<<24|uint32(c)<<16|uint32(code))...)
			}
		}
		write(name, b)
	}
	charMap := filepath.Join(dir, "keymap.map")
	if err := os.WriteFile(charMap, []byte("keycode 55 = asterisk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := kbReadLayout(dir, charMap)
	if l.Source != "device" || l.Rows != 5 || l.Cols != 10 {
		t.Fatalf("layout %s %dx%d", l.Source, l.Rows, l.Cols)
	}
	if got := l.Keys["r1c0"].Sym.Label; got != "*" {
		t.Fatalf("runtime char map not applied: %q", got)
	}
	if got := kbReadLayout(t.TempDir(), charMap); got.Source != "builtin" || len(got.Keys) != 46 {
		t.Fatalf("missing device tree must fall back: %s %d", got.Source, len(got.Keys))
	}
}

func TestParseKbModifierState(t *testing.T) {
	m, seq, err := parseKbModifierState("version=1 sequence=53 changed_mask=0x00 reason=2 sym=0 shift=0 fn=3 ctrl=0 alt=")
	if err != nil || seq != 53 || m != (kbMods{Shift: kbModOff, Sym: kbModOff, Fn: kbModLocked, Source: "sysfs"}) {
		t.Fatalf("device example: %+v %d %v", m, seq, err)
	}
	m, seq, err = parseKbModifierState("future=x fn=4 shift=1 version=1 sym=2 sequence=18446744073709551615 ctrl=1")
	if err != nil || seq != 1<<64-1 || m.Shift != kbModHeld || m.Sym != kbModOneShot || m.Fn != kbModHeld {
		t.Fatalf("field order: %+v %d %v", m, seq, err)
	}
	for _, bad := range []string{
		"version=2 sequence=1 sym=0 shift=0 fn=0",
		"version=1 sequence=1 sym=0 shift=0",
		"version=1 sequence=1 sym=0 shift=5 fn=0",
		"version=1 sequence=1 sym=0 sym=0 shift=0 fn=0",
		"version=1 sequence=1 sym=0 shift=0 fn=0 junk",
		"version=1 sequence=x sym=0 shift=0 fn=0",
	} {
		if _, _, err := parseKbModifierState(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestKbModes(t *testing.T) {
	legacy := []string{kbModOff, kbModOneShot, kbModLocked, kbModHeld}
	for v, want := range legacy {
		if got, ok := kbModeLegacy(uint8(v)); !ok || got != want {
			t.Errorf("legacy %d = %q", v, got)
		}
	}
	if _, ok := kbModeLegacy(4); ok {
		t.Error("legacy 4 accepted")
	}
	sysfs := []string{kbModOff, kbModHeld, kbModOneShot, kbModLocked, kbModHeld}
	for v, want := range sysfs {
		if got, ok := kbModeSysfs(uint64(v)); !ok || got != want {
			t.Errorf("sysfs %d = %q", v, got)
		}
	}
}

func TestKbMergeMods(t *testing.T) {
	before := kbMods{Shift: kbModOneShot, Sym: kbModOff, Fn: kbModLocked, Source: "i2c"}
	now := kbMods{Shift: kbModOff, Sym: kbModHeld, Fn: kbModLocked, Source: "i2c"}
	got := kbMergeMods(before, now)
	want := kbMods{Shift: kbModOneShot, Sym: kbModHeld, Fn: kbModLocked, Source: "i2c"}
	if got != want {
		t.Fatalf("got %+v", got)
	}
}

func TestKbCodeName(t *testing.T) {
	for code, want := range map[uint16]string{30: "KEY_A", 2: "KEY_1", 11: "KEY_0", 59: "KEY_F1", kcCD: "KEY_CD", 103: "KEY_UP", 500: "KEY_500"} {
		if got := kbCodeName(code); got != want {
			t.Errorf("kbCodeName(%d) = %s", code, got)
		}
	}
}

// Raw evdev bytes through the decoder and the backend to the SSE messages a
// window receives (no modifier source: Shift comes from KEY_LEFTSHIFT).
func TestKbBackendStreamsKeys(t *testing.T) {
	b := &kbBackend{pressed: map[string]bool{}, codes: map[uint16]bool{}}
	b.state.Layout = kbBuiltinLayout()
	b.state.Mods = kbMods{Shift: kbModOff, Sym: kbModOff, Fn: kbModOff, Source: "evdev"}
	b.hub = newHWHub(nil, nil)
	sub := make(chan []byte, 32)
	b.hub.subs[sub] = true

	var buf []byte
	for _, e := range [][3]int{
		{kbEvMsc, kbMscScan, 0x41}, {kbEvKey, kcLeftShift, 1}, {kbEvSyn, 0, 0},
		{kbEvMsc, kbMscScan, 0x20}, {kbEvKey, 30, 1}, {kbEvSyn, 0, 0},
		{kbEvMsc, kbMscScan, 0x43}, {kbEvSyn, 0, 0},
	} {
		buf = append(buf, kbTestEvent(uint16(e[0]), uint16(e[1]), int32(e[2]))...)
	}
	dec := newKbDecoder()
	for _, e := range kbDecodeEvents(buf, 24) {
		for _, raw := range dec.feed(e) {
			b.handle(raw)
		}
	}
	var keys []kbKeyEvent
	for len(sub) > 0 {
		msg := string(<-sub)
		if ev, data, ok := strings.Cut(msg, "\ndata: "); ok && ev == "event: key" {
			var k kbKeyEvent
			if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &k); err != nil {
				t.Fatal(err)
			}
			keys = append(keys, k)
		}
	}
	if len(keys) != 3 {
		t.Fatalf("%d key events: %+v", len(keys), keys)
	}
	if k := keys[0]; k.ID != "r4c1" || k.Name != "KEY_LEFTSHIFT" || k.Value != 1 {
		t.Errorf("shift: %+v", k)
	}
	if k := keys[1]; k.ID != "r2c0" || k.Text != "A" || k.Mods.Shift != kbModHeld {
		t.Errorf("A with shift held: %+v", k)
	}
	if k := keys[2]; k.ID != "r4c3" || k.Value != -1 {
		t.Errorf("sym tap: %+v", k)
	}
	if s := b.snapshot(); len(s.Pressed) != 2 {
		t.Errorf("pressed %v", s.Pressed)
	}
}
