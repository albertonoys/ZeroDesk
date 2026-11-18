package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Media helpers for the Camera hardware app (hw_camera.go): JPEG stream
// splitting, EXIF reading, the MJPEG AVI container of CardputerZero/Camera
// (MIT, src/services/video_recorder.cpp), rpicam metadata and the shared
// device config file.

// ---- JPEG ----

const camMaxJPEG = 6 << 20

// readJPEG returns the next complete JPEG (SOI..EOI) from a concatenated
// stream such as rpicam-vid --codec mjpeg or ffmpeg -f mjpeg output.
func readJPEG(r *bufio.Reader) ([]byte, error) {
	prev := byte(0)
	for { // find SOI
		c, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if prev == 0xff && c == 0xd8 {
			break
		}
		prev = c
	}
	out := make([]byte, 0, 32<<10)
	out = append(out, 0xff, 0xd8)
	var m byte
	pending := false // m was read at the end of entropy-coded data
	for {
		if len(out) > camMaxJPEG {
			return nil, errors.New("jpeg frame too large")
		}
		if !pending {
			c, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			if c != 0xff {
				return nil, errors.New("jpeg: marker expected")
			}
			if m, err = r.ReadByte(); err != nil {
				return nil, err
			}
			for m == 0xff { // fill bytes
				if m, err = r.ReadByte(); err != nil {
					return nil, err
				}
			}
		}
		pending = false
		out = append(out, 0xff, m)
		if m == 0xd9 {
			return out, nil
		}
		if m == 0x01 || (m >= 0xd0 && m <= 0xd7) {
			continue
		}
		var lb [2]byte
		if _, err := io.ReadFull(r, lb[:]); err != nil {
			return nil, err
		}
		n := int(binary.BigEndian.Uint16(lb[:]))
		if n < 2 || len(out)+n > camMaxJPEG {
			return nil, errors.New("jpeg: bad segment length")
		}
		out = append(out, lb[:]...)
		start := len(out)
		out = append(out, make([]byte, n-2)...)
		if _, err := io.ReadFull(r, out[start:]); err != nil {
			return nil, err
		}
		if m != 0xda {
			continue
		}
		// entropy-coded data runs until a marker that is neither a stuffed
		// 0xFF00 nor a restart marker
		for !pending {
			// copy buffered data up to the next 0xFF in one go
			buf, err := r.Peek(max(1, r.Buffered()))
			if len(buf) == 0 {
				return nil, err
			}
			if i := bytes.IndexByte(buf, 0xff); i != 0 {
				if i < 0 {
					i = len(buf)
				}
				out = append(out, buf[:i]...)
				_, _ = r.Discard(i)
				if len(out) > camMaxJPEG {
					return nil, errors.New("jpeg frame too large")
				}
				continue
			}
			_, _ = r.Discard(1) // the 0xFF
			d, err := r.ReadByte()
			for err == nil && d == 0xff {
				d, err = r.ReadByte()
			}
			if err != nil {
				return nil, err
			}
			if d == 0x00 || (d >= 0xd0 && d <= 0xd7) {
				out = append(out, 0xff, d)
				continue
			}
			m, pending = d, true
		}
	}
}

// jpegSize returns the frame size from the SOF segment.
func jpegSize(b []byte) (w, h int, ok bool) {
	if len(b) < 4 || b[0] != 0xff || b[1] != 0xd8 {
		return 0, 0, false
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xff {
			return 0, 0, false
		}
		m := b[i+1]
		if m == 0xff {
			i++
			continue
		}
		if m == 0xd8 || m == 0x01 || (m >= 0xd0 && m <= 0xd7) {
			i += 2
			continue
		}
		n := int(binary.BigEndian.Uint16(b[i+2:]))
		if (m >= 0xc0 && m <= 0xcf) && m != 0xc4 && m != 0xc8 && m != 0xcc {
			if i+9 > len(b) {
				return 0, 0, false
			}
			return int(binary.BigEndian.Uint16(b[i+7:])), int(binary.BigEndian.Uint16(b[i+5:])), true
		}
		if m == 0xda || m == 0xd9 {
			return 0, 0, false
		}
		i += 2 + n
	}
	return 0, 0, false
}

// ---- EXIF ----

