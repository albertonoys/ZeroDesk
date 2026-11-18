package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Camera: the CSI camera (IMX219 through libcamera / unicam) or a USB UVC
// camera. Ported from CardputerZero/Camera (MIT): photos are saved where the
// original saves them (~/Pictures/DCIM/Camera/CAM_YYYYMMDD_HHMMSS.jpg, with
// the same EXIF make/model), videos as its MJPEG AVI (~/Videos/Camera/
// VID_*.avi), the still resolution is the original's
// camera.resolution in ~/.config/cardputerzero/config.json, zoom has its
// 1x/2.5x/5x steps with panning and the image is rotated 180°, as the sensor
// is mounted upside down. Without cgo the backend drives the installed
// rpicam-apps (rpicam-vid streams MJPEG and per-frame metadata, rpicam-still
// takes the photo with the preview's exposure locked) and ffmpeg for USB
// cameras and thumbnails. The camera runs only while a window shows the
// preview.

const (
	camPreviewW   = 640
	camPreviewH   = 480
	camPreviewFPS = 15
	// The CardputerZero's IMX219 delivers 5/6 of the frame rate libcamera is
	// asked for (measured from sensor timestamps: 15 -> 12.5 fps, 30 -> 25 fps),
	// so 18 is requested for a 15 fps preview.
	camRequestFPS = 18
	camSensorMode = "1640:1232:10:P" // full field of view, 2x2 binned
	camGrace      = 2 * time.Second  // keep the camera through a reload
)

var camZoomLevels = []int{100, 250, 500}

const camPanStep = 8 // percent, as the original

type camState struct {
	State       string   `json:"state"` // idle starting live capturing busy nocamera error
	Holder      string   `json:"holder,omitempty"`
	Error       string   `json:"error,omitempty"`
	Sensor      string   `json:"sensor,omitempty"`
	Source      string   `json:"source"` // csi | usb
	USB         string   `json:"usb,omitempty"`
	Resolution  [2]int   `json:"resolution"`
	Resolutions [][2]int `json:"resolutions"`
	Zoom        int      `json:"zoom"`
	ViewX       int      `json:"view_x"`
	ViewY       int      `json:"view_y"`
	CanZoom     bool     `json:"can_zoom"`
	FPS         float64  `json:"fps"`
	Lux         float64  `json:"lux"`
	HaveLux     bool     `json:"have_lux"`
	Recording   bool     `json:"recording"`
	RecordSince int64    `json:"record_since,omitempty"` // unix ms
	Preview     [2]int   `json:"preview"`
}

type camProc struct {
	cmd     *exec.Cmd
	done    chan struct{}
	stopped bool
	tail    *camTail
	frames  int
	usbRaw  bool
	started time.Time
}

type camBackend struct {
	ctl sync.Mutex // serialises starting/stopping processes
	mu  sync.Mutex
	hub *hwHub

	frames camFrames

	proc     *camProc
	viewers  int
	watching bool
	restart  bool

	state, holder, errMsg string
	sensor                string
	retryAt               time.Time
	backoff               time.Duration
	usbRaw                bool

	source  string
	res     [2]int
	zoom    int
	viewX   int
	viewY   int
	meta    camMeta
	metaAt  time.Time
	fps     float64
	nFrames int
	fpsAt   time.Time
	preview [2]int

	capturing bool
	recMu     sync.Mutex
	rec       *aviWriter
	recSince  time.Time

	thumbMu sync.Mutex
}

func init() { hwApps["camera_app"] = hwCamera }

func camHome() string {
	h, _ := os.UserHomeDir()
	if h == "" {
		h = "/home/pi"
	}
	return h
}

func camPhotoDir() string { return filepath.Join(camHome(), "Pictures", "DCIM", "Camera") }
func camVideoDir() string { return filepath.Join(camHome(), "Videos", "Camera") }
func camThumbDir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "CameraApp", "zerodesk")
	}
	return filepath.Join(camHome(), ".cache", "CameraApp", "zerodesk")
}

