package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testJPEG(t *testing.T, w, h int, restart bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x * 7), uint8(y * 13), uint8(x ^ y), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if restart {
		// insert a DRI segment and stuffed/RST bytes are exercised by real
		// encoders; here we only check the splitter tolerates an extra APP segment
		app := []byte{0xff, 0xe0, 0x00, 0x06, 'Z', 'D', 0xff, 0xd9}
		b = append(append(append([]byte{}, b[:2]...), app...), b[2:]...)
	}
	return b
}

func TestReadJPEGSplitsStream(t *testing.T) {
	a := testJPEG(t, 33, 17, false)
	b := testJPEG(t, 64, 48, true)
	var stream []byte
	stream = append(stream, 0x00, 0x12, 0xff) // junk before the first frame
	stream = append(stream, a...)
	stream = append(stream, b...)
	stream = append(stream, a...)
	for _, size := range []int{16, 4096} { // tiny buffers cross every boundary
		r := bufio.NewReaderSize(bytes.NewReader(stream), size)
		for i, want := range [][]byte{a, b, a} {
			got, err := readJPEG(r)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("buffer %d frame %d: %v", size, i, err)
			}
		}
	}
	r := bufio.NewReader(bytes.NewReader(stream))
	for i, want := range [][]byte{a, b, a} {
		got, err := readJPEG(r)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("frame %d: got %d bytes, want %d", i, len(got), len(want))
		}
		if _, err := jpeg.Decode(bytes.NewReader(got)); err != nil {
			t.Fatalf("frame %d does not decode: %v", i, err)
		}
	}
	if _, err := readJPEG(r); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
	if w, h, ok := jpegSize(b); !ok || w != 64 || h != 48 {
		t.Fatalf("jpegSize = %d %d %v", w, h, ok)
	}
}

func TestReadJPEGEntropyMarkers(t *testing.T) {
	// SOI, SOS with 1-byte header, entropy data with FF00 and RST0, EOI
	frame := []byte{0xff, 0xd8, 0xff, 0xda, 0x00, 0x03, 0x01, 0x11, 0xff, 0x00, 0x22, 0xff, 0xd0, 0x33, 0xff, 0xff, 0xd9}
	got, err := readJPEG(bufio.NewReader(bytes.NewReader(append(append([]byte{}, frame...), frame...))))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0xff, 0xd8, 0xff, 0xda, 0x00, 0x03, 0x01, 0x11, 0xff, 0x00, 0x22, 0xff, 0xd0, 0x33, 0xff, 0xd9}
	if !bytes.Equal(got, want) {
		t.Fatalf("got % x", got)
	}
}

// buildExif makes a little-endian TIFF block with the tags the gallery shows
// and an IFD1 thumbnail.
func buildExif(thumb []byte) []byte {
	le := binary.LittleEndian
	var b []byte
	u16 := func(v uint16) { b = le.AppendUint16(b, v) }
	u32 := func(v uint32) { b = le.AppendUint32(b, v) }
	b = append(b, 'I', 'I')
	u16(42)
	u32(8)
	// IFD0 at 8: Make, Model, ExifIFD; strings stored after the IFDs
	const ifd0 = 8
	const ifd0Len = 2 + 3*12 + 4
	const exifIFD = ifd0 + ifd0Len
	const exifLen = 2 + 3*12 + 4
	const ifd1 = exifIFD + exifLen
	const ifd1Len = 2 + 2*12 + 4
	const data = ifd1 + ifd1Len
	makeS, modelS := "M5Stack\x00", "CardputerZero IMX219\x00"
	makeOff := uint32(data)
	modelOff := makeOff + uint32(len(makeS))
	expOff := modelOff + uint32(len(modelS))
	fnOff := expOff + 8
	thumbOff := fnOff + 8
	u16(3)
	u16(0x010f)
	u16(2)
	u32(uint32(len(makeS)))
	u32(makeOff)
	u16(0x0110)
	u16(2)
	u32(uint32(len(modelS)))
	u32(modelOff)
	u16(0x8769)
	u16(4)
	u32(1)
	u32(exifIFD)
	u32(ifd1)
	// Exif IFD: ExposureTime 1/60, FNumber 2.2, ISO 400
	u16(3)
	u16(0x829a)
	u16(5)
	u32(1)
	u32(expOff)
	u16(0x829d)
	u16(5)
	u32(1)
	u32(fnOff)
	u16(0x8827)
	u16(3)
	u32(1)
	u32(400)
	u32(0)
	// IFD1: thumbnail
	u16(2)
	u16(0x0201)
	u16(4)
	u32(1)
	u32(thumbOff)
	u16(0x0202)
	u16(4)
	u32(1)
	u32(uint32(len(thumb)))
	u32(0)
	b = append(b, makeS...)
	b = append(b, modelS...)
	u32(1)
	u32(60)
	u32(22)
	u32(10)
	b = append(b, thumb...)
	return b
}

