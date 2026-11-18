package main

import (
	"errors"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// GPS: the Cap LoRa-1262's ATGM336H receiver, ported from
// CardputerZero/Cap-LoRa-1262-GPS (MIT; it uses minmea, MIT): NMEA 0183 on
// /dev/ttyS0 at 115200 8N1, read-only and exclusive (flock + TIOCEXCL),
// Cap power through ext_5v_out, the same RMC/GGA/GLL/GSA/GSV/VTG/ZDA
// handling and fix/stale rules (src/gps/nmea_decoder.cpp,
// src/models/gps_model.cpp). Per-satellite data from GSV/GSA is kept as
// well for the sky plot and signal bars.

func init() { hwApps["cap-lora-1262-gps"] = hwLoRaGPS }

const (
	gpsDevice       = "/dev/ttyS0"
	gpsBaud         = 115200
	gpsStaleAfter   = 3 * time.Second
	gpsSatExpire    = 5 * time.Second
	gpsMaxSentence  = 160
	gpsPublishEvery = 250 * time.Millisecond
	knotsToKph      = 1.852
)

// ---- NMEA ----

// nmeaFields validates a sentence ("$GPRMC,...*hh", no line ending) and
// returns talker, type and fields. ok is false for a bad checksum or shape
// (minmea_sentence_id with strict checksums).
func nmeaFields(line string) (talker, kind string, f []string, ok bool) {
	if len(line) < 6 || line[0] != '$' {
		return "", "", nil, false
	}
	star := strings.LastIndexByte(line, '*')
	if star < 0 || star+3 != len(line) {
		return "", "", nil, false
	}
	want, err := strconv.ParseUint(line[star+1:], 16, 8)
	if err != nil {
		return "", "", nil, false
	}
	var sum byte
	for i := 1; i < star; i++ {
		c := line[i]
		if c < 0x20 || c > 0x7e {
			return "", "", nil, false
		}
		sum ^= c
	}
	if sum != byte(want) {
		return "", "", nil, false
	}
	parts := strings.Split(line[1:star], ",")
	id := parts[0]
	if len(id) != 5 {
		return "", "", nil, true // valid but unknown (proprietary etc.)
	}
	return id[:2], id[2:], parts[1:], true
}

func nmeaField(f []string, i int) string {
	if i < len(f) {
		return strings.TrimSpace(f[i])
	}
	return ""
}

func nmeaFloat(f []string, i int) (float64, bool) {
	s := nmeaField(f, i)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func nmeaInt(f []string, i int) (int, bool) {
	s := nmeaField(f, i)
	if s == "" {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	return v, err == nil
}

// nmeaCoord parses ddmm.mmmm + hemisphere into signed degrees.
func nmeaCoord(f []string, i int) (float64, bool) {
	raw, ok := nmeaFloat(f, i)
	if !ok {
		return 0, false
	}
	deg := math.Trunc(raw / 100)
	v := deg + (raw-deg*100)/60
	switch nmeaField(f, i+1) {
	case "S", "W":
		v = -v
	case "N", "E":
	default:
		return 0, false
	}
	return v, true
}

type gpsUTC struct {
	DateValid, TimeValid           bool
	Year, Month, Day               int
	Hour, Minute, Second, Millisec int
}

func (u *gpsUTC) setTime(s string) {
	if len(s) < 6 {
		return
	}
	h, e1 := strconv.Atoi(s[0:2])
	m, e2 := strconv.Atoi(s[2:4])
	sec, e3 := strconv.ParseFloat(s[4:], 64)
	if e1 != nil || e2 != nil || e3 != nil || h > 23 || m > 59 || sec < 0 || sec >= 61 {
		return
	}
	u.TimeValid, u.Hour, u.Minute = true, h, m
	u.Second, u.Millisec = int(sec), int(math.Round((sec-math.Trunc(sec))*1000))%1000
}

func (u *gpsUTC) setDate(day, month, year int) {
	if year < 0 || month < 1 || month > 12 || day < 1 || day > 31 {
		return
	}
	if year < 100 {
		year += 2000
	}
	u.DateValid, u.Year, u.Month, u.Day = true, year, month, day
}

// satellite systems (NMEA 4.1 system ids)
const (
	gnssGPS     = 1
	gnssGLONASS = 2
	gnssGalileo = 3
	gnssBeiDou  = 4
	gnssQZSS    = 5
	gnssNavIC   = 6
)

func gnssFromTalker(t string) int {
	switch t {
	case "GP":
		return gnssGPS
	case "GL":
		return gnssGLONASS
	case "GA":
		return gnssGalileo
	case "BD", "GB":
		return gnssBeiDou
	case "GQ", "QZ":
		return gnssQZSS
	case "GI":
		return gnssNavIC
	}
	return 0
}

// gnssFromPRN guesses the system for combined ("GN") sentences without a
// system id (NMEA 4.0 numbering).
func gnssFromPRN(prn int) int {
	switch {
	case prn >= 1 && prn <= 32:
		return gnssGPS
	case prn >= 65 && prn <= 96:
		return gnssGLONASS
	case prn >= 193 && prn <= 200:
		return gnssQZSS
	case prn >= 201 && prn <= 263:
		return gnssBeiDou
	case prn >= 301 && prn <= 336:
		return gnssGalileo
	}
	return gnssGPS
}

type gpsSat struct {
	System int  `json:"system"`
	PRN    int  `json:"prn"`
	Elev   *int `json:"elevation"`
	Az     *int `json:"azimuth"`
	SNR    *int `json:"snr"`
	Used   bool `json:"used"`
}

type gpsSatGroup struct {
	sats []gpsSat
	at   time.Time
}

type gpsUsedGroup struct {
	prns map[int]bool
	at   time.Time
}

// nmeaDecoder is the original NmeaDecoder plus satellite tracking.
type nmeaDecoder struct {
	line    []byte
	discard bool

	PositionValid, FixValid bool
	Lat, Lon                float64
	AltValid                bool
	Alt                     float64
	SpeedValid              bool
	SpeedKph                float64
	CourseValid             bool
	Course                  float64
	HDOPValid, PDOPValid    bool
	VDOPValid               bool
	HDOP, PDOP, VDOP        float64
	FixQuality              int
	FixType                 int
	SatsUsed                int
	UTC                     gpsUTC
	Talker, LastSentence    string
	Bytes, Sentences        uint64
	Invalid                 uint64

	hasSentence, hasFix bool
	lastSentence        time.Time
	lastFix             time.Time

	gsvPending map[string][]gpsSat     // talker/signal -> partial cycle
	gsv        map[string]*gpsSatGroup // talker/signal -> last full cycle
	used       map[int]*gpsUsedGroup   // system -> PRNs used in the solution
}

func newNmeaDecoder() *nmeaDecoder {
	return &nmeaDecoder{FixType: 1, gsvPending: map[string][]gpsSat{}, gsv: map[string]*gpsSatGroup{}, used: map[int]*gpsUsedGroup{}}
}

// feed consumes raw UART bytes; it returns true when something changed.
func (d *nmeaDecoder) feed(b []byte, now time.Time) bool {
	d.Bytes += uint64(len(b))
	changed := false
	for _, c := range b {
		switch {
		case c == '\r' || c == '\n':
			if len(d.line) > 0 || d.discard {
				changed = d.finish(now) || changed
			}
			d.line, d.discard = d.line[:0], false
		case c == '$':
			d.line, d.discard = append(d.line[:0], c), false
		case d.discard || len(d.line) == 0:
		case len(d.line) >= gpsMaxSentence:
			d.line, d.discard = d.line[:0], true
		default:
			d.line = append(d.line, c)
		}
	}
	return changed
}

func (d *nmeaDecoder) setFix(fixed bool, now time.Time) {
	d.FixValid = fixed
	if fixed {
		d.hasFix, d.lastFix = true, now
	}
}

func (d *nmeaDecoder) finish(now time.Time) bool {
	if d.discard || len(d.line) == 0 {
		d.Invalid++
		return true
	}
	talker, kind, f, ok := nmeaFields(string(d.line))
	if !ok {
		d.Invalid++
		return true
	}
	switch kind {
	case "RMC", "GGA", "GLL", "GSA", "GSV", "VTG", "ZDA", "GBS", "GST":
	default:
		return false // unknown sentences are ignored, as minmea does
	}
	d.Sentences++
	d.hasSentence, d.lastSentence = true, now
	d.Talker, d.LastSentence = talker, kind
	bad := func() bool { d.Invalid++; return true }
	switch kind {
	case "RMC":
		if len(f) < 9 {
			return bad()
		}
		d.UTC.setTime(nmeaField(f, 0))
		if ds := nmeaField(f, 8); len(ds) == 6 {
			dd, _ := strconv.Atoi(ds[0:2])
			mm, _ := strconv.Atoi(ds[2:4])
			yy, _ := strconv.Atoi(ds[4:6])
			d.UTC.setDate(dd, mm, yy)
		}
		fixed := false
		if nmeaField(f, 1) == "A" {
			lat, ok1 := nmeaCoord(f, 2)
			lon, ok2 := nmeaCoord(f, 4)
			if ok1 && ok2 && math.Abs(lat) <= 90 && math.Abs(lon) <= 180 {
				d.Lat, d.Lon, d.PositionValid, fixed = lat, lon, true, true
			}
		}
		d.setFix(fixed, now)
		if v, ok := nmeaFloat(f, 6); ok && v >= 0 {
			d.SpeedKph, d.SpeedValid = v*knotsToKph, true
		}
		if v, ok := nmeaFloat(f, 7); ok && v >= 0 {
			d.Course, d.CourseValid = v, true
		}
	case "GGA":
		if len(f) < 9 {
			return bad()
		}
		q, _ := nmeaInt(f, 5)
		n, _ := nmeaInt(f, 6)
		d.FixQuality, d.SatsUsed = q, max(0, n)
		d.UTC.setTime(nmeaField(f, 0))
		fixed := false
		if q > 0 {
			lat, ok1 := nmeaCoord(f, 1)
			lon, ok2 := nmeaCoord(f, 3)
			if ok1 && ok2 && math.Abs(lat) <= 90 && math.Abs(lon) <= 180 {
				d.Lat, d.Lon, d.PositionValid, fixed = lat, lon, true, true
			}
			if v, ok := nmeaFloat(f, 8); ok {
				d.Alt, d.AltValid = v, true
			}
		}
		d.setFix(fixed, now)
		if v, ok := nmeaFloat(f, 7); ok && v >= 0 {
			d.HDOP, d.HDOPValid = v, true
		}
	case "GLL":
		if len(f) < 6 {
			return bad()
		}
		d.UTC.setTime(nmeaField(f, 4))
		fixed := false
		if nmeaField(f, 5) == "A" {
			lat, ok1 := nmeaCoord(f, 0)
			lon, ok2 := nmeaCoord(f, 2)
			if ok1 && ok2 && math.Abs(lat) <= 90 && math.Abs(lon) <= 180 {
				d.Lat, d.Lon, d.PositionValid, fixed = lat, lon, true, true
			}
		}
		d.setFix(fixed, now)
	case "GSA":
		if len(f) < 17 {
			return bad()
		}
		ft, _ := nmeaInt(f, 1)
		d.FixType = ft
		if ft <= 1 {
			d.FixValid = false
		}
		if v, ok := nmeaFloat(f, 14); ok && v >= 0 {
			d.PDOP, d.PDOPValid = v, true
		}
		if v, ok := nmeaFloat(f, 15); ok && v >= 0 {
			d.HDOP, d.HDOPValid = v, true
		}
		if v, ok := nmeaFloat(f, 16); ok && v >= 0 {
			d.VDOP, d.VDOPValid = v, true
		}
		sys, _ := nmeaInt(f, 17)
		if sys == 0 {
			sys = gnssFromTalker(talker)
		}
		groups := map[int]map[int]bool{}
		for i := 2; i < 14; i++ {
			prn, ok := nmeaInt(f, i)
			if !ok || prn <= 0 {
				continue
			}
			s := sys
			if s == 0 {
				s = gnssFromPRN(prn)
			}
			if groups[s] == nil {
				groups[s] = map[int]bool{}
			}
			groups[s][prn] = true
		}
		if sys != 0 && groups[sys] == nil {
			groups[sys] = map[int]bool{} // an empty list clears that system
		}
		for s, g := range groups {
			d.used[s] = &gpsUsedGroup{prns: g, at: now}
		}
	case "GSV":
		total, ok1 := nmeaInt(f, 0)
		num, ok2 := nmeaInt(f, 1)
		if !ok1 || !ok2 || num < 1 || num > total || len(f) < 3 {
			return bad()
		}
		sats := (len(f) - 3) / 4
		signal := ""
		if (len(f)-3)%4 == 1 { // NMEA 4.1 signal id
			signal = nmeaField(f, len(f)-1)
		}
		key := talker + "/" + signal
		if num == 1 {
			d.gsvPending[key] = d.gsvPending[key][:0]
		}
		sys := gnssFromTalker(talker)
		for i := range sats {
			base := 3 + i*4
			prn, ok := nmeaInt(f, base)
			if !ok || prn <= 0 {
				continue
			}
			s := gpsSat{System: sys, PRN: prn}
			if s.System == 0 {
				s.System = gnssFromPRN(prn)
			}
			if v, ok := nmeaInt(f, base+1); ok && v >= -90 && v <= 90 {
				s.Elev = &v
			}
			if v, ok := nmeaInt(f, base+2); ok && v >= 0 && v < 360 {
				s.Az = &v
			}
			if v, ok := nmeaInt(f, base+3); ok && v >= 0 {
				s.SNR = &v
			}
			d.gsvPending[key] = append(d.gsvPending[key], s)
		}
		if num == total {
			d.gsv[key] = &gpsSatGroup{sats: append([]gpsSat(nil), d.gsvPending[key]...), at: now}
			delete(d.gsvPending, key)
		}
	case "VTG":
		// new layout: course,T,mag,M,knots,N,kph,K ; old: course,mag,knots,kph
		course, kph := 0, 6
		if nmeaField(f, 1) != "T" && nmeaField(f, 1) != "" && len(f) < 8 {
			kph = 3
		}
		if v, ok := nmeaFloat(f, kph); ok && v >= 0 {
			d.SpeedKph, d.SpeedValid = v, true
		}
		if v, ok := nmeaFloat(f, course); ok && v >= 0 {
			d.Course, d.CourseValid = v, true
		}
	case "ZDA":
		d.UTC.setTime(nmeaField(f, 0))
		dd, ok1 := nmeaInt(f, 1)
		mm, ok2 := nmeaInt(f, 2)
		yy, ok3 := nmeaInt(f, 3)
		if ok1 && ok2 && ok3 {
			d.UTC.setDate(dd, mm, yy)
		}
	}
	return true
}

// satellites merges the latest GSV cycles (one per system/signal, the best
// SNR wins for a satellite heard on several signals) with the GSA "used"
// lists, dropping data older than gpsSatExpire.
func (d *nmeaDecoder) satellites(now time.Time) []gpsSat {
	type key struct{ sys, prn int }
	m := map[key]gpsSat{}
	for k, g := range d.gsv {
		if now.Sub(g.at) > gpsSatExpire {
			delete(d.gsv, k)
			continue
		}
		for _, s := range g.sats {
			k := key{s.System, s.PRN}
			if old, ok := m[k]; ok && old.SNR != nil && (s.SNR == nil || *s.SNR < *old.SNR) {
				continue
			}
			m[k] = s
		}
	}
	for sys, g := range d.used {
		if now.Sub(g.at) > gpsSatExpire {
			delete(d.used, sys)
		}
	}
	out := make([]gpsSat, 0, len(m))
	for _, s := range m {
		if g := d.used[s.System]; g != nil && g.prns[s.PRN] {
			s.Used = true
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].System != out[j].System {
			return out[i].System < out[j].System
		}
		return out[i].PRN < out[j].PRN
	})
	return out
}

// ---- state ----

type gpsState struct {
	State       string   `json:"state"` // starting, nodata, searching, fixed, stale, error, stopped
	Error       string   `json:"error,omitempty"`
	Diagnostics string   `json:"diagnostics"`
	Attempt     int      `json:"attempt"`
	FixType     int      `json:"fix_type"`
	FixQuality  int      `json:"fix_quality"`
	Lat         *float64 `json:"lat"`
	Lon         *float64 `json:"lon"`
	Alt         *float64 `json:"altitude"`
	Speed       *float64 `json:"speed_kph"`
	Course      *float64 `json:"course"`
	HDOP        *float64 `json:"hdop"`
	PDOP        *float64 `json:"pdop"`
	VDOP        *float64 `json:"vdop"`
	SatsUsed    int      `json:"sats_used"`
	SatsVisible int      `json:"sats_visible"`
	Date        string   `json:"date,omitempty"` // YYYY-MM-DD
	Time        string   `json:"time,omitempty"` // hh:mm:ss
	Satellites  []gpsSat `json:"satellites"`
	Device      string   `json:"device"`
	Baud        int      `json:"baud"`
	Module      string   `json:"module"`
	Protocol    string   `json:"protocol"`
	Talker      string   `json:"talker,omitempty"`
	Last        string   `json:"last_sentence,omitempty"`
	Sentences   uint64   `json:"sentences"`
	Invalid     uint64   `json:"invalid"`
	Bytes       uint64   `json:"bytes"`
	DataAgeMs   *int64   `json:"data_age_ms"`
	FixAgeMs    *int64   `json:"fix_age_ms"`
}

func fptr(ok bool, v float64) *float64 {
	if !ok {
		return nil
	}
	return &v
}

// gpsStatus derives the original's GpsState from the decoder.
func gpsStatus(d *nmeaDecoder, opened, now time.Time) gpsState {
	s := gpsState{
		FixType: d.FixType, FixQuality: d.FixQuality, SatsUsed: d.SatsUsed,
		Lat: fptr(d.PositionValid, d.Lat), Lon: fptr(d.PositionValid, d.Lon),
		Alt: fptr(d.AltValid, d.Alt), Speed: fptr(d.SpeedValid, d.SpeedKph), Course: fptr(d.CourseValid, d.Course),
		HDOP: fptr(d.HDOPValid, d.HDOP), PDOP: fptr(d.PDOPValid, d.PDOP), VDOP: fptr(d.VDOPValid, d.VDOP),
		Talker: d.Talker, Last: d.LastSentence, Sentences: d.Sentences, Invalid: d.Invalid, Bytes: d.Bytes,
	}
	if d.UTC.DateValid {
		s.Date = time.Date(d.UTC.Year, time.Month(d.UTC.Month), d.UTC.Day, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	}
	if d.UTC.TimeValid {
		s.Time = time.Date(2000, 1, 1, d.UTC.Hour, d.UTC.Minute, d.UTC.Second, 0, time.UTC).Format("15:04:05")
	}
	s.Satellites = d.satellites(now)
	s.SatsVisible = len(s.Satellites)
	var dataAge int64
	if d.hasSentence {
		dataAge = now.Sub(d.lastSentence).Milliseconds()
	} else {
		dataAge = now.Sub(opened).Milliseconds()
	}
	s.DataAgeMs = &dataAge
	if d.hasFix {
		a := now.Sub(d.lastFix).Milliseconds()
		s.FixAgeMs = &a
	}
	switch {
	case !d.hasSentence:
		s.State = "nodata"
		if d.Bytes == 0 {
			s.Diagnostics = "No GPS data received"
		} else {
			s.Diagnostics = "Waiting for valid NMEA data"
		}
	case dataAge > gpsStaleAfter.Milliseconds():
		s.State, s.Diagnostics = "stale", "GPS data is stale"
	case d.FixValid && s.FixAgeMs != nil && *s.FixAgeMs <= gpsStaleAfter.Milliseconds():
		s.State, s.Diagnostics = "fixed", "Position fix"
		if d.FixType >= 3 {
			s.Diagnostics = "3D position fix"
		}
	default:
		s.State, s.Diagnostics = "searching", "NMEA active; waiting for position fix"
	}
	return s
}

// ---- the backend ----

type gpsBackend struct {
	mu      sync.Mutex
	hub     *hwHub
	state   gpsState
	attempt int
	running bool
	stop    chan struct{}
	done    chan struct{}
	retry   chan struct{}
	idle    *time.Timer
}

func hwLoRaGPS(mux *http.ServeMux) func() {
	b := &gpsBackend{}
	b.state = b.base(gpsState{State: "stopped", Diagnostics: "GPS receiver stopped"})
	b.hub = newHWHub(b.subscribed, b.unsubscribed)
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		hwJSON(w, http.StatusOK, b.snapshot())
	})
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		b.hub.serve(w, r, func() (string, any) { return "state", b.snapshot() })
	})
	mux.HandleFunc("POST /api/retry", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		q := b.retry
		b.mu.Unlock()
		if q != nil {
			select {
			case q <- struct{}{}:
			default:
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return b.shutdown
}

func (b *gpsBackend) base(s gpsState) gpsState {
	s.Device, s.Baud = gpsDevice, gpsBaud
	if p := os.Getenv("CAP_GPS_UART_DEVICE"); p != "" {
		s.Device = p
	}
	s.Module, s.Protocol = "ATGM336H-6N / AT6668", "NMEA 0183 4.1"
	s.Attempt = b.attempt
	if s.Satellites == nil {
		s.Satellites = []gpsSat{}
	}
	return s
}

func (b *gpsBackend) snapshot() gpsState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *gpsBackend) set(s gpsState) {
	b.mu.Lock()
	b.state = b.base(s)
	st := b.state
	b.mu.Unlock()
	b.hub.publish("state", st)
}

func (b *gpsBackend) subscribed() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.idle != nil {
		b.idle.Stop()
		b.idle = nil
	}
	if b.running {
		return
	}
	b.running = true
	b.stop, b.done, b.retry = make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	go b.worker(b.stop, b.done, b.retry)
}