type camExif struct {
	Make, Model, Software, DateTime string
	ExposureUS                      float64 // µs
	FNumber, FocalLength            float64
	ISO                             int
	Width, Height                   int
	Thumb                           []byte // IFD1 JPEG thumbnail
}

// jpegExif returns the APP1 Exif payload (starting at the TIFF header) of a
// JPEG read from the start of r.
func jpegExif(head []byte) []byte {
	if len(head) < 4 || head[0] != 0xff || head[1] != 0xd8 {
		return nil
	}
	i := 2
	for i+4 <= len(head) && head[i] == 0xff {
		m := head[i+1]
		n := int(binary.BigEndian.Uint16(head[i+2:]))
		if m == 0xda || n < 2 {
			return nil
		}
		if m == 0xe1 && i+2+n <= len(head) && n > 8 && string(head[i+4:i+10]) == "Exif\x00\x00" {
			return head[i+10 : i+2+n]
		}
		i += 2 + n
	}
	return nil
}

func parseExif(tiff []byte) (camExif, bool) {
	var x camExif
	if len(tiff) < 8 {
		return x, false
	}
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return x, false
	}
	if bo.Uint16(tiff[2:]) != 42 {
		return x, false
	}
	type entry struct {
		tag, typ uint16
		count    uint32
		val      []byte // the 4 value bytes
	}
	readIFD := func(off uint32) ([]entry, uint32) {
		if int(off)+2 > len(tiff) {
			return nil, 0
		}
		n := int(bo.Uint16(tiff[off:]))
		var es []entry
		p := int(off) + 2
		for k := 0; k < n && p+12 <= len(tiff); k++ {
			es = append(es, entry{bo.Uint16(tiff[p:]), bo.Uint16(tiff[p+2:]), bo.Uint32(tiff[p+4:]), tiff[p+8 : p+12]})
			p += 12
		}
		next := uint32(0)
		if p+4 <= len(tiff) {
			next = bo.Uint32(tiff[p:])
		}
		return es, next
	}
	typeSize := map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 7: 1, 9: 4, 10: 8}
	data := func(e entry) []byte {
		sz := typeSize[e.typ] * int(e.count)
		if sz <= 4 {
			return e.val[:sz]
		}
		o := int(bo.Uint32(e.val))
		if o < 0 || o+sz > len(tiff) {
			return nil
		}
		return tiff[o : o+sz]
	}
	str := func(e entry) string {
		if e.typ != 2 {
			return ""
		}
		return strings.TrimSpace(strings.TrimRight(string(data(e)), "\x00"))
	}
	num := func(e entry) float64 {
		d := data(e)
		switch e.typ {
		case 3:
			if len(d) >= 2 {
				return float64(bo.Uint16(d))
			}
		case 4:
			if len(d) >= 4 {
				return float64(bo.Uint32(d))
			}
		case 9:
			if len(d) >= 4 {
				return float64(int32(bo.Uint32(d)))
			}
		case 5, 10:
			if len(d) >= 8 {
				n, dd := float64(bo.Uint32(d)), float64(bo.Uint32(d[4:]))
				if e.typ == 10 {
					n, dd = float64(int32(bo.Uint32(d))), float64(int32(bo.Uint32(d[4:])))
				}
				if dd != 0 {
					return n / dd
				}
			}
		}
		return 0
	}
	ifd0, ifd1 := readIFD(bo.Uint32(tiff[4:]))
	var exifOff uint32
	for _, e := range ifd0 {
		switch e.tag {
		case 0x010f:
			x.Make = str(e)
		case 0x0110:
			x.Model = str(e)
		case 0x0131:
			x.Software = str(e)
		case 0x0132:
			if x.DateTime == "" {
				x.DateTime = str(e)
			}
		case 0x8769:
			exifOff = uint32(num(e))
		}
	}
	if exifOff != 0 {
		es, _ := readIFD(exifOff)
		for _, e := range es {
			switch e.tag {
			case 0x829a:
				x.ExposureUS = num(e) * 1e6
			case 0x829d:
				x.FNumber = num(e)
			case 0x8827:
				x.ISO = int(num(e))
			case 0x9003:
				x.DateTime = str(e)
			case 0x920a:
				x.FocalLength = num(e)
			case 0xa002:
				x.Width = int(num(e))
			case 0xa003:
				x.Height = int(num(e))
			}
		}
	}
	if ifd1 != 0 && int(ifd1) < len(tiff) {
		es, _ := readIFD(ifd1)
		var off, n int
		for _, e := range es {
			switch e.tag {
			case 0x0201:
				off = int(num(e))
			case 0x0202:
				n = int(num(e))
			}
		}
		if off > 0 && n > 0 && off+n <= len(tiff) && n > 2 && tiff[off] == 0xff && tiff[off+1] == 0xd8 {
			x.Thumb = tiff[off : off+n]
		}
	}
	return x, true
}

