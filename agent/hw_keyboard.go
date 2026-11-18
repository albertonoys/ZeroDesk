package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"math/bits"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Keyboard Guide: a Keyboard Viewer for the built-in TCA8418 keyboard. Keys
// are read from its evdev node without EVIOCGRAB, so the launcher and the
// foreground app keep receiving them. The three key layers come from the
// running device tree (linux,keymap / keymap1 = sym / keymap2 = fn), the sym
// characters from /usr/share/keymaps/tca8418_keypad_m5stack_keymap.map, and the
// sticky Shift/Sym/Fn state (tap = one-shot, double-tap = lock, hold) from the
// keyboard driver's modifier_state sysfs file or, on current images, the
// m5ioe1 LED-mode registers. Input handling and the modifier-state parsing are
// ported from CardputerZero/Keyboard-Guide (MIT,
// src/input/keyboard_guide_input.cpp, src/input/modifier_state_parser.cpp);
// the built-in sym table is CardputerZero/launcher's (MIT,
// ext_components/cp0_lvgl/src/cp0/cp0_keyboard_keymap.c).

func init() { hwApps["keyboard-guide"] = hwKeyboard }

// Linux key codes used by the layers (linux/input-event-codes.h)
const (
	kcEsc        = 1
	kcBackspace  = 14
	kcTab        = 15
	kcEnter      = 28
	kcLeftCtrl   = 29
	kcLeftShift  = 42
	kcRightShift = 54
	kcLeftAlt    = 56
	kcSpace      = 57
	kcCD         = 0x17f // sym key
	kcDVD        = 0x185 // fn key
	kcFn         = 0x1d0
	kcCompose    = 127
)

// Built-in copy of the device tree keymaps (5 x 10 matrix) of the shipping
// image, used when the device tree is not readable.
var kbBuiltinLayers = [3][5][10]uint16{
	{ // base
		{3, 4, 5, 6, 7, 8, 9, 10, 11, 14},
		{16, 17, 18, 19, 20, 21, 22, 23, 24, 25},
		{30, 31, 32, 33, 34, 35, 36, 37, 38, 28},
		{29, 56, 44, 45, 46, 47, 48, 49, 50, 57},
		{1, 42, kcDVD, kcCD, 2, 0, 0, 0, 0, 15},
	},
	{ // sym
		{27, 39, 40, 41, 43, 51, 52, 53, 94, 14},
		{55, 69, 70, 71, 72, 73, 74, 75, 76, 77},
		{79, 80, 81, 0, 82, 83, 85, 86, 89, 28},
		{29, 56, 0, 0, 0, 90, 91, 92, 93, 57},
		{1, 78, kcDVD, kcCD, 26, 0, 0, 0, 0, 15},
	},
	{ // fn
		{60, 61, 62, 63, 64, 65, 66, 67, 68, 111},
		{164, 165, 163, 19, 20, 21, 224, 225, 87, 88},
		{113, 114, 115, 103, 34, 138, 99, 102, 104, 28},
		{29, 56, 105, 108, 106, 47, 110, 107, 109, 57},
		{1, 42, kcDVD, kcCD, 59, 0, 0, 0, 0, 15},
	},
}

var kbBuiltinChars = map[uint16]string{
	26: "!", 27: "@", 39: "#", 40: "$", 41: "%", 43: "^", 51: "&", 52: "*", 53: "(", 94: ")",
	55: "~", 69: "`", 70: "_", 71: "-", 72: "+", 73: "=", 74: "[", 75: "]", 76: "{", 77: "}",
	79: ";", 80: ":", 81: "'", 82: "\"", 83: "<", 85: ">", 86: "\\", 89: "|",
	90: ",", 91: ".", 92: "/", 93: "?",
}

// X keysym names that appear in the runtime keymap file
var kbKeysyms = map[string]string{
	"exclam": "!", "at": "@", "numbersign": "#", "dollar": "$", "percent": "%", "asciicircum": "^",
	"ampersand": "&", "asterisk": "*", "parenleft": "(", "parenright": ")", "asciitilde": "~",
	"grave": "`", "underscore": "_", "minus": "-", "plus": "+", "equal": "=", "bracketleft": "[",
	"bracketright": "]", "braceleft": "{", "braceright": "}", "semicolon": ";", "colon": ":",
	"apostrophe": "'", "quotedbl": "\"", "less": "<", "greater": ">", "backslash": "\\", "bar": "|",
	"comma": ",", "period": ".", "slash": "/", "question": "?", "space": " ",
}