func (b *gpsBackend) unsubscribed() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.idle != nil {
		b.idle.Stop()
	}
	b.idle = time.AfterFunc(loraIdleOff, func() {
		if b.hub.count() == 0 {
			b.shutdown()
		}
	})
}

func (b *gpsBackend) shutdown() {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return
	}
	b.running = false
	stop, done := b.stop, b.done
	b.mu.Unlock()
	close(stop)
	<-done
}

func gpsErrorText(err error) string {
	var en syscall.Errno
	if errors.As(err, &en) {
		switch en {
		case syscall.EACCES, syscall.EPERM:
			return "Permission denied: " + err.Error()
		case syscall.EWOULDBLOCK, syscall.EBUSY:
			return "GPS port is in use: " + err.Error()
		}
	}
	return err.Error()
}

func (b *gpsBackend) worker(stop, done, retry chan struct{}) {
	defer close(done)
	for {
		b.mu.Lock()
		b.attempt++
		b.mu.Unlock()
		b.set(gpsState{State: "starting", Diagnostics: "Opening GPS receiver"})
		err := b.run(stop)
		if err == nil {
			b.set(gpsState{State: "stopped", Diagnostics: "GPS receiver stopped"})
			return
		}
		msg := gpsErrorText(err)
		b.set(gpsState{State: "error", Error: msg, Diagnostics: msg})
		select { // the original retries on Enter; here on Retry
		case <-stop:
			b.set(gpsState{State: "stopped", Diagnostics: "GPS receiver stopped"})
			return
		case <-retry:
		}
	}
}

