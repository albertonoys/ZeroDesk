package main

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// nmea appends the checksum to a sentence body ("GPGGA,...").
func nmea(body string) string {
	var sum byte
	for i := 0; i < len(body); i++ {
		sum ^= body[i]
	}
	return fmt.Sprintf("$%s*%02X\r\n", body, sum)
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestNmeaChecksum(t *testing.T) {
	// classic reference sentence with a known checksum
	_, kind, f, ok := nmeaFields("$GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*47")
	if !ok || kind != "GGA" || f[1] != "4807.038" {
		t.Fatalf("valid GGA rejected: %v %q", ok, kind)
	}
	if _, _, _, ok := nmeaFields("$GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*48"); ok {
		t.Fatal("bad checksum accepted")
	}
	if _, _, _, ok := nmeaFields("$GPGGA,123519,4807.038,N"); ok {
		t.Fatal("missing checksum accepted")
	}
}

func TestNmeaDecoderFix(t *testing.T) {
	d := newNmeaDecoder()
	now := time.Unix(1000, 0)
	feed := func(s string) { d.feed([]byte(s), now) }
	feed(nmea("GNRMC,014530.000,A,2232.6416,N,11402.3713,E,1.20,87.5,181126,,,A,V"))
	feed(nmea("GNGGA,014530.000,2232.6416,N,11402.3713,E,1,09,1.10,35.2,M,-2.1,M,,"))
	feed(nmea("GNGSA,A,3,05,13,15,18,,,,,,,,,1.90,1.10,1.55,1"))
	feed(nmea("GNGSA,A,3,07,10,,,,,,,,,,,1.90,1.10,1.55,4"))
	feed(nmea("GPGSV,2,1,05,05,45,120,38,13,60,300,42,15,20,045,30,18,10,200,25,0"))
	feed(nmea("GPGSV,2,2,05,29,05,330,,0"))
	feed(nmea("BDGSV,1,1,03,07,70,010,44,10,33,250,36,30,12,100,,0"))
	feed(nmea("GNVTG,87.5,T,,M,1.20,N,2.22,K,A"))
	feed(nmea("GNZDA,014530.000,18,11,2026,00,00"))
	feed("$GPTXT,01,01,02,ANTSTATUS=OPEN*2B\r\n") // unknown: ignored
	feed("$GNGGA,garbage*00\r\n")                 // bad checksum: invalid

	if !d.PositionValid || !near(d.Lat, 22+32.6416/60) || !near(d.Lon, 114+2.3713/60) {
		t.Fatalf("position %v %v %v", d.PositionValid, d.Lat, d.Lon)
	}
	if !d.AltValid || d.Alt != 35.2 || d.SatsUsed != 9 || d.FixQuality != 1 || d.FixType != 3 {
		t.Fatalf("gga/gsa %+v", d)
	}
	if !d.SpeedValid || d.SpeedKph != 2.22 || !d.CourseValid || d.Course != 87.5 {
		t.Fatalf("vtg speed %v course %v", d.SpeedKph, d.Course)
	}
	if !d.HDOPValid || d.HDOP != 1.10 || d.PDOP != 1.90 || d.VDOP != 1.55 {
		t.Fatal("dop")
	}
	if !d.UTC.DateValid || d.UTC.Year != 2026 || d.UTC.Month != 11 || d.UTC.Day != 18 || d.UTC.Hour != 1 || d.UTC.Minute != 45 || d.UTC.Second != 30 {
		t.Fatalf("utc %+v", d.UTC)
	}
	if d.Sentences != 9 || d.Invalid != 1 {
		t.Fatalf("counts sentences=%d invalid=%d", d.Sentences, d.Invalid)
	}

	sats := d.satellites(now)
	if len(sats) != 8 {
		t.Fatalf("satellites %d: %+v", len(sats), sats)
	}
	used := 0
	for _, s := range sats {
		if s.Used {
			used++
		}
		if s.System == gnssGPS && s.PRN == 29 && (s.SNR != nil || s.Elev == nil || *s.Elev != 5) {
			t.Fatalf("G29 %+v", s)
		}
		if s.System == gnssBeiDou && s.PRN == 7 && (!s.Used || *s.SNR != 44) {
			t.Fatalf("C07 %+v", s)
		}
	}
	if used != 6 {
		t.Fatalf("used %d", used)
	}

	st := gpsStatus(d, now, now)
	if st.State != "fixed" || st.Diagnostics != "3D position fix" || st.SatsVisible != 8 || st.Date != "2026-11-18" || st.Time != "01:45:30" {
		t.Fatalf("status %+v", st)
	}
	// stale after 3 s without sentences; satellites expire after 5 s
	st = gpsStatus(d, now, now.Add(4*time.Second))
	if st.State != "stale" {
		t.Fatalf("stale: %s", st.State)
	}
	if len(d.satellites(now.Add(6*time.Second))) != 0 {
		t.Fatal("satellites did not expire")
	}
}

func TestNmeaDecoderNoFix(t *testing.T) {
	d := newNmeaDecoder()
	now := time.Unix(1000, 0)
	if st := gpsStatus(d, now, now); st.State != "nodata" || st.Diagnostics != "No GPS data received" {
		t.Fatalf("nodata %+v", st)
	}
	d.feed([]byte(nmea("GPRMC,014530.000,V,,,,,,,181126,,,N,V")), now)
	d.feed([]byte(nmea("GPGGA,014530.000,,,,,0,00,99.99,,,,,,")), now)
	d.feed([]byte(nmea("GPGSA,A,1,,,,,,,,,,,,,99.99,99.99,99.99,1")), now)
	st := gpsStatus(d, now, now)
	if st.State != "searching" || st.Lat != nil || st.Alt != nil || st.FixType != 1 {
		t.Fatalf("searching %+v", st)
	}
	// partial lines and overlong garbage are counted as invalid, not crash
	d.feed([]byte("$GPGGA,1234"), now)
	d.feed(make([]byte, 300), now)
	d.feed([]byte("\n"), now)
	if d.Invalid != 1 {
		t.Fatalf("invalid %d", d.Invalid)
	}
}

func TestNmeaCoord(t *testing.T) {
	v, ok := nmeaCoord([]string{"4807.038", "S"}, 0)
	if !ok || !near(v, -(48+7.038/60)) {
		t.Fatalf("%v %v", v, ok)
	}
	if _, ok := nmeaCoord([]string{"4807.038", ""}, 0); ok {
		t.Fatal("missing hemisphere accepted")
	}
	// old VTG layout: course, magnetic, knots, kph
	d := newNmeaDecoder()
	d.feed([]byte(nmea("GPVTG,054.7,034.4,005.5,010.2")), time.Unix(0, 0))
	if d.SpeedKph != 10.2 || d.Course != 54.7 {
		t.Fatalf("old vtg %v %v", d.SpeedKph, d.Course)
	}
}