func hwCamera(mux *http.ServeMux) func() {
	b := &camBackend{source: "csi", res: loadCamResolution(camConfigPath()), zoom: 100, viewX: 50, viewY: 50, state: "idle"}
	b.frames.next = make(chan struct{})
	b.hub = newHWHub(nil, nil)
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		hwJSON(w, http.StatusOK, b.snapshot())
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		b.hub.serve(w, r, func() (string, any) { return "state", b.snapshot() })
	})
	mux.HandleFunc("GET /api/preview", b.servePreview)
	mux.HandleFunc("POST /api/capture", func(w http.ResponseWriter, r *http.Request) {
		name, status, err := b.capture()
		if err != nil {
			hwError(w, status, err.Error())
			return
		}
		hwJSON(w, http.StatusOK, map[string]string{"name": name})
	})
	mux.HandleFunc("POST /api/record/start", func(w http.ResponseWriter, r *http.Request) {
		if err := b.startRecording(); err != nil {
			hwError(w, http.StatusConflict, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/record/stop", func(w http.ResponseWriter, r *http.Request) {
		name, err := b.stopRecording()
		if err != nil {
			hwError(w, http.StatusInternalServerError, err.Error())
			return
		}
		hwJSON(w, http.StatusOK, map[string]string{"name": name})
	})
	mux.HandleFunc("POST /api/settings", b.handleSettings)
	mux.HandleFunc("GET /api/media", func(w http.ResponseWriter, r *http.Request) {
		hwJSON(w, http.StatusOK, camLibrary())
	})
	mux.HandleFunc("GET /api/media/{name}", b.serveMedia)
	mux.HandleFunc("GET /api/media/{name}/thumb", b.serveThumb)
	mux.HandleFunc("GET /api/media/{name}/info", func(w http.ResponseWriter, r *http.Request) {
		p, kind, ok := camMediaPath(r.PathValue("name"))
		if !ok {
			hwError(w, http.StatusNotFound, "not found")
			return
		}
		info, err := camMediaInfo(p, kind)
		if err != nil {
			hwError(w, http.StatusNotFound, err.Error())
			return
		}
		hwJSON(w, http.StatusOK, info)
	})
	mux.HandleFunc("GET /api/media/{name}/play", b.servePlay)
	mux.HandleFunc("DELETE /api/media/{name}", func(w http.ResponseWriter, r *http.Request) {
		p, _, ok := camMediaPath(r.PathValue("name"))
		if !ok {
			hwError(w, http.StatusNotFound, "not found")
			return
		}
		if err := os.Remove(p); err != nil {
			hwError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = os.Remove(filepath.Join(camThumbDir(), filepath.Base(p)+".jpg"))
		b.hub.publish("library", map[string]string{"deleted": filepath.Base(p)})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/sound/shutter", func(w http.ResponseWriter, r *http.Request) {
		for _, p := range []string{"/usr/share/camera_app/audio/shutter.wav", "/usr/share/CameraApp/assets/audio/shutter.wav"} {
			if f, err := os.Open(p); err == nil {
				defer f.Close()
				st, _ := f.Stat()
				w.Header().Set("Cache-Control", "max-age=86400")
				http.ServeContent(w, r, "shutter.wav", st.ModTime(), f)
				return
			}
		}
		http.NotFound(w, r)
	})
	return b.shutdown
}

// ---- state ----

func (b *camBackend) snapshot() camState {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := camState{
		State: b.state, Holder: b.holder, Error: b.errMsg, Sensor: b.sensor, Source: b.source,
		Resolution: b.res, Resolutions: camResolutions, Zoom: b.zoom, ViewX: b.viewX, ViewY: b.viewY,
		CanZoom: b.source == "csi", FPS: b.fps, Preview: b.preview,
	}
	if b.capturing {
		s.State = "capturing"
	}
	if b.meta.Valid && time.Since(b.metaAt) < 3*time.Second {
		s.Lux, s.HaveLux = b.meta.Lux, true
	}
	if u := camUSBDevices(); len(u) > 0 {
		s.USB = u[0].name
	}
	b.recMu.Lock()
	if b.rec != nil {
		s.Recording, s.RecordSince = true, b.recSince.UnixMilli()
	}
	b.recMu.Unlock()
	return s
}

func (b *camBackend) publish() { b.hub.publish("state", b.snapshot()) }

func (b *camBackend) setState(state, holder, msg string) {
	b.mu.Lock()
	changed := b.state != state || b.holder != holder || b.errMsg != msg
	b.state, b.holder, b.errMsg = state, holder, msg
	if state != "live" {
		b.fps = 0
	}
	b.mu.Unlock()
	if changed {
		b.publish()
	}
}

// ---- preview viewers ----

type camFrames struct {
	mu    sync.Mutex
	frame []byte
	seq   uint64
	next  chan struct{}
}

func (f *camFrames) put(b []byte) {
	f.mu.Lock()
	f.frame = b
	f.seq++
	close(f.next)
	f.next = make(chan struct{})
	f.mu.Unlock()
}

func (f *camFrames) wait(ctx context.Context, after uint64) ([]byte, uint64, bool) {
	for {
		f.mu.Lock()
		if f.seq > after && f.frame != nil {
			b, s := f.frame, f.seq
			f.mu.Unlock()
			return b, s, true
		}
		ch := f.next
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, after, false
		case <-ch:
		}
	}
}

func (b *camBackend) servePreview(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	b.mu.Lock()
	b.viewers++
	first := b.viewers == 1
	b.mu.Unlock()
	if first {
		go b.reconcile()
		b.startWatch()
	}
	defer func() {
		b.mu.Lock()
		b.viewers--
		last := b.viewers == 0
		b.mu.Unlock()
		if last {
			time.AfterFunc(camGrace, b.reconcile)
		}
	}()
	const boundary = "zdframe"
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+boundary)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	var seq uint64
	b.frames.mu.Lock()
	if b.frames.seq > 0 {
		seq = b.frames.seq - 1 // send the last frame at once (e.g. after a capture)
	}
	b.frames.mu.Unlock()
	for {
		frame, s, ok := b.frames.wait(r.Context(), seq)
		if !ok {
			return
		}
		seq = s
		if _, err := fmt.Fprintf(w, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", boundary, len(frame)); err != nil {
			return
		}
		if _, err := w.Write(frame); err != nil {
			return
		}
		if _, err := io.WriteString(w, "\r\n"); err != nil {
			return
		}
		fl.Flush()
	}
}