// run opens the receiver and streams state until stop (nil) or an error.
func (b *gpsBackend) run(stop chan struct{}) error {
	dev := b.base(gpsState{}).Device
	if loraConsoleOn(dev) {
		return errors.New(dev + " is assigned to the serial console; disable serial login before using GPS")
	}
	power, err := loraPowerOn(loraExt5V)
	if err != nil {
		return errors.New("Cap power enable failed: " + err.Error())
	}
	defer power.release()
	time.Sleep(150 * time.Millisecond)
	port, err := openLoraSerial(dev)
	if err != nil {
		return err
	}
	defer port.close()

	type chunk struct {
		b   []byte
		err error
	}
	data := make(chan chunk, 8)
	quit := make(chan struct{})
	defer close(quit)
	go func() {
		buf := make([]byte, 512)
		for {
			select {
			case <-quit:
				return
			default:
			}
			_ = port.f.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, err := port.f.Read(buf)
			if n > 0 {
				select {
				case data <- chunk{b: append([]byte(nil), buf[:n]...)}:
				case <-quit:
					return
				}
			}
			if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
				select {
				case data <- chunk{err: err}:
				case <-quit:
				}
				return
			}
		}
	}()

	dec := newNmeaDecoder()
	opened := time.Now()
	tick := time.NewTicker(gpsPublishEvery)
	defer tick.Stop()
	publish := func() {
		s := gpsStatus(dec, opened, time.Now())
		b.set(s)
	}
	publish()
	for {
		select {
		case <-stop:
			return nil
		case c := <-data:
			if c.err != nil {
				return errors.New("UART read failed: " + c.err.Error())
			}
			first := dec.feed(c.b, time.Now()) && !dec.hasFix && dec.Sentences == 1
			if first {
				publish()
			}
		case <-tick.C:
			publish()
		}
	}
}