// readJPEGHead reads enough of a JPEG file for its EXIF block and SOF.
func readJPEGHead(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := make([]byte, 160<<10)
	n, err := io.ReadFull(f, b)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return b[:n], nil
}

// ---- MJPEG AVI (format of CardputerZero/Camera's MjpegAviWriter) ----

type aviWriter struct {
	f                            *os.File
	path                         string
	w, h, fps                    int
	frames                       uint32
	index                        []uint32 // offset, size pairs
	riffOff, totalOff, lengthOff int64
	moviSizeOff, moviStart, pos  int64
	usOff, rateOff               int64
	first, last                  time.Time
	err                          error
}

func (a *aviWriter) put(v ...any) {
	if a.err != nil {
		return
	}
	for _, x := range v {
		var err error
		switch t := x.(type) {
		case string:
			_, err = a.f.WriteString(t)
			a.pos += int64(len(t))
		case uint32:
			err = binary.Write(a.f, binary.LittleEndian, t)
			a.pos += 4
		case uint16:
			err = binary.Write(a.f, binary.LittleEndian, t)
			a.pos += 2
		case []byte:
			_, err = a.f.Write(t)
			a.pos += int64(len(t))
		}
		if err != nil {
			a.err = err
			return
		}
	}
}

func (a *aviWriter) patch(off int64, v uint32) {
	if a.err != nil {
		return
	}
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	if _, err := a.f.WriteAt(b[:], off); err != nil {
		a.err = err
	}
}