// startWatch runs a 1 s supervisor while a window shows the preview: retries
// a busy or failed camera and publishes the frame rate.
func (b *camBackend) startWatch() {
	b.mu.Lock()
	if b.watching {
		b.mu.Unlock()
		return
	}
	b.watching = true
	b.mu.Unlock()
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			b.mu.Lock()
			if b.viewers == 0 && b.proc == nil {
				b.watching = false
				b.mu.Unlock()
				return
			}
			state := b.state
			live := state == "live"
			hung := state == "starting" && b.proc != nil && b.proc.frames == 0 && time.Since(b.proc.started) > 20*time.Second
			if hung {
				// no frame: let it exit and report what it printed
				_ = b.proc.cmd.Process.Signal(syscall.SIGINT)
			}
			if live && time.Since(b.fpsAt) >= 2*time.Second {
				b.fps = float64(b.nFrames) / time.Since(b.fpsAt).Seconds()
				b.nFrames, b.fpsAt = 0, time.Now()
			}
			b.mu.Unlock()
			if live {
				b.publish()
			} else if state != "starting" {
				b.reconcile()
			}
		}
	}()
}

// ---- process control ----

func (b *camBackend) reconcile() {
	b.ctl.Lock()
	defer b.ctl.Unlock()
	b.mu.Lock()
	want := b.viewers > 0 && !b.capturing
	restart := b.restart
	b.restart = false
	running := b.proc != nil
	b.mu.Unlock()
	if running && (!want || restart) {
		b.stopProcLocked()
		running = false
	}
	if !want {
		b.mu.Lock()
		capturing := b.capturing
		b.mu.Unlock()
		if !capturing {
			b.stopRecording() // the window that showed the preview is gone
			b.setState("idle", "", "")
		}
		return
	}
	if running {
		return
	}
	b.mu.Lock()
	state, retryAt, src := b.state, b.retryAt, b.source
	b.mu.Unlock()
	if state == "busy" {
		holders := camHolders(src)
		if len(holders) > 0 {
			b.setState("busy", holders[0], "")
			if time.Now().Before(retryAt) {
				return
			}
		}
	} else if (state == "error" || state == "nocamera") && time.Now().Before(retryAt) {
		return
	}
	if err := b.startProcLocked(); err != nil {
		b.setState("error", "", err.Error())
		b.scheduleRetry(false)
	}
}

func (b *camBackend) scheduleRetry(reset bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if reset || b.backoff == 0 {
		b.backoff = 2 * time.Second
	} else {
		b.backoff = min(b.backoff*2, 30*time.Second)
	}
	b.retryAt = time.Now().Add(b.backoff)
}

// camTail keeps the end of a process's stderr.
type camTail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *camTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 8192 {
		t.buf = append([]byte{}, t.buf[len(t.buf)-8192:]...)
	}
	t.mu.Unlock()
	return len(p), nil
}

