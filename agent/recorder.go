package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// recorder captures the Zero's microphone into ~/Recordings, the folder the
// official Recorder app uses, so recordings show up in both places.
// arecord streams raw PCM to us; we write the WAV ourselves so we can report
// a live level for the web UI's waveform.

const (
	recRate     = 44100
	recChannels = 1
	recBits     = 16
)

type recorder struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	done    chan struct{} // closed when the current arecord has exited
	file    *os.File
	path    string
	started time.Time
	bytes   int64
	levels  []float32 // recent levels, newest last, 0..1
	paused  bool      // while paused the microphone keeps running but nothing is kept
	err     string
}

var rec = &recorder{}

func recordingsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Recordings")
}

// captureDevice picks the first sound card that can record. The system's
// default ALSA device on the Zero is playback-only, so "default" fails.
func captureDevice() string {
	b, err := os.ReadFile("/proc/asound/pcm")
	if err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			// "01-00: (null)-ES8389 HiFi ... : playback 1 : capture 1"
			if !strings.Contains(line, "capture") || len(line) < 5 {
				continue
			}
			card, dev := strings.TrimLeft(line[0:2], "0"), strings.TrimLeft(line[3:5], "0")
			if card == "" {
				card = "0"
			}
			if dev == "" {
				dev = "0"
			}
			return "plughw:" + card + "," + dev
		}
	}
	return "default"
}

func wavHeader(dataLen uint32) []byte {
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], 36+dataLen)
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], recChannels)
	binary.LittleEndian.PutUint32(h[24:], recRate)
	binary.LittleEndian.PutUint32(h[28:], recRate*recChannels*recBits/8)
	binary.LittleEndian.PutUint16(h[32:], recChannels*recBits/8)
	binary.LittleEndian.PutUint16(h[34:], recBits)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], dataLen)
	return h
}

func (r *recorder) start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil {
		return errors.New("already recording")
	}
	dir := recordingsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := time.Now().Format("2006-01-02 15.04.05") + ".wav"
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(wavHeader(0)); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	cmd := exec.Command("arecord", "-q", "-D", captureDevice(), "-t", "raw", "-f", "S16_LE",
		"-r", fmt.Sprint(recRate), "-c", fmt.Sprint(recChannels))
	out, err := cmd.StdoutPipe()
	if err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := cmd.Start(); err != nil {
		f.Close()
		os.Remove(path)
		return fmt.Errorf("arecord: %w", err)
	}
	done := make(chan struct{})
	r.cmd, r.done, r.file, r.path, r.started, r.bytes, r.levels, r.err, r.paused = cmd, done, f, path, time.Now(), 0, nil, "", false
	go r.pump(cmd, out, f, done)
	log.Printf("recorder: started %s", path)
	return nil
}

// pump copies PCM into the file and keeps ~20 levels per second.
func (r *recorder) pump(cmd *exec.Cmd, out io.Reader, f *os.File, done chan struct{}) {
	buf := make([]byte, recRate*2/20) // 50 ms
	for {
		n, err := io.ReadFull(out, buf)
		if n > 0 {
			chunk := buf[:n-n%2]
			var sum float64
			for i := 0; i+1 < len(chunk); i += 2 {
				v := float64(int16(binary.LittleEndian.Uint16(chunk[i:]))) / 32768
				sum += v * v
			}
			rms := math.Sqrt(sum / float64(len(chunk)/2+1))
			// perceptual-ish scale: -50 dB..0 dB -> 0..1
			lvl := 0.0
			if rms > 0 {
				lvl = (20*math.Log10(rms) + 50) / 50
			}
			lvl = math.Max(0, math.Min(1, lvl))
			r.mu.Lock()
			paused := r.paused
			r.mu.Unlock()
			if paused {
				if err != nil {
					break
				}
				continue
			}
			if _, werr := f.Write(chunk); werr != nil {
				err = werr
			}
			r.mu.Lock()
			r.bytes += int64(len(chunk))
			r.levels = append(r.levels, float32(lvl))
			if len(r.levels) > 400 {
				r.levels = r.levels[len(r.levels)-400:]
			}
			r.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	_ = cmd.Wait()
	close(done)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == cmd { // ended on its own (device busy, unplugged…)
		r.finishLocked()
		if r.bytes == 0 {
			r.err = "microphone unavailable"
		}
	}
}

// finishLocked fixes up the WAV header and closes the file. Empty recordings are removed.
func (r *recorder) finishLocked() {
	if r.file == nil {
		return
	}
	if r.bytes > 0 {
		if _, err := r.file.WriteAt(wavHeader(uint32(r.bytes)), 0); err != nil {
			log.Printf("recorder: header: %v", err)
		}
		r.file.Close()
		log.Printf("recorder: saved %s (%d bytes)", r.path, r.bytes)
	} else {
		r.file.Close()
		os.Remove(r.path)
	}
	r.cmd, r.file = nil, nil
}

func (r *recorder) stop() (string, error) {
	r.mu.Lock()
	cmd, done := r.cmd, r.done
	r.mu.Unlock()
	if cmd == nil {
		return "", errors.New("not recording")
	}
	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	path := r.path
	if r.cmd == cmd {
		r.finishLocked()
	}
	return path, nil
}

func (r *recorder) setPaused(p bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == nil {
		return errors.New("not recording")
	}
	r.paused = p
	return nil
}

func handleRecPause(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Paused bool `json:"paused"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1024)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if err := rec.setPaused(body.Paused); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"paused": body.Paused})
}

func handleRecStart(w http.ResponseWriter, _ *http.Request) {
	if err := rec.start(); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func handleRecStop(w http.ResponseWriter, _ *http.Request) {
	path, err := rec.stop()
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

// handleRecStatus returns the levels recorded since `since` (a count the client keeps).
func handleRecStatus(w http.ResponseWriter, r *http.Request) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	res := map[string]any{"recording": rec.cmd != nil, "paused": rec.paused, "dir": recordingsDir()}
	if rec.cmd != nil {
		res["elapsed"] = float64(rec.bytes) / float64(recRate*recChannels*recBits/8)
		res["path"] = rec.path
		res["levels"] = rec.levels
	}
	if rec.err != "" {
		res["error"] = rec.err
	}
	writeJSON(w, http.StatusOK, res)
}