func withExif(jpg, tiff []byte) []byte {
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xff, 0xe1, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}
	out := append([]byte{}, jpg[:2]...)
	out = append(out, app1...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

func TestParseExif(t *testing.T) {
	thumb := testJPEG(t, 16, 12, false)
	photo := withExif(testJPEG(t, 80, 60, false), buildExif(thumb))
	x, ok := parseExif(jpegExif(photo))
	if !ok {
		t.Fatal("no exif")
	}
	if x.Make != "M5Stack" || x.Model != "CardputerZero IMX219" || x.ISO != 400 {
		t.Fatalf("%+v", x)
	}
	if d := x.ExposureUS - 1e6/60; d > 0.01 || d < -0.01 {
		t.Fatalf("exposure %v", x.ExposureUS)
	}
	if x.FNumber != 2.2 {
		t.Fatalf("f %v", x.FNumber)
	}
	if !bytes.Equal(x.Thumb, thumb) {
		t.Fatal("thumbnail mismatch")
	}
	if w, h, ok := jpegSize(photo); !ok || w != 80 || h != 60 {
		t.Fatalf("size %d %d", w, h)
	}
	if _, ok := parseExif(jpegExif(testJPEG(t, 8, 8, false))); ok {
		t.Fatal("exif found in a plain JPEG")
	}
}

func TestAVIRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "VID_20261118_120000.avi")
	a, err := newAVIWriter(p, 64, 48, 15)
	if err != nil {
		t.Fatal(err)
	}
	frames := [][]byte{testJPEG(t, 64, 48, false), testJPEG(t, 64, 48, true), testJPEG(t, 64, 48, false)[:101]}
	t0 := time.Now()
	for i, f := range frames {
		if err := a.writeFrameAt(f, t0.Add(time.Duration(i)*80*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.close(); err != nil {
		t.Fatal(err)
	}
	check := func(path string) {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		st, _ := f.Stat()
		info, err := readAVI(f, st.Size())
		if err != nil {
			t.Fatal(err)
		}
		if info.W != 64 || info.H != 48 || info.FPS != 13 || len(info.Frames) != len(frames) { // 80 ms apart
			t.Fatalf("%+v", info)
		}
		for i, fr := range info.Frames {
			got := make([]byte, fr.Size)
			if _, err := f.ReadAt(got, fr.Off); err != nil || !bytes.Equal(got, frames[i]) {
				t.Fatalf("frame %d differs", i)
			}
		}
	}
	check(p)
	// RIFF header fields as the original writes them
	raw, _ := os.ReadFile(p)
	if string(raw[:4]) != "RIFF" || binary.LittleEndian.Uint32(raw[4:]) != uint32(len(raw)-8) {
		t.Fatal("riff size")
	}
	if binary.LittleEndian.Uint32(raw[48:]) != uint32(len(frames)) { // avih total frames
		t.Fatalf("total frames %d", binary.LittleEndian.Uint32(raw[48:]))
	}
	// an interrupted recording (no idx1, sizes never patched) still plays
	q := filepath.Join(dir, "VID_cut.avi")
	cut := bytes.Index(raw, []byte("idx1"))
	trunc := append([]byte{}, raw[:cut]...)
	moviSize := bytes.Index(trunc, []byte("movi")) - 4
	binary.LittleEndian.PutUint32(trunc[moviSize:], 0)
	if err := os.WriteFile(q, trunc, 0o644); err != nil {
		t.Fatal(err)
	}
	check(q)
}

func TestParseCamMeta(t *testing.T) {
	block := strings.Split(`ExposureTime=66623
ColourGains=[ 1.419388, 1.706080 ]
AnalogueGain=9.846154
FrameDuration=66662
Lux=0.245020
DigitalGain=1.015625
ColourTemperature=4000`, "\n")
	m := parseCamMeta(block)
	if !m.Valid || m.ExposureUS != 66623 || m.RedGain != 1.419388 || m.BlueGain != 1.70608 || m.Lux != 0.24502 {
		t.Fatalf("%+v", m)
	}
	if g := m.Gain; g < 9.99 || g > 10.01 {
		t.Fatalf("gain %v", g)
	}
	args := strings.Join(camStillArgs("/x.jpg", [2]int{3280, 2464}, "imx219", m, true, ""), " ")
	for _, want := range []string{"--immediate", "--shutter 66623", "--gain 10.000", "--awbgains 1.4194,1.7061", "--width 3280", "--rotation 180", "IFD0.Model=CardputerZero IMX219"} {
		if !strings.Contains(args, want) {
			t.Fatalf("%q missing in %s", want, args)
		}
	}
	if args := strings.Join(camStillArgs("/x.jpg", [2]int{1640, 1232}, "", m, false, "0.1,0.1,0.4,0.4"), " "); strings.Contains(args, "--immediate") || !strings.Contains(args, "-t 1200") || !strings.Contains(args, "--roi 0.1,0.1,0.4,0.4") {
		t.Fatal(args)
	}
}

func TestCamROI(t *testing.T) {
	if camROI(100, 10, 90) != "" {
		t.Fatal("no zoom must not crop")
	}
	if got := camROI(250, 50, 50); got != "0.3000,0.3000,0.4000,0.4000" {
		t.Fatal(got)
	}
	// rotated 180°: looking at the visible left edge crops the sensor's right side
	if got := camROI(500, 0, 100); got != "0.8000,0.0000,0.2000,0.2000" {
		t.Fatal(got)
	}
}

func TestCamConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cardputerzero", "config.json")
	if r := loadCamResolution(p); r != [2]int{3280, 2464} {
		t.Fatal(r)
	}
	orig := `{
  "appstore": {
    "registry": {
      "0": {
        "entry": "Hub\thttps://x/registry.json?a=1&b=<2>"
      }
    }
  },
  "big": 12345678901234567890,
  "camera": {
    "other": true
  }
}
`
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(orig), 0o600)
	if err := saveCamResolution(p, [2]int{1640, 1232}); err != nil {
		t.Fatal(err)
	}
	if r := loadCamResolution(p); r != [2]int{1640, 1232} {
		t.Fatal(r)
	}
	b, _ := os.ReadFile(p)
	var v map[string]any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"entry": "Hub\thttps://x/registry.json?a=1&b=<2>"`) || !strings.Contains(string(b), "12345678901234567890") {
		t.Fatalf("other keys changed:\n%s", b)
	}
	if v["camera"].(map[string]any)["other"] != true {
		t.Fatal("camera.other lost")
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatal("mode changed")
	}
	os.WriteFile(p, []byte(`{"camera":{"resolution":{"width":1920,"height":1080}}}`), 0o644)
	if r := loadCamResolution(p); r != [2]int{3280, 2464} {
		t.Fatal("unsupported resolution must fall back to the default", r)
	}
}

func TestCamNames(t *testing.T) {
	tm := time.Date(2026, 11, 18, 9, 5, 7, 0, time.Local)
	taken := map[string]bool{"CAM_20261118_090507.jpg": true, "CAM_20261118_090507_2.jpg": true}
	if n := camFileName("CAM_", ".jpg", tm, func(s string) bool { return taken[s] }); n != "CAM_20261118_090507_3.jpg" {
		t.Fatal(n)
	}
	for name, ok := range map[string]bool{
		"CAM_20261118_090507.jpg": true, "VID_20261118_090507.avi": true, "CAM_x.JPG": true,
		"../CAM_a.jpg": false, "CAM_a/../../b.jpg": false, "IMG_1.jpg": false, "CAM_..jpg": false, "VID_a.mp4": false,
	} {
		if camNameRe.MatchString(name) && !strings.Contains(name, "..") != ok {
			t.Fatalf("%s: want %v", name, ok)
		}
	}
}

func TestClassifyCamError(t *testing.T) {
	busy := "[0:01] INFO Camera camera.cpp:1021 Pipeline handler in use by another process\nERROR: *** failed to acquire camera /base/soc/i2c0mux/i2c@1/imx219@10 ***\n"
	if s, _ := classifyCamError(busy); s != "busy" {
		t.Fatal(s)
	}
	if s, _ := classifyCamError("ERROR: *** no cameras available ***\n"); s != "nocamera" {
		t.Fatal(s)
	}
	if s, m := classifyCamError("[x] INFO foo\nERROR: *** something odd ***\n"); s != "error" || m != "something odd" {
		t.Fatal(s, m)
	}
	if m := camSensorRe.FindStringSubmatch("Registered camera /base/soc/i2c0mux/i2c@1/imx219@10 to Unicam"); m == nil || m[1] != "imx219" {
		t.Fatal(m)
	}
}

func TestCamUSBMaxSize(t *testing.T) {
	out := `[video4linux2,v4l2 @ 0x55] Compressed:       mjpeg :          Motion-JPEG : 640x480 1280x720 1920x1080 320x240
[video4linux2,v4l2 @ 0x55] Raw       :     yuyv422 :           YUYV 4:2:2 : 640x480 2592x1944`
	if w, h := camUSBMaxSize(out); w != 1920 || h != 1080 {
		t.Fatal(w, h)
	}
}

func TestCamLibraryOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(camPhotoDir(), 0o755)
	os.MkdirAll(camVideoDir(), 0o755)
	for _, n := range []string{"CAM_20261101_100000.jpg", "CAM_20261118_090000.jpg", "notes.txt", ".CAM_20261118_100000.jpg.part"} {
		os.WriteFile(filepath.Join(camPhotoDir(), n), []byte("x"), 0o644)
	}
	os.WriteFile(filepath.Join(camVideoDir(), "VID_20261110_120000.avi"), []byte("x"), 0o644)
	var got []string
	for _, it := range camLibrary() {
		got = append(got, it.Name+":"+it.Kind)
	}
	want := "CAM_20261118_090000.jpg:photo VID_20261110_120000.avi:video CAM_20261101_100000.jpg:photo"
	if strings.Join(got, " ") != want {
		t.Fatal(got)
	}
	if _, _, ok := camMediaPath("VID_20261110_120000.avi"); !ok {
		t.Fatal("video not found")
	}
	if _, _, ok := camMediaPath("CAM_missing.jpg"); ok {
		t.Fatal("missing file found")
	}
}