func (t *camTail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// camROI is rpicam's --roi for a zoom state. ScalerCrop works in the
// unrotated sensor frame while the picture is rotated 180°, so the visible
// pan direction maps to the opposite crop movement (as in the original).
func camROI(zoom, viewX, viewY int) string {
	if zoom <= 100 {
		return ""
	}
	s := 100.0 / float64(zoom)
	maxOff := 1 - s
	x := maxOff * float64(100-max(0, min(100, viewX))) / 100
	y := maxOff * float64(100-max(0, min(100, viewY))) / 100
	return fmt.Sprintf("%.4f,%.4f,%.4f,%.4f", x, y, s, s)
}

func camPreviewArgs(roi string) []string {
	a := []string{"-t", "0", "-n", "--codec", "mjpeg", "--mode", camSensorMode,
		"--width", strconv.Itoa(camPreviewW), "--height", strconv.Itoa(camPreviewH),
		"--framerate", strconv.Itoa(camRequestFPS), "-q", "75", "--rotation", "180", "--flush",
		"--metadata", "/dev/fd/3", "--metadata-format", "txt", "-o", "-"}
	if roi != "" {
		a = append(a, "--roi", roi)
	}
	return a
}

var camSensorRe = regexp.MustCompile(`/((?:imx|ov|ar|gc)[0-9a-z]+)@`)

func (b *camBackend) startProcLocked() error {
	b.mu.Lock()
	src, roi := b.source, camROI(b.zoom, b.viewX, b.viewY)
	usbRaw := b.usbRaw
	b.mu.Unlock()
	var cmd *exec.Cmd
	var metaR *os.File
	if src == "usb" {
		devs := camUSBDevices()
		if len(devs) == 0 {
			b.setState("nocamera", "", "")
			b.scheduleRetry(true)
			return nil
		}
		cmd = exec.Command("ffmpeg", camUSBPreviewArgs(devs[0].dev, usbRaw)...)
	} else {
		if _, err := exec.LookPath("rpicam-vid"); err != nil {
			return errors.New("rpicam-vid not installed")
		}
		cmd = exec.Command("rpicam-vid", camPreviewArgs(roi)...)
		r, w, err := os.Pipe()
		if err != nil {
			return err
		}
		cmd.ExtraFiles = []*os.File{w}
		metaR = r
		defer w.Close()
	}
	p := &camProc{cmd: cmd, done: make(chan struct{}), tail: &camTail{}, usbRaw: usbRaw, started: time.Now()}
	cmd.Stderr = p.tail
	out, err := cmd.StdoutPipe()
	if err != nil {
		if metaR != nil {
			metaR.Close()
		}
		return err
	}
	if err := cmd.Start(); err != nil {
		if metaR != nil {
			metaR.Close()
		}
		return err
	}
	b.mu.Lock()
	b.proc = p
	b.nFrames, b.fpsAt = 0, time.Now()
	b.mu.Unlock()
	b.setState("starting", "", "")
	if metaR != nil {
		go b.readMeta(metaR)
	}
	go func() {
		br := bufio.NewReaderSize(out, 64<<10)
		for {
			frame, err := readJPEG(br)
			if err != nil {
				break
			}
			b.onFrame(p, frame)
		}
		_ = cmd.Wait()
		close(p.done)
		b.onExit(p)
	}()
	return nil
}

func (b *camBackend) readMeta(r *os.File) {
	defer r.Close()
	sc := bufio.NewScanner(r)
	var block []string
	for sc.Scan() {
		l := sc.Text()
		if strings.TrimSpace(l) != "" {
			block = append(block, l)
			continue
		}
		if len(block) > 0 {
			if m := parseCamMeta(block); m.Valid {
				b.mu.Lock()
				b.meta, b.metaAt = m, time.Now()
				b.mu.Unlock()
			}
			block = block[:0]
		}
	}
}

func (b *camBackend) onFrame(p *camProc, frame []byte) {
	b.mu.Lock()
	p.frames++
	b.nFrames++
	first := b.state != "live" && !b.capturing && b.proc == p
	if first {
		b.state, b.holder, b.errMsg = "live", "", ""
		b.backoff = 0
		b.nFrames, b.fpsAt = 0, time.Now() // measure from the first frame, not from start-up
		if b.sensor == "" && b.source == "csi" {
			if m := camSensorRe.FindStringSubmatch(p.tail.String()); m != nil {
				b.sensor = m[1]
			}
		}
		if w, h, ok := jpegSize(frame); ok {
			b.preview = [2]int{w, h}
		}
		log.Printf("camera: %s live after %v", b.source, time.Since(p.started).Round(10*time.Millisecond))
	}
	b.mu.Unlock()
	b.frames.put(frame)
	b.recMu.Lock()
	if b.rec != nil {
		if err := b.rec.writeFrame(frame); err != nil {
			log.Printf("camera: recording stopped: %v", err)
			_ = b.rec.close()
			b.rec = nil
			go b.hub.publish("library", map[string]string{})
		}
	}
	b.recMu.Unlock()
	if first {
		b.publish()
	}
}

// classifyCamError maps rpicam/ffmpeg stderr to a state.
func classifyCamError(stderr string) (state, msg string) {
	low := strings.ToLower(stderr)
	switch {
	case strings.Contains(low, "in use by another process"), strings.Contains(low, "failed to acquire camera"),
		strings.Contains(low, "device or resource busy"):
		return "busy", ""
	case strings.Contains(low, "no cameras available"), strings.Contains(low, "no such file or directory") && strings.Contains(low, "/dev/video"):
		return "nocamera", ""
	}
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "[") {
			continue
		}
		return "error", strings.TrimPrefix(strings.Trim(strings.TrimPrefix(l, "ERROR:"), " *"), "ERROR: ")
	}
	return "error", "camera stopped"
}

func (b *camBackend) onExit(p *camProc) {
	b.ctl.Lock()
	defer b.ctl.Unlock()
	b.mu.Lock()
	if b.proc != p {
		b.mu.Unlock()
		return
	}
	b.proc = nil
	stopped, src := p.stopped, b.source
	b.mu.Unlock()
	if stopped {
		return
	}
	b.stopRecording()
	state, msg := classifyCamError(p.tail.String())
	if src == "usb" && p.frames == 0 && !p.usbRaw && state == "error" {
		// the camera may not offer MJPEG: retry with raw frames encoded by ffmpeg
		b.mu.Lock()
		b.usbRaw = true
		b.mu.Unlock()
		b.scheduleRetry(true)
		b.mu.Lock()
		b.retryAt = time.Now()
		b.mu.Unlock()
		b.setState("starting", "", "")
		go b.reconcile()
		return
	}
	holder := ""
	if state == "busy" {
		if h := camHolders(src); len(h) > 0 {
			holder = h[0]
		}
	}
	log.Printf("camera: preview exited (%s) %s", state, msg)
	b.setState(state, holder, msg)
	b.scheduleRetry(state == "busy" || p.frames > 0)
	if state == "busy" {
		b.mu.Lock()
		b.retryAt = time.Now().Add(10 * time.Second) // while the holder is still running
		b.mu.Unlock()
	}
}

func (b *camBackend) stopProcLocked() {
	b.mu.Lock()
	p := b.proc
	if p != nil {
		p.stopped = true
	}
	b.mu.Unlock()
	if p == nil {
		return
	}
	_ = p.cmd.Process.Signal(syscall.SIGINT)
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
	b.mu.Lock()
	if b.proc == p {
		b.proc = nil
	}
	b.mu.Unlock()
}

func (b *camBackend) shutdown() {
	b.stopRecording()
	b.ctl.Lock()
	b.stopProcLocked()
	b.ctl.Unlock()
}