func newAVIWriter(path string, w, h, fps int) (*aviWriter, error) {
	if w <= 0 || h <= 0 || fps <= 0 {
		return nil, errors.New("avi: bad size")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	a := &aviWriter{f: f, path: path, w: w, h: h, fps: fps}
	frameBytes := uint32(w * h * 3)
	a.put("RIFF")
	a.riffOff = a.pos
	a.put(uint32(0), "AVI ", "LIST")
	hdrlSizeOff := a.pos
	a.put(uint32(0))
	hdrlStart := a.pos
	a.put("hdrl", "avih", uint32(56))
	a.usOff = a.pos
	a.put(uint32(1000000/fps), uint32(0), uint32(0), uint32(0x10))
	a.totalOff = a.pos
	a.put(uint32(0), uint32(0), uint32(1), frameBytes, uint32(w), uint32(h), uint32(0), uint32(0), uint32(0), uint32(0))
	a.put("LIST")
	strlSizeOff := a.pos
	a.put(uint32(0))
	strlStart := a.pos
	a.put("strl", "strh", uint32(56), "vids", "MJPG", uint32(0), uint16(0), uint16(0), uint32(0), uint32(1))
	a.rateOff = a.pos
	a.put(uint32(fps), uint32(0))
	a.lengthOff = a.pos
	a.put(uint32(0), frameBytes, uint32(0xffffffff), uint32(0), uint16(0), uint16(0), uint16(min(w, 65535)), uint16(min(h, 65535)))
	a.put("strf", uint32(40), uint32(40), uint32(w), uint32(h), uint16(1), uint16(24), "MJPG", frameBytes, uint32(0), uint32(0), uint32(0), uint32(0))
	a.patch(strlSizeOff, uint32(a.pos-strlStart))
	a.patch(hdrlSizeOff, uint32(a.pos-hdrlStart))
	a.put("LIST")
	a.moviSizeOff = a.pos
	a.put(uint32(0), "movi")
	a.moviStart = a.pos
	if a.err != nil {
		f.Close()
		os.Remove(path)
		return nil, a.err
	}
	return a, nil
}

func (a *aviWriter) writeFrame(jpeg []byte) error {
	return a.writeFrameAt(jpeg, time.Now())
}

func (a *aviWriter) writeFrameAt(jpeg []byte, t time.Time) error {
	if len(jpeg) == 0 {
		return nil
	}
	if a.frames == 0 {
		a.first = t
	}
	a.last = t
	off := a.pos - a.moviStart
	a.put("00dc", uint32(len(jpeg)), jpeg)
	if len(jpeg)&1 == 1 {
		a.put([]byte{0})
	}
	a.index = append(a.index, uint32(off), uint32(len(jpeg)))
	a.frames++
	return a.err
}

func (a *aviWriter) close() error {
	beforeIdx := a.pos
	a.put("idx1", uint32(len(a.index)/2*16))
	for i := 0; i < len(a.index); i += 2 {
		a.put("00dc", uint32(0x10), a.index[i], a.index[i+1])
	}
	end := a.pos
	// the header carries the rate the frames actually arrived at
	if a.frames > 1 {
		if el := a.last.Sub(a.first); el > 0 {
			us := el.Microseconds() / int64(a.frames-1)
			if us > 0 {
				a.patch(a.usOff, uint32(us))
				a.patch(a.rateOff, uint32(max(1, (1000000+us/2)/us)))
			}
		}
	}
	a.patch(a.riffOff, uint32(min(end-8, math.MaxUint32)))
	a.patch(a.totalOff, a.frames)
	a.patch(a.lengthOff, a.frames)
	a.patch(a.moviSizeOff, uint32(beforeIdx-a.moviSizeOff-4))
	err := a.err
	if cerr := a.f.Close(); err == nil {
		err = cerr
	}
	return err
}

type aviInfo struct {
	W, H, FPS int
	Frames    []struct{ Off, Size int64 } // absolute file offsets of the JPEG data
}

// readAVI parses an MJPEG AVI's header and frame list (from idx1, or by
// walking the movi list when the index is missing, e.g. an interrupted
// recording).
func readAVI(f io.ReaderAt, size int64) (aviInfo, error) {
	var info aviInfo
	hdr := make([]byte, 12)
	if _, err := f.ReadAt(hdr, 0); err != nil || string(hdr[:4]) != "RIFF" || string(hdr[8:12]) != "AVI " {
		return info, errors.New("not an AVI file")
	}
	u32 := func(off int64) (uint32, error) {
		var b [4]byte
		if _, err := f.ReadAt(b[:], off); err != nil {
			return 0, err
		}
		return binary.LittleEndian.Uint32(b[:]), nil
	}
	id := func(off int64) string {
		var b [4]byte
		if _, err := f.ReadAt(b[:], off); err != nil {
			return ""
		}
		return string(b[:])
	}
	var moviStart, moviEnd int64 = -1, -1
	var idxOff, idxLen int64 = -1, 0
	for p := int64(12); p+8 <= size; {
		cid := id(p)
		n, err := u32(p + 4)
		if err != nil {
			break
		}
		switch {
		case cid == "LIST" && id(p+8) == "hdrl":
			// avih follows "hdrl": microseconds per frame, then width/height at +32/+36
			if id(p+12) == "avih" {
				us, _ := u32(p + 20)
				if us > 0 {
					info.FPS = int((1000000 + us/2) / us)
				}
				w, _ := u32(p + 20 + 32)
				h, _ := u32(p + 20 + 36)
				info.W, info.H = int(w), int(h)
			}
		case cid == "LIST" && id(p+8) == "movi":
			moviStart, moviEnd = p+8, min(p+8+int64(n), size)
			if n == 0 { // never finalised
				moviEnd = size
			}
		case cid == "idx1":
			idxOff, idxLen = p+8, int64(n)
		}
		if n == 0 && cid == "LIST" && id(p+8) == "movi" {
			break
		}
		p += 8 + int64(n) + int64(n&1)
	}
	if moviStart < 0 {
		return info, errors.New("avi: no movi list")
	}
	if idxOff > 0 && idxLen >= 16 {
		buf := make([]byte, idxLen)
		if _, err := f.ReadAt(buf, idxOff); err == nil {
			// idx1 offsets are relative to the "movi" fourcc in the AVI spec,
			// to the byte after it in the original's writer, or absolute in
			// some files: use the base under which the first entry is a chunk
			base := int64(-1)
			first := int64(binary.LittleEndian.Uint32(buf[8:]))
			for _, c := range []int64{moviStart + 4, moviStart, 0} {
				if cid := id(c + first); len(cid) == 4 && cid[2:] == "dc" {
					base = c
					break
				}
			}
			for i := 0; base >= 0 && i+16 <= len(buf); i += 16 {
				off := int64(binary.LittleEndian.Uint32(buf[i+8:]))
				sz := int64(binary.LittleEndian.Uint32(buf[i+12:]))
				abs := base + off + 8
				if abs+sz <= size {
					info.Frames = append(info.Frames, struct{ Off, Size int64 }{abs, sz})
				}
			}
			if base >= 0 {
				return info, nil
			}
		}
	}
	for p := moviStart + 4; p+8 <= moviEnd; {
		n, err := u32(p + 4)
		if err != nil || p+8+int64(n) > size {
			break
		}
		if cid := id(p); len(cid) == 4 && cid[2:] == "dc" {
			info.Frames = append(info.Frames, struct{ Off, Size int64 }{p + 8, int64(n)})
		}
		p += 8 + int64(n) + int64(n&1)
	}
	return info, nil
}

// ---- rpicam metadata (--metadata-format txt) ----

type camMeta struct {
	ExposureUS  float64
	Gain        float64 // analogue × digital
	RedGain     float64
	BlueGain    float64
	Lux         float64
	ColourTempK float64
	Valid       bool
}

// parseCamMeta parses one "Key=value" block of rpicam's txt metadata.
func parseCamMeta(lines []string) camMeta {
	var m camMeta
	ag, dg := 0.0, 1.0
	for _, l := range lines {
		k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
		if !ok {
			continue
		}
		f := func() float64 {
			x, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
			return x
		}
		switch k {
		case "ExposureTime":
			m.ExposureUS = f()
		case "AnalogueGain":
			ag = f()
		case "DigitalGain":
			dg = f()
		case "Lux":
			m.Lux = f()
		case "ColourTemperature":
			m.ColourTempK = f()
		case "ColourGains":
			v = strings.Trim(strings.TrimSpace(v), "[]")
			if r, b, ok := strings.Cut(v, ","); ok {
				m.RedGain, _ = strconv.ParseFloat(strings.TrimSpace(r), 64)
				m.BlueGain, _ = strconv.ParseFloat(strings.TrimSpace(b), 64)
			}
		}
	}
	m.Gain = ag * dg
	m.Valid = m.ExposureUS > 0 && ag > 0
	return m
}

// ---- shared device config (~/.config/cardputerzero/config.json) ----

// The original reads camera.resolution.{width,height}; only 3280x2464 and
// 1640x1232 are accepted, anything else means the 3280x2464 default.
var camResolutions = [][2]int{{3280, 2464}, {1640, 1232}}

func camConfigPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/home/pi"
	}
	return filepath.Join(home, ".config", "cardputerzero", "config.json")
}