func kbKeysymChar(name string) string {
	if c, ok := kbKeysyms[name]; ok {
		return c
	}
	if len(name) == 1 && name[0] > ' ' && name[0] < 0x7f {
		return name
	}
	return ""
}

// parseKbCharMap reads "keycode <n> = <keysym>" lines.
func parseKbCharMap(text string) map[uint16]string {
	m := map[uint16]string{}
	for _, line := range strings.Split(text, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) != 4 || f[0] != "keycode" || f[2] != "=" {
			continue
		}
		n, err := strconv.ParseUint(f[1], 10, 16)
		if err != nil {
			continue
		}
		if c := kbKeysymChar(f[3]); c != "" {
			m[uint16(n)] = c
		}
	}
	return m
}

// parseKbDTKeymap decodes a linux,keymap property (big-endian u32 entries of
// row<<24 | col<<16 | code) into a rows x cols table.
func parseKbDTKeymap(b []byte, rows, cols int) ([][]uint16, bool) {
	if rows <= 0 || cols <= 0 || len(b) == 0 || len(b)%4 != 0 {
		return nil, false
	}
	t := make([][]uint16, rows)
	for r := range t {
		t[r] = make([]uint16, cols)
	}
	for i := 0; i < len(b); i += 4 {
		v := binary.BigEndian.Uint32(b[i:])
		r, c, code := int(v>>24), int(v>>16&0xff), uint16(v)
		if r >= rows || c >= cols {
			return nil, false
		}
		t[r][c] = code
	}
	return t, true
}

// kbRowShift matches the tca8418 driver: MATRIX_SCAN_CODE(row, col,
// get_count_order(cols)).
func kbRowShift(cols int) uint {
	if cols <= 1 {
		return 0
	}
	return uint(bits.Len(uint(cols - 1)))
}

// ---- key captions ----