// ---- camera holders ----

// camHolders lists other programs that have the camera's video node open
// (libcamera keeps it open while a camera manager exists; PipeWire does so
// without using the camera, so it is ignored).
func camHolders(src string) []string {
	node := "/dev/video0"
	if src == "usb" {
		if d := camUSBDevices(); len(d) > 0 {
			node = d[0].dev
		}
	} else if n := camUnicamNode(); n != "" {
		node = n
	}
	self := os.Getpid()
	var out []string
	seen := map[string]bool{}
	procs, _ := filepath.Glob("/proc/[0-9]*")
	for _, pd := range procs {
		pid, _ := strconv.Atoi(filepath.Base(pd))
		if pid == self || camIsChild(pd, self) {
			continue
		}
		comm, _ := sysRead(pd + "/comm")
		if comm == "pipewire" || comm == "wireplumber" || comm == "pipewire-pulse" {
			continue
		}
		fds, err := os.ReadDir(pd + "/fd")
		if err != nil {
			continue
		}
		for _, fd := range fds {
			if l, err := os.Readlink(pd + "/fd/" + fd.Name()); err == nil && l == node {
				cmdline, _ := os.ReadFile(pd + "/cmdline")
				name := camHolderName(comm, string(cmdline))
				if !seen[name] {
					seen[name] = true
					out = append(out, name)
				}
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

func camIsChild(procDir string, parent int) bool {
	st, err := os.ReadFile(procDir + "/stat")
	if err != nil {
		return false
	}
	s := string(st)
	if i := strings.LastIndexByte(s, ')'); i >= 0 {
		f := strings.Fields(s[i+1:])
		if len(f) > 1 {
			pp, _ := strconv.Atoi(f[1])
			return pp == parent
		}
	}
	return false
}

func camHolderName(comm, cmdline string) string {
	c := strings.ToLower(strings.ReplaceAll(cmdline, "\x00", " "))
	switch {
	case strings.Contains(c, "camera_app"):
		return "Camera"
	case strings.Contains(c, "auracam") || strings.Contains(c, "aura-cardputer"):
		return "AuraCam"
	case strings.HasPrefix(comm, "rpicam") || strings.HasPrefix(comm, "libcamera"):
		return comm
	}
	return comm
}

func camUnicamNode() string {
	dirs, _ := filepath.Glob("/sys/class/video4linux/video*")
	for _, d := range dirs {
		if n, err := sysRead(d + "/name"); err == nil && strings.HasPrefix(n, "unicam") {
			return "/dev/" + filepath.Base(d)
		}
	}
	return ""
}

// ---- USB cameras (V4L2 UVC through ffmpeg) ----

type camUSB struct{ dev, name string }

func camUSBDevices() []camUSB {
	dirs, _ := filepath.Glob("/sys/class/video4linux/video*")
	var out []camUSB
	for _, d := range dirs {
		drv, err := filepath.EvalSymlinks(d + "/device/driver")
		if err != nil || filepath.Base(drv) != "uvcvideo" {
			continue
		}
		if idx, _ := sysRead(d + "/index"); idx != "0" {
			continue
		}
		name, _ := sysRead(d + "/name")
		out = append(out, camUSB{"/dev/" + filepath.Base(d), name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].dev < out[j].dev })
	return out
}

func camUSBPreviewArgs(dev string, raw bool) []string {
	a := []string{"-hide_banner", "-loglevel", "error", "-f", "v4l2"}
	size := fmt.Sprintf("%dx%d", camPreviewW, camPreviewH)
	if raw {
		return append(a, "-framerate", strconv.Itoa(camPreviewFPS), "-video_size", size, "-i", dev,
			"-c:v", "mjpeg", "-q:v", "6", "-f", "mjpeg", "-")
	}
	return append(a, "-input_format", "mjpeg", "-framerate", strconv.Itoa(camPreviewFPS), "-video_size", size,
		"-i", dev, "-c:v", "copy", "-f", "mjpeg", "-")
}

var camSizeRe = regexp.MustCompile(`\b(\d{3,5})x(\d{3,5})\b`)

// camUSBMaxSize parses `ffmpeg -f v4l2 -list_formats compressed` output.
func camUSBMaxSize(listing string) (int, int) {
	bw, bh := 0, 0
	for _, l := range strings.Split(listing, "\n") {
		if !strings.Contains(strings.ToLower(l), "mjpeg") {
			continue
		}
		for _, m := range camSizeRe.FindAllStringSubmatch(l, -1) {
			w, _ := strconv.Atoi(m[1])
			h, _ := strconv.Atoi(m[2])
			if w*h > bw*bh {
				bw, bh = w, h
			}
		}
	}
	return bw, bh
}

// ---- capture ----

func camFileName(prefix, ext string, t time.Time, exists func(string) bool) string {
	base := prefix + t.Format("20060102_150405")
	name := base + ext
	for i := 2; exists(name); i++ {
		name = fmt.Sprintf("%s_%d%s", base, i, ext)
	}
	return name
}

func camStillArgs(out string, res [2]int, sensor string, meta camMeta, fresh bool, roi string) []string {
	model := "CardputerZero IMX219"
	if sensor != "" {
		model = "CardputerZero " + strings.ToUpper(sensor)
	}
	a := []string{"-n", "--rotation", "180", "--width", strconv.Itoa(res[0]), "--height", strconv.Itoa(res[1]),
		"-q", "90", "--exif", "IFD0.Make=M5Stack", "--exif", "IFD0.Model=" + model, "-o", out}
	if fresh && meta.Valid {
		// lock the exposure the preview converged on: no settling frames needed
		a = append(a, "--immediate", "--shutter", strconv.Itoa(int(meta.ExposureUS+0.5)),
			"--gain", strconv.FormatFloat(meta.Gain, 'f', 3, 64))
		if meta.RedGain > 0 && meta.BlueGain > 0 {
			a = append(a, "--awbgains", strconv.FormatFloat(meta.RedGain, 'f', 4, 64)+","+strconv.FormatFloat(meta.BlueGain, 'f', 4, 64))
		}
	} else {
		a = append(a, "-t", "1200")
	}
	if roi != "" {
		a = append(a, "--roi", roi)
	}
	return a
}

func (b *camBackend) capture() (string, int, error) {
	b.recMu.Lock()
	recording := b.rec != nil
	b.recMu.Unlock()
	if recording {
		return "", http.StatusConflict, errors.New("recording")
	}
	b.mu.Lock()
	if b.capturing {
		b.mu.Unlock()
		return "", http.StatusConflict, errors.New("busy")
	}
	b.capturing = true
	src, res, sensor := b.source, b.res, b.sensor
	roi := camROI(b.zoom, b.viewX, b.viewY)
	meta, fresh := b.meta, time.Since(b.metaAt) < 2*time.Second && b.state == "live"
	b.mu.Unlock()
	b.publish()
	defer func() {
		b.mu.Lock()
		b.capturing = false
		b.mu.Unlock()
		b.publish()
		go b.reconcile()
	}()

	b.ctl.Lock()
	defer b.ctl.Unlock()
	b.stopProcLocked()

	dir := camPhotoDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", http.StatusInternalServerError, err
	}
	name := camFileName("CAM_", ".jpg", time.Now(), func(n string) bool {
		_, err := os.Stat(filepath.Join(dir, n))
		return err == nil
	})
	tmp := filepath.Join(dir, "."+name+".part")
	defer os.Remove(tmp)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if src == "usb" {
		devs := camUSBDevices()
		if len(devs) == 0 {
			return "", http.StatusServiceUnavailable, errors.New("nocamera")
		}
		list, _ := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-f", "v4l2", "-list_formats", "compressed", "-i", devs[0].dev).CombinedOutput()
		w, h := camUSBMaxSize(string(list))
		args := []string{"-hide_banner", "-loglevel", "error", "-f", "v4l2"}
		if w > 0 {
			args = append(args, "-input_format", "mjpeg", "-video_size", fmt.Sprintf("%dx%d", w, h))
		}
		// skip the first frames while exposure settles
		args = append(args, "-i", devs[0].dev, "-vf", `select=gte(n\,8)`, "-frames:v", "1", "-q:v", "2", "-f", "image2", "-c:v", "mjpeg", tmp)
		cmd = exec.CommandContext(ctx, "ffmpeg", args...)
	} else {
		cmd = exec.CommandContext(ctx, "rpicam-still", camStillArgs(tmp, res, sensor, meta, fresh, roi)...)
	}
	tail := &camTail{}
	cmd.Stderr = tail
	cmd.Stdout = io.Discard
	start := time.Now()
	if err := cmd.Run(); err != nil {
		state, msg := classifyCamError(tail.String())
		log.Printf("camera: capture failed (%s): %v %s", state, err, msg)
		if state == "busy" {
			return "", http.StatusConflict, errors.New("busy")
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", http.StatusInternalServerError, errors.New(msg)
	}
	if st, err := os.Stat(tmp); err != nil || st.Size() == 0 {
		return "", http.StatusInternalServerError, errors.New("no image")
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		return "", http.StatusInternalServerError, err
	}
	log.Printf("camera: saved %s in %v (locked exposure %v)", name, time.Since(start).Round(time.Millisecond), fresh)
	b.hub.publish("library", map[string]string{"added": name})
	return name, http.StatusOK, nil
}

// ---- video (the original's MJPEG AVI of the preview stream) ----

func (b *camBackend) startRecording() error {
	b.mu.Lock()
	live, prev := b.state == "live" && !b.capturing, b.preview
	b.mu.Unlock()
	if !live || prev[0] == 0 {
		return errors.New("not live")
	}
	b.recMu.Lock()
	defer b.recMu.Unlock()
	if b.rec != nil {
		return nil
	}
	dir := camVideoDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := camFileName("VID_", ".avi", time.Now(), func(n string) bool {
		_, err := os.Stat(filepath.Join(dir, n))
		return err == nil
	})
	a, err := newAVIWriter(filepath.Join(dir, name), prev[0], prev[1], camPreviewFPS)
	if err != nil {
		return err
	}
	b.rec, b.recSince = a, time.Now()
	go b.publish()
	return nil
}

func (b *camBackend) stopRecording() (string, error) {
	b.recMu.Lock()
	a := b.rec
	b.rec = nil
	b.recMu.Unlock()
	if a == nil {
		return "", nil
	}
	err := a.close()
	name := filepath.Base(a.path)
	if a.frames == 0 {
		_ = os.Remove(a.path)
		name = ""
	}
	b.publish()
	b.hub.publish("library", map[string]string{"added": name})
	return name, err
}

// ---- settings ----

func (b *camBackend) handleSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Resolution *[2]int `json:"resolution"`
		Source     *string `json:"source"`
		Zoom       *int    `json:"zoom"`
		ViewX      *int    `json:"view_x"`
		ViewY      *int    `json:"view_y"`
		Pan        *[2]int `json:"pan"` // steps, as the original's arrow keys
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		hwError(w, http.StatusBadRequest, err.Error())
		return
	}
	b.mu.Lock()
	restart := false
	if req.Resolution != nil {
		ok := false
		for _, v := range camResolutions {
			ok = ok || v == *req.Resolution
		}
		if !ok {
			b.mu.Unlock()
			hwError(w, http.StatusBadRequest, "unsupported resolution")
			return
		}
		if err := saveCamResolution(camConfigPath(), *req.Resolution); err != nil {
			b.mu.Unlock()
			hwError(w, http.StatusInternalServerError, err.Error())
			return
		}
		b.res = *req.Resolution
	}
	if req.Source != nil && (*req.Source == "csi" || *req.Source == "usb") && *req.Source != b.source {
		b.source, b.usbRaw, b.zoom, b.viewX, b.viewY = *req.Source, false, 100, 50, 50
		b.state, b.retryAt = "starting", time.Time{}
		restart = true
	}
	oz, ox, oy := b.zoom, b.viewX, b.viewY
	if req.Zoom != nil && b.source == "csi" {
		z := 100
		for _, l := range camZoomLevels {
			if *req.Zoom >= l {
				z = l
			}
		}
		b.zoom = z
	}
	if req.ViewX != nil {
		b.viewX = max(0, min(100, *req.ViewX))
	}
	if req.ViewY != nil {
		b.viewY = max(0, min(100, *req.ViewY))
	}
	if req.Pan != nil && b.zoom > 100 {
		b.viewX = max(0, min(100, b.viewX+req.Pan[0]*camPanStep))
		b.viewY = max(0, min(100, b.viewY+req.Pan[1]*camPanStep))
	}
	if b.zoom == 100 {
		b.viewX, b.viewY = 50, 50
	}
	if b.zoom != oz || b.viewX != ox || b.viewY != oy {
		restart = true
	}
	if restart && b.proc != nil {
		b.restart = true
	}
	b.mu.Unlock()
	if restart {
		go b.reconcile()
	}
	b.publish()
	hwJSON(w, http.StatusOK, b.snapshot())
}

// ---- library ----

type camItem struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // photo | video
	Time int64  `json:"time"` // unix ms (file time)
	Size int64  `json:"size"`
}

var camNameRe = regexp.MustCompile(`^(CAM_[^/\\]+\.[jJ][pP][gG]|VID_[^/\\]+\.[aA][vV][iI])$`)

// camMediaPath resolves a library file name (as the original lists them:
// CAM_*.jpg photos, VID_*.avi videos).
func camMediaPath(name string) (string, string, bool) {
	if !camNameRe.MatchString(name) || strings.Contains(name, "..") {
		return "", "", false
	}
	p, kind := filepath.Join(camPhotoDir(), name), "photo"
	if strings.HasPrefix(name, "VID_") {
		p, kind = filepath.Join(camVideoDir(), name), "video"
	}
	if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
		return "", "", false
	}
	return p, kind, true
}