func loadCamResolution(path string) [2]int {
	b, err := os.ReadFile(path)
	if err != nil {
		return camResolutions[0]
	}
	var cfg struct {
		Camera struct {
			Resolution struct {
				Width, Height json.Number
			} `json:"resolution"`
		} `json:"camera"`
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if d.Decode(&cfg) != nil {
		return camResolutions[0]
	}
	w, e1 := cfg.Camera.Resolution.Width.Int64()
	h, e2 := cfg.Camera.Resolution.Height.Int64()
	if e1 != nil || e2 != nil {
		return camResolutions[0]
	}
	for _, r := range camResolutions {
		if int64(r[0]) == w && int64(r[1]) == h {
			return r
		}
	}
	return camResolutions[0]
}

// saveCamResolution sets camera.resolution in the shared config, keeping every
// other key.
func saveCamResolution(path string, res [2]int) error {
	root := map[string]any{}
	mode := os.FileMode(0o644)
	if b, err := os.ReadFile(path); err == nil {
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		if err := d.Decode(&root); err != nil {
			return fmt.Errorf("config.json: %w", err)
		}
		if root == nil {
			root = map[string]any{}
		}
		if st, err := os.Stat(path); err == nil {
			mode = st.Mode().Perm()
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	cam, _ := root["camera"].(map[string]any)
	if cam == nil {
		cam = map[string]any{}
	}
	cam["resolution"] = map[string]any{"width": res[0], "height": res[1]}
	root["camera"] = cam
	var buf bytes.Buffer
	e := json.NewEncoder(&buf)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	if err := e.Encode(root); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
