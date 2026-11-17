package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sync"
	"time"
)

// Waveform peaks for a WAV file, so the browser can draw a long recording
// straight away and stream the audio instead of downloading and decoding the
// whole file first.

// peaks of finished files, keyed by path, size and mtime
var peaksCache sync.Map

const (
	peaksPerSecond = 50
	peaksMax       = 20000
)

type wavInfo struct {
	channels   int
	rate       int
	bits       int
	dataOffset int64
	dataSize   int64
}

func readWavInfo(f *os.File) (wavInfo, error) {
	var w wavInfo
	var hdr [12]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return w, err
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return w, errors.New("not a wav file")
	}
	off := int64(12)
	st, err := f.Stat()
	if err != nil {
		return w, err
	}
	for {
		var ch [8]byte
		if _, err := f.ReadAt(ch[:], off); err != nil {
			return w, errors.New("no data chunk")
		}
		id := string(ch[0:4])
		size := int64(binary.LittleEndian.Uint32(ch[4:8]))
		body := off + 8
		switch id {
		case "fmt ":
			var fm [16]byte
			if _, err := f.ReadAt(fm[:], body); err != nil {
				return w, err
			}
			if format := binary.LittleEndian.Uint16(fm[0:2]); format != 1 && format != 0xFFFE {
				return w, errors.New("only PCM wav is supported")
			}
			w.channels = int(binary.LittleEndian.Uint16(fm[2:4]))
			w.rate = int(binary.LittleEndian.Uint32(fm[4:8]))
			w.bits = int(binary.LittleEndian.Uint16(fm[14:16]))
		case "data":
			w.dataOffset = body
			// a file still being written has a placeholder size
			if size == 0 || body+size > st.Size() {
				size = st.Size() - body
			}
			w.dataSize = size
			if w.channels == 0 || w.rate == 0 || w.bits != 16 {
				return w, errors.New("only 16-bit PCM wav is supported")
			}
			return w, nil
		}
		off = body + size + size%2
	}
}

func handleAudioPeaks(w http.ResponseWriter, r *http.Request) {
	p, err := cleanPath(r.URL.Query().Get("path"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	f, err := os.Open(p)
	if err != nil {
		fsError(w, err)
		return
	}
	defer f.Close()
	info, err := readWavInfo(f)
	if err != nil {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": err.Error()})
		return
	}

	st, _ := f.Stat()
	key := fmt.Sprintf("%s|%d|%d", p, st.Size(), st.ModTime().UnixNano())
	if v, ok := peaksCache.Load(key); ok {
		writeJSON(w, http.StatusOK, v)
		return
	}

	frameBytes := int64(info.channels * 2)
	frames := info.dataSize / frameBytes
	duration := float64(frames) / float64(info.rate)
	n := int(math.Ceil(duration * peaksPerSecond))
	if n > peaksMax {
		n = peaksMax
	}
	if n < 1 {
		n = 1
	}
	perBucket := (frames + int64(n) - 1) / int64(n)
	if perBucket < 1 {
		perBucket = 1
	}

	if _, err := f.Seek(info.dataOffset, io.SeekStart); err != nil {
		fsError(w, err)
		return
	}
	src := io.LimitReader(f, frames*frameBytes)
	peaks := make([]float64, 0, n)
	block := make([]byte, (1<<16)/int(frameBytes)*int(frameBytes))
	var max int
	var inBucket int64
	ch := info.channels
	for {
		got, err := io.ReadFull(src, block)
		got -= got % int(frameBytes)
		for i := 0; i < got; i += 2 {
			v := int(int16(uint16(block[i]) | uint16(block[i+1])<<8))
			if v < 0 {
				v = -v
			}
			if v > max {
				max = v
			}
			if (i/2+1)%ch == 0 {
				inBucket++
				if inBucket == perBucket {
					peaks = append(peaks, math.Round(float64(max)/32768*1000)/1000)
					max, inBucket = 0, 0
				}
			}
		}
		if err != nil {
			break
		}
	}
	if inBucket > 0 {
		peaks = append(peaks, math.Round(float64(max)/32768*1000)/1000)
	}
	res := map[string]any{"duration": duration, "peaks": peaks}
	// a file still being written changes every request; don't keep those
	if time.Since(st.ModTime()) > 5*time.Second {
		peaksCache.Store(key, res)
	}
	writeJSON(w, http.StatusOK, res)
}