func camLibrary() []camItem {
	items := []camItem{}
	for _, d := range []struct{ dir, kind string }{{camPhotoDir(), "photo"}, {camVideoDir(), "video"}} {
		es, _ := os.ReadDir(d.dir)
		for _, e := range es {
			if !camNameRe.MatchString(e.Name()) || !e.Type().IsRegular() {
				continue
			}
			if (d.kind == "photo") != strings.HasPrefix(e.Name(), "CAM_") {
				continue
			}
			st, err := e.Info()
			if err != nil {
				continue
			}
			items = append(items, camItem{e.Name(), d.kind, st.ModTime().UnixMilli(), st.Size()})
		}
	}
	// names carry the capture time (CAM_/VID_YYYYMMDD_HHMMSS), newest first
	sort.SliceStable(items, func(i, j int) bool {
		a, c := items[i].Name[4:], items[j].Name[4:]
		if a != c {
			return a > c
		}
		return items[i].Time > items[j].Time
	})
	return items
}

type camInfo struct {
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Size        int64   `json:"size"`
	Time        int64   `json:"time"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Make        string  `json:"make,omitempty"`
	Model       string  `json:"model,omitempty"`
	Taken       string  `json:"taken,omitempty"`
	ExposureUS  float64 `json:"exposure_us,omitempty"`
	FNumber     float64 `json:"f_number,omitempty"`
	FocalLength float64 `json:"focal_length,omitempty"`
	ISO         int     `json:"iso,omitempty"`
	Frames      int     `json:"frames,omitempty"`
	FPS         int     `json:"fps,omitempty"`
}

func camMediaInfo(p, kind string) (camInfo, error) {
	st, err := os.Stat(p)
	if err != nil {
		return camInfo{}, err
	}
	in := camInfo{Name: filepath.Base(p), Kind: kind, Size: st.Size(), Time: st.ModTime().UnixMilli()}
	if kind == "video" {
		f, err := os.Open(p)
		if err != nil {
			return in, err
		}
		defer f.Close()
		a, err := readAVI(f, st.Size())
		if err != nil {
			return in, nil
		}
		in.Width, in.Height, in.FPS, in.Frames = a.W, a.H, a.FPS, len(a.Frames)
		return in, nil
	}
	head, err := readJPEGHead(p)
	if err != nil {
		return in, err
	}
	if w, h, ok := jpegSize(head); ok {
		in.Width, in.Height = w, h
	}
	if x, ok := parseExif(jpegExif(head)); ok {
		in.Make, in.Model, in.Taken = x.Make, x.Model, x.DateTime
		in.ExposureUS, in.FNumber, in.FocalLength, in.ISO = x.ExposureUS, x.FNumber, x.FocalLength, x.ISO
		if in.Width == 0 {
			in.Width, in.Height = x.Width, x.Height
		}
	}
	return in, nil
}

func (b *camBackend) serveMedia(w http.ResponseWriter, r *http.Request) {
	p, kind, ok := camMediaPath(r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	if r.URL.Query().Get("download") != "" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(p)+`"`)
	}
	if kind == "video" {
		w.Header().Set("Content-Type", "video/x-msvideo")
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, filepath.Base(p), st.ModTime(), f)
}