type kbCap struct {
	Code  uint16 `json:"code"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
	Text  string `json:"text,omitempty"` // character typed, if any
}

var kbLetters = map[uint16]byte{}

func init() {
	for i, c := range "qwertyuiop" {
		kbLetters[uint16(16+i)] = byte(c)
	}
	for i, c := range "asdfghjkl" {
		kbLetters[uint16(30+i)] = byte(c)
	}
	for i, c := range "zxcvbnm" {
		kbLetters[uint16(44+i)] = byte(c)
	}
}

var kbNamed = map[uint16]kbCap{
	kcEsc: {Label: "esc"}, kcBackspace: {Label: "del", Icon: "delete_left"}, kcTab: {Label: "tab"},
	kcEnter: {Label: "OK", Icon: "return"}, kcLeftCtrl: {Label: "ctrl"}, kcLeftAlt: {Label: "alt"},
	kcSpace: {Label: "space", Text: " "}, kcLeftShift: {Label: "shift"}, kcRightShift: {Label: "shift"},
	kcCD: {Label: "sym"}, kcCompose: {Label: "sym"}, kcDVD: {Label: "fn"}, kcFn: {Label: "fn"},
	87: {Label: "F11"}, 88: {Label: "F12"}, 99: {Label: "PrtSc"}, 102: {Label: "home"},
	103: {Label: "up", Icon: "arrowtriangle_up_fill"}, 104: {Label: "PgUp"},
	105: {Label: "left", Icon: "arrowtriangle_left_fill"}, 106: {Label: "right", Icon: "arrowtriangle_right_fill"},
	107: {Label: "end"}, 108: {Label: "down", Icon: "arrowtriangle_down_fill"}, 109: {Label: "PgDn"},
	110: {Label: "ins"}, 111: {Label: "del", Icon: "delete_right"},
	113: {Label: "mute", Icon: "speaker_slash_fill"}, 114: {Label: "vol−", Icon: "speaker_1_fill"},
	115: {Label: "vol+", Icon: "speaker_3_fill"}, 138: {Label: "help", Icon: "question_circle"},
	163: {Label: "next", Icon: "forward_fill"}, 164: {Label: "play", Icon: "playpause_fill"},
	165: {Label: "prev", Icon: "backward_fill"}, 224: {Label: "bright−", Icon: "sun_min_fill"},
	225: {Label: "bright+", Icon: "sun_max_fill"},
}

// kbCaption is what a key code produces: the sym-layer codes are looked up in
// the character map, the rest follow the US layout the launcher uses (xkb
// "us"), with Shift applied to letters and digits.
func kbCaption(code uint16, shift bool, chars map[uint16]string) kbCap {
	c := kbCap{Code: code}
	if code == 0 {
		return c
	}
	if s, ok := chars[code]; ok {
		c.Label, c.Text = s, s
		return c
	}
	if l, ok := kbLetters[code]; ok {
		if shift {
			l -= 'a' - 'A'
		}
		c.Label, c.Text = string(l), string(l)
		return c
	}
	if code >= 2 && code <= 11 { // KEY_1 .. KEY_0
		i := int(code-2+1) % 10
		s := string("0123456789"[i])
		if shift {
			s = string(")!@#$%^&*("[i])
		}
		c.Label, c.Text = s, s
		return c
	}
	if code >= 59 && code <= 68 { // KEY_F1 .. KEY_F10
		c.Label = "F" + strconv.Itoa(int(code-58))
		return c
	}
	if n, ok := kbNamed[code]; ok {
		n.Code = code
		return n
	}
	c.Label = strconv.Itoa(int(code))
	return c
}

var kbCodeNames = map[uint16]string{
	1: "KEY_ESC", 14: "KEY_BACKSPACE", 15: "KEY_TAB", 28: "KEY_ENTER", 29: "KEY_LEFTCTRL", 42: "KEY_LEFTSHIFT",
	54: "KEY_RIGHTSHIFT", 56: "KEY_LEFTALT", 57: "KEY_SPACE", 26: "KEY_LEFTBRACE", 27: "KEY_RIGHTBRACE",
	39: "KEY_SEMICOLON", 40: "KEY_APOSTROPHE", 41: "KEY_GRAVE", 43: "KEY_BACKSLASH", 51: "KEY_COMMA",
	52: "KEY_DOT", 53: "KEY_SLASH", 55: "KEY_KPASTERISK", 69: "KEY_NUMLOCK", 70: "KEY_SCROLLLOCK",
	71: "KEY_KP7", 72: "KEY_KP8", 73: "KEY_KP9", 74: "KEY_KPMINUS", 75: "KEY_KP4", 76: "KEY_KP5", 77: "KEY_KP6",
	78: "KEY_KPPLUS", 79: "KEY_KP1", 80: "KEY_KP2", 81: "KEY_KP3", 82: "KEY_KP0", 83: "KEY_KPDOT",
	85: "KEY_ZENKAKUHANKAKU", 86: "KEY_102ND", 87: "KEY_F11", 88: "KEY_F12", 89: "KEY_RO", 90: "KEY_KATAKANA",
	91: "KEY_HIRAGANA", 92: "KEY_HENKAN", 93: "KEY_KATAKANAHIRAGANA", 94: "KEY_MUHENKAN", 99: "KEY_SYSRQ",
	102: "KEY_HOME", 103: "KEY_UP", 104: "KEY_PAGEUP", 105: "KEY_LEFT", 106: "KEY_RIGHT", 107: "KEY_END",
	108: "KEY_DOWN", 109: "KEY_PAGEDOWN", 110: "KEY_INSERT", 111: "KEY_DELETE", 113: "KEY_MUTE",
	114: "KEY_VOLUMEDOWN", 115: "KEY_VOLUMEUP", 127: "KEY_COMPOSE", 138: "KEY_HELP", 163: "KEY_NEXTSONG",
	164: "KEY_PLAYPAUSE", 165: "KEY_PREVIOUSSONG", 224: "KEY_BRIGHTNESSDOWN", 225: "KEY_BRIGHTNESSUP",
	kcCD: "KEY_CD", kcDVD: "KEY_DVD", kcFn: "KEY_FN",
}

func kbCodeName(code uint16) string {
	if n, ok := kbCodeNames[code]; ok {
		return n
	}
	if l, ok := kbLetters[code]; ok {
		return "KEY_" + strings.ToUpper(string(l))
	}
	if code >= 2 && code <= 11 {
		return "KEY_" + string("1234567890"[code-2])
	}
	if code >= 59 && code <= 68 {
		return "KEY_F" + strconv.Itoa(int(code-58))
	}
	return "KEY_" + strconv.Itoa(int(code))
}

// ---- layout ----

type kbKeyCaps struct {
	Base  kbCap `json:"base"`
	Shift kbCap `json:"shift"`
	Sym   kbCap `json:"sym"`
	Fn    kbCap `json:"fn"`
}

type kbLayout struct {
	Rows   int                  `json:"rows"`
	Cols   int                  `json:"cols"`
	Source string               `json:"source"` // "device" or "builtin"
	Keys   map[string]kbKeyCaps `json:"keys"`   // by matrix id "r<row>c<col>"
	layers [3][][]uint16
	chars  map[uint16]string
	shift  uint
}

func kbModifierCode(code uint16) bool {
	switch code {
	case kcLeftShift, kcRightShift, kcCD, kcDVD, kcFn, kcCompose:
		return true
	}
	return false
}

func kbKeyID(r, c int) string { return "r" + strconv.Itoa(r) + "c" + strconv.Itoa(c) }

func newKbLayout(layers [3][][]uint16, chars map[uint16]string, source string) *kbLayout {
	l := &kbLayout{Rows: len(layers[0]), Source: source, Keys: map[string]kbKeyCaps{}, layers: layers, chars: chars}
	if l.Rows > 0 {
		l.Cols = len(layers[0][0])
	}
	l.shift = kbRowShift(l.Cols)
	for r := range l.Rows {
		for c := range l.Cols {
			base := layers[0][r][c]
			if base == 0 {
				continue
			}
			if kbModifierCode(base) { // shift, sym, fn: the same cap in every layer
				b := kbCaption(base, false, chars)
				l.Keys[kbKeyID(r, c)] = kbKeyCaps{Base: b, Shift: b, Sym: b, Fn: b}
				continue
			}
			l.Keys[kbKeyID(r, c)] = kbKeyCaps{
				Base:  kbCaption(base, false, chars),
				Shift: kbCaption(base, true, chars),
				Sym:   kbCaption(layers[1][r][c], false, chars),
				Fn:    kbCaption(layers[2][r][c], false, chars),
			}
		}
	}
	return l
}

func kbBuiltinLayout() *kbLayout {
	var layers [3][][]uint16
	for i, t := range kbBuiltinLayers {
		for _, row := range t {
			layers[i] = append(layers[i], append([]uint16(nil), row[:]...))
		}
	}
	return newKbLayout(layers, kbBuiltinChars, "builtin")
}

// kbReadLayout reads the keymaps of the running device tree node of the
// keyboard (/sys/class/input/eventN/device/device/of_node).
func kbReadLayout(ofNode, charMap string) *kbLayout {
	u32 := func(name string) int {
		b, err := os.ReadFile(filepath.Join(ofNode, name))
		if err != nil || len(b) != 4 {
			return 0
		}
		return int(binary.BigEndian.Uint32(b))
	}
	chars := kbBuiltinChars
	if b, err := os.ReadFile(charMap); err == nil {
		if m := parseKbCharMap(string(b)); len(m) > 0 {
			chars = m
		}
	}
	rows, cols := u32("keypad,num-rows"), u32("keypad,num-columns")
	var layers [3][][]uint16
	for i, name := range []string{"linux,keymap", "linux,keymap1", "linux,keymap2"} {
		b, err := os.ReadFile(filepath.Join(ofNode, name))
		if err != nil && i > 0 {
			layers[i] = layers[0] // no sym/fn table: those keys stay the same
			continue
		}
		t, ok := parseKbDTKeymap(b, rows, cols)
		if err != nil || !ok {
			l := kbBuiltinLayout()
			return newKbLayout(l.layers, chars, "builtin")
		}
		layers[i] = t
	}
	return newKbLayout(layers, chars, "device")
}

// keyID finds the physical key of an event: from the MSC_SCAN code when the
// driver sent one, else by looking the key code up in the layers.
func (l *kbLayout) keyID(scan int, code uint16) string {
	if scan >= 0 {
		r, c := scan>>l.shift, scan&(1<<l.shift-1)
		if r < l.Rows && c < l.Cols {
			return kbKeyID(r, c)
		}
	}
	if code == 0 {
		return ""
	}
	for _, t := range l.layers {
		for r, row := range t {
			for c, v := range row {
				if v == code {
					return kbKeyID(r, c)
				}
			}
		}
	}
	return ""
}

// ---- event decoding ----

// kbRaw is one key action: Value 0 up, 1 down, 2 repeat, -1 a scan code
// without a key event (the driver reports Sym/Fn taps that way).
type kbRaw struct {
	Scan  int
	Code  uint16
	Value int32
}

// kbDecoder pairs each EV_MSC/MSC_SCAN with the EV_KEY that follows it.
type kbDecoder struct{ scan int }

func newKbDecoder() *kbDecoder { return &kbDecoder{scan: -1} }

func (d *kbDecoder) feed(e kbInputEvent) []kbRaw {
	var out []kbRaw
	switch e.Type {
	case kbEvMsc:
		if e.Code == kbMscScan {
			if d.scan >= 0 {
				out = append(out, kbRaw{Scan: d.scan, Value: -1})
			}
			d.scan = int(e.Value)
		}
	case kbEvKey:
		if e.Value >= 0 && e.Value <= 2 {
			out = append(out, kbRaw{Scan: d.scan, Code: e.Code, Value: e.Value})
		}
		d.scan = -1
	case kbEvSyn:
		if d.scan >= 0 {
			out = append(out, kbRaw{Scan: d.scan, Value: -1})
		}
		d.scan = -1
	}
	return out
}

// ---- modifier state ----

const (
	kbModOff     = "off"
	kbModOneShot = "oneshot"
	kbModLocked  = "locked"
	kbModHeld    = "held"
)

type kbMods struct {
	Shift  string `json:"shift"`
	Sym    string `json:"sym"`
	Fn     string `json:"fn"`
	Source string `json:"source"` // "sysfs", "i2c" or "evdev"
}

func (m kbMods) eq(o kbMods) bool {
	return m.Shift == o.Shift && m.Sym == o.Sym && m.Fn == o.Fn && m.Source == o.Source
}

// kbModeSysfs maps the modifier_state ABI values (0 inactive, 1 pressed,
// 2 one-shot, 3 locked, 4 held).
func kbModeSysfs(v uint64) (string, bool) {
	switch v {
	case 0:
		return kbModOff, true
	case 1, 4:
		return kbModHeld, true
	case 2:
		return kbModOneShot, true
	case 3:
		return kbModLocked, true
	}
	return "", false
}

// kbModeLegacy maps the m5ioe1 LED-mode register values.
func kbModeLegacy(v uint8) (string, bool) {
	switch v {
	case 0:
		return kbModOff, true
	case 1:
		return kbModOneShot, true
	case 2:
		return kbModLocked, true
	case 3:
		return kbModHeld, true
	}
	return "", false
}

// parseKbModifierState parses the sysfs snapshot, e.g.
// "version=1 sequence=53 changed_mask=0x00 reason=2 sym=0 shift=0 fn=3 ctrl=0 alt=".
func parseKbModifierState(text string) (kbMods, uint64, error) {
	var m kbMods
	var version, seq uint64
	seen := map[string]bool{}
	for _, tok := range strings.Fields(text) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok || k == "" {
			return m, 0, fmt.Errorf("malformed token %q", tok)
		}
		switch k {
		case "version", "sequence", "shift", "sym", "fn":
		default:
			continue // optional or future fields
		}
		if seen[k] {
			return m, 0, fmt.Errorf("duplicate field %s", k)
		}
		seen[k] = true
		n, err := strconv.ParseUint(v, 0, 64)
		if err != nil {
			return m, 0, fmt.Errorf("invalid value for %s", k)
		}
		switch k {
		case "version":
			version = n
		case "sequence":
			seq = n
		default:
			mode, ok := kbModeSysfs(n)
			if !ok {
				return m, 0, fmt.Errorf("modifier mode out of range for %s", k)
			}
			switch k {
			case "shift":
				m.Shift = mode
			case "sym":
				m.Sym = mode
			case "fn":
				m.Fn = mode
			}
		}
	}
	if len(seen) != 5 {
		return m, 0, errors.New("missing required modifier state field")
	}
	if version != 1 {
		return m, 0, fmt.Errorf("unsupported modifier state ABI version %d", version)
	}
	m.Source = "sysfs"
	return m, seq, nil
}

// kbModSource reads the sticky modifier state.
type kbModSource struct {
	sysfs    *os.File
	i2c      *kbI2C
	regs     [3]uint8 // shift, sym, fn
	failures int
}

func (s *kbModSource) read() (kbMods, error) {
	if s.sysfs != nil {
		buf := make([]byte, 512)
		n, err := s.sysfs.ReadAt(buf, 0)
		if n == 0 || n == len(buf) {
			if err == nil {
				err = errors.New("short read")
			}
			return kbMods{}, err
		}
		m, _, perr := parseKbModifierState(string(buf[:n]))
		return m, perr
	}
	if s.i2c != nil {
		var modes [3]string
		for i, reg := range s.regs {
			v, err := s.i2c.readByte(reg)
			if err != nil {
				return kbMods{}, err
			}
			mode, ok := kbModeLegacy(v)
			if !ok {
				return kbMods{}, fmt.Errorf("register 0x%02x: invalid mode %d", reg, v)
			}
			modes[i] = mode
		}
		return kbMods{Shift: modes[0], Sym: modes[1], Fn: modes[2], Source: "i2c"}, nil
	}
	return kbMods{}, errors.New("no modifier source")
}

func (s *kbModSource) close() {
	if s.sysfs != nil {
		s.sysfs.Close()
		s.sysfs = nil
	}
	s.i2c.close()
	s.i2c = nil
}

func kbEnvByte(name string, def uint8) uint8 {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.ParseUint(v, 0, 8); err == nil {
			return uint8(n)
		}
	}
	return def
}

// openKbModSource tries the sysfs ABI first, then the legacy registers. The
// register numbers come from the keyboard node's *-led-mode properties
// (<&m5ioe1 reg>), the expander from its supplier link (i2c:1-004f).
func openKbModSource(i2cDir, ofNode string) *kbModSource {
	path := os.Getenv("KEYBOARD_GUIDE_MODIFIER_STATE_PATH")
	if path == "" {
		path = filepath.Join(i2cDir, "modifier_state")
	}
	if f, err := os.Open(path); err == nil {
		s := &kbModSource{sysfs: f}
		if _, err := s.read(); err == nil {
			return s
		}
		f.Close()
	}
	reg := func(prop string, def uint8) uint8 {
		if b, err := os.ReadFile(filepath.Join(ofNode, prop)); err == nil && len(b) == 8 {
			return uint8(binary.BigEndian.Uint32(b[4:]))
		}
		return def
	}
	regs := [3]uint8{
		kbEnvByte("KEYBOARD_GUIDE_SHIFT_REGISTER", reg("capslock-led-mode", 0xbd)),
		kbEnvByte("KEYBOARD_GUIDE_SYM_REGISTER", reg("tables-sel-led-mode", 0xbf)),
		kbEnvByte("KEYBOARD_GUIDE_FN_REGISTER", reg("fn-led-mode", 0xbe)),
	}
	dev, addr := "/dev/i2c-1", uint8(0x4f)
	if links, _ := filepath.Glob(filepath.Join(i2cDir, "supplier:i2c:*")); len(links) > 0 {
		var bus int
		var a uint
		if _, err := fmt.Sscanf(strings.TrimPrefix(filepath.Base(links[0]), "supplier:i2c:"), "%d-%x", &bus, &a); err == nil {
			dev, addr = "/dev/i2c-"+strconv.Itoa(bus), uint8(a)
		}
	}
	if v := os.Getenv("KEYBOARD_GUIDE_MODIFIER_I2C_DEVICE"); v != "" {
		dev = v
	}
	addr = kbEnvByte("KEYBOARD_GUIDE_MODIFIER_I2C_ADDRESS", addr)
	c, err := kbOpenI2C(dev, addr)
	if err != nil {
		return nil
	}
	s := &kbModSource{i2c: c, regs: regs}
	if _, err := s.read(); err != nil {
		s.close()
		return nil
	}
	return s
}

// ---- the backend ----

type kbDevice struct {
	Path  string `json:"path,omitempty"`
	Name  string `json:"name,omitempty"`
	Keys  int    `json:"keys"` // EV_KEY capabilities
	Scan  bool   `json:"scan"` // reports MSC_SCAN
	Event string `json:"event,omitempty"`
}

type kbState struct {
	Available bool      `json:"available"`
	Error     string    `json:"error,omitempty"` // no-device | denied | read
	Device    kbDevice  `json:"device"`
	Layout    *kbLayout `json:"layout"`
	Mods      kbMods    `json:"mods"`
	Pressed   []string  `json:"pressed"`
}

type kbKeyEvent struct {
	ID    string `json:"id"`
	Scan  int    `json:"scan"`
	Code  uint16 `json:"code"`
	Value int32  `json:"value"`
	Name  string `json:"name,omitempty"`
	kbCap
	Mods kbMods `json:"mods"` // state the key was typed with
}

type kbBackend struct {
	mu      sync.Mutex
	hub     *hwHub
	stop    chan struct{}
	f       *os.File
	mods    *kbModSource
	state   kbState
	pressed map[string]bool
	codes   map[uint16]bool // key codes down (for Shift without a modifier source)
	ofNode  string
	i2cDir  string
}

func kbFindDevice() string {
	for _, v := range []string{"KEYBOARD_GUIDE_KEYBOARD_DEVICE", "APPLAUNCH_LINUX_KEYBOARD_DEVICE", "LV_LINUX_KEYBOARD_DEVICE"} {
		if p := os.Getenv(v); p != "" {
			return p
		}
	}
	for _, p := range []string{"/dev/input/by-path/platform-3f804000.i2c-event", "/dev/input/cardputer-zero-internal"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	nodes, _ := filepath.Glob("/sys/class/input/event*")
	for _, n := range nodes {
		if name, err := sysRead(filepath.Join(n, "device", "name")); err == nil && strings.Contains(strings.ToLower(name), "tca8418") {
			return "/dev/input/" + filepath.Base(n)
		}
	}
	return ""
}

func hwKeyboard(mux *http.ServeMux) func() {
	b := &kbBackend{pressed: map[string]bool{}, codes: map[uint16]bool{}}
	b.locate()
	b.hub = newHWHub(b.start, b.stopAll)
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		hwJSON(w, http.StatusOK, b.snapshot())
	})
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		b.hub.serve(w, r, func() (string, any) { return "state", b.snapshot() })
	})
	return b.stopAll
}

// locate finds the keyboard node and reads its layout (no device access).
func (b *kbBackend) locate() {
	path := kbFindDevice()
	b.state.Device = kbDevice{Path: path}
	if path == "" {
		b.state.Error = "no-device"
		b.state.Layout = kbBuiltinLayout()
		return
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		real = path
	}
	b.state.Device.Event = filepath.Base(real)
	b.i2cDir, _ = filepath.EvalSymlinks(filepath.Join("/sys/class/input", filepath.Base(real), "device", "device"))
	b.ofNode = filepath.Join(b.i2cDir, "of_node")
	charMap := os.Getenv("KEYBOARD_GUIDE_CHAR_MAP")
	if charMap == "" {
		charMap = "/usr/share/keymaps/tca8418_keypad_m5stack_keymap.map"
	}
	b.state.Layout = kbReadLayout(b.ofNode, charMap)
}

func (b *kbBackend) snapshot() kbState {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.state
	s.Pressed = []string{}
	for id := range b.pressed {
		s.Pressed = append(s.Pressed, id)
	}
	return s
}

func (b *kbBackend) start() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stop != nil {
		return
	}
	b.stop = make(chan struct{})
	b.openLocked()
	go b.readLoop(b.stop)
	go b.modLoop(b.stop)
}

func (b *kbBackend) stopAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stop == nil {
		return
	}
	close(b.stop)
	b.stop = nil
	if b.f != nil {
		b.f.Close() // unblocks the reader
		b.f = nil
	}
	if b.mods != nil {
		b.mods.close()
		b.mods = nil
	}
	clear(b.pressed)
	clear(b.codes)
	b.state.Available = false
}

// openLocked opens the evdev node read-only and shared (no EVIOCGRAB).
func (b *kbBackend) openLocked() {
	path := b.state.Device.Path
	if path == "" {
		b.state.Error = "no-device"
		return
	}
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		b.state.Available = false
		if errors.Is(err, fs.ErrPermission) {
			b.state.Error = "denied"
		} else {
			b.state.Error = "no-device"
		}
		return
	}
	b.f = f
	b.state.Available, b.state.Error = true, ""
	b.state.Device.Name = kbDeviceName(f)
	if keys, err := kbBits(f, func(n int) uintptr { return kbEVIOCGBIT(kbEvKey, n) }, kbKeyMax+1); err == nil {
		b.state.Device.Keys = kbBitCount(keys)
	}
	if msc, err := kbBits(f, func(n int) uintptr { return kbEVIOCGBIT(kbEvMsc, n) }, 8); err == nil {
		b.state.Device.Scan = kbBitSet(msc, kbMscScan)
	}
	clear(b.pressed)
	clear(b.codes)
	if down, err := kbBits(f, kbEVIOCGKEY, kbKeyMax+1); err == nil {
		for _, code := range kbBitList(down) {
			b.codes[code] = true
			if id := b.state.Layout.keyID(-1, code); id != "" {
				b.pressed[id] = true
			}
		}
	}
	if b.mods == nil && b.i2cDir != "" {
		b.mods = openKbModSource(b.i2cDir, b.ofNode)
	}
	b.state.Mods = b.readModsLocked()
}

func (b *kbBackend) readModsLocked() kbMods {
	if b.mods != nil {
		m, err := b.mods.read()
		if err == nil {
			b.mods.failures = 0
			return m
		}
		// like the original: give up on the source after 4 failed reads
		if b.mods.failures++; b.mods.failures >= 4 {
			b.mods.close()
			b.mods = nil
		} else {
			return b.state.Mods
		}
	}
	shift := kbModOff
	if b.codes[kcLeftShift] || b.codes[kcRightShift] {
		shift = kbModHeld
	}
	return kbMods{Shift: shift, Sym: kbModOff, Fn: kbModOff, Source: "evdev"}
}

func (b *kbBackend) readLoop(stop chan struct{}) {
	buf := make([]byte, kbEventSize*64)
	dec := newKbDecoder()
	for {
		b.mu.Lock()
		f := b.f
		b.mu.Unlock()
		if f == nil {
			// device missing or failed: retry while a window is open
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Second):
			}
			b.mu.Lock()
			if b.stop != stop {
				b.mu.Unlock()
				return
			}
			b.openLocked()
			if b.f != nil {
				b.mu.Unlock()
				b.hub.publish("state", b.snapshot())
				continue
			}
			b.mu.Unlock()
			continue
		}
		n, err := f.Read(buf)
		if err != nil {
			select {
			case <-stop:
				return
			default:
			}
			b.mu.Lock()
			if b.f == f {
				f.Close()
				b.f = nil
				b.state.Available, b.state.Error = false, "read"
				clear(b.pressed)
			}
			b.mu.Unlock()
			b.hub.publish("state", b.snapshot())
			continue
		}
		for _, e := range kbDecodeEvents(buf[:n], kbEventSize) {
			for _, raw := range dec.feed(e) {
				b.handle(raw)
			}
		}
	}
}

// handle publishes one key action. The modifier state that applied to a key
// is the one before the press: the driver clears a one-shot as the key goes
// down, so a fresh "off" read keeps the cached state for this key (the
// original's deferred-inactive rule).
func (b *kbBackend) handle(raw kbRaw) {
	b.mu.Lock()
	l := b.state.Layout
	before := b.state.Mods
	ev := kbKeyEvent{ID: l.keyID(raw.Scan, raw.Code), Scan: raw.Scan, Code: raw.Code, Value: raw.Value}
	applied := b.state.Mods
	if raw.Value == 1 && b.mods != nil {
		fresh := b.readModsLocked()
		applied = kbMergeMods(applied, fresh)
		b.state.Mods = fresh
	}
	if raw.Value >= 0 {
		down := raw.Value != 0
		if down {
			b.codes[raw.Code] = true
		} else {
			delete(b.codes, raw.Code)
		}
		if ev.ID != "" {
			if down {
				b.pressed[ev.ID] = true
			} else {
				delete(b.pressed, ev.ID)
			}
		}
		if b.mods == nil {
			b.state.Mods = b.readModsLocked()
			if raw.Code != kcLeftShift && raw.Code != kcRightShift {
				applied = b.state.Mods
			}
		}
		ev.Name = kbCodeName(raw.Code)
		ev.kbCap = kbCaption(raw.Code, applied.Shift != kbModOff, l.chars)
	}
	ev.Mods = applied
	mods := b.state.Mods
	b.mu.Unlock()
	b.hub.publish("key", ev)
	if !mods.eq(before) {
		b.hub.publish("mods", mods)
	}
}

// kbMergeMods keeps a modifier that was active before a key press when the
// read taken at the press already shows it released.
func kbMergeMods(before, now kbMods) kbMods {
	keep := func(a, b string) string {
		if b == kbModOff && a != "" && a != kbModOff {
			return a
		}
		return b
	}
	return kbMods{Shift: keep(before.Shift, now.Shift), Sym: keep(before.Sym, now.Sym), Fn: keep(before.Fn, now.Fn), Source: now.Source}
}

// modLoop polls the sticky modifier state (the driver gives no event for
// it) at the original app's 25 ms rate.
func (b *kbBackend) modLoop(stop chan struct{}) {
	t := time.NewTicker(25 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		b.mu.Lock()
		if b.mods == nil || b.stop != stop {
			b.mu.Unlock()
			continue
		}
		m := b.readModsLocked()
		changed := !m.eq(b.state.Mods)
		b.state.Mods = m
		b.mu.Unlock()
		if changed {
			b.hub.publish("mods", m)
		}
	}
}