func (b *camBackend) serveThumb(w http.ResponseWriter, r *http.Request) {
	p, kind, ok := camMediaPath(r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	var data []byte
	if kind == "video" {
		f, err := os.Open(p)
		if err == nil {
			st, _ := f.Stat()
			if a, err := readAVI(f, st.Size()); err == nil && len(a.Frames) > 0 && a.Frames[0].Size < camMaxJPEG {
				data = make([]byte, a.Frames[0].Size)
				if _, err := f.ReadAt(data, a.Frames[0].Off); err != nil {
					data = nil
				}
			}
			f.Close()
		}
	} else {
		data = b.photoThumb(p)
	}
	if data == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(data)
}

// photoThumb returns the EXIF thumbnail (rpicam-still embeds one) or a cached
// reduced copy made by ffmpeg, decoding at 1/2^n scale to keep memory low
// (photos by the original app have no embedded thumbnail).
func (b *camBackend) photoThumb(p string) []byte {
	head, err := readJPEGHead(p)
	if err != nil {
		return nil
	}
	if x, ok := parseExif(jpegExif(head)); ok && len(x.Thumb) > 0 {
		return x.Thumb
	}
	st, err := os.Stat(p)
	if err != nil {
		return nil
	}
	cache := filepath.Join(camThumbDir(), filepath.Base(p)+".jpg")
	if cst, err := os.Stat(cache); err == nil && !cst.ModTime().Before(st.ModTime()) {
		if d, err := os.ReadFile(cache); err == nil {
			return d
		}
	}
	b.thumbMu.Lock() // one decoder at a time
	defer b.thumbMu.Unlock()
	lowres := 0
	if w, _, ok := jpegSize(head); ok {
		for lowres < 3 && w>>(lowres+1) >= 320 {
			lowres++
		}
	}
	if err := os.MkdirAll(camThumbDir(), 0o755); err != nil {
		return nil
	}
	tmp := cache + ".part.jpg"
	defer os.Remove(tmp)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-lowres", strconv.Itoa(lowres),
		"-i", p, "-vf", "scale=400:-2", "-frames:v", "1", "-q:v", "5", "-y", tmp).CombinedOutput()
	if err != nil {
		log.Printf("camera: thumbnail %s: %v %s", filepath.Base(p), err, strings.TrimSpace(string(out)))
		return nil
	}
	if err := os.Rename(tmp, cache); err != nil {
		return nil
	}
	d, _ := os.ReadFile(cache)
	return d
}

// servePlay streams a recorded AVI as MJPEG at its frame rate.
func (b *camBackend) servePlay(w http.ResponseWriter, r *http.Request) {
	p, kind, ok := camMediaPath(r.PathValue("name"))
	if !ok || kind != "video" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	a, err := readAVI(f, st.Size())
	if err != nil || len(a.Frames) == 0 {
		http.Error(w, "unreadable video", http.StatusUnprocessableEntity)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	fps := a.FPS
	if fps <= 0 || fps > 60 {
		fps = camPreviewFPS
	}
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=zdframe")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	t := time.NewTicker(time.Second / time.Duration(fps))
	defer t.Stop()
	buf := make([]byte, 0, 64<<10)
	for _, fr := range a.Frames {
		if fr.Size <= 0 || fr.Size > camMaxJPEG {
			continue
		}
		buf = buf[:fr.Size]
		if int64(cap(buf)) < fr.Size {
			buf = make([]byte, fr.Size)
		}
		if _, err := f.ReadAt(buf, fr.Off); err != nil {
			return
		}
		fmt.Fprintf(w, "--zdframe\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(buf))
		if _, err := w.Write(buf); err != nil {
			return
		}
		_, _ = io.WriteString(w, "\r\n")
		fl.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
		}
	}
}
