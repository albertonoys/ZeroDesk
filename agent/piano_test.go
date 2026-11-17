package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPianoSampleName(t *testing.T) {
	for _, ok := range []string{"PP C4.ogg", "PP B-1.ogg", "MF D#0.m4a", "Mf G#4.ogg", "Mp A0.ogg"} {
		if !pianoName.MatchString(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{"../x.ogg", "PP C4.wav", "PP C4.ogg/..", "PP H4.ogg", "PP C4"} {
		if pianoName.MatchString(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestPianoSampleRejectsBadName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/piano/{name}", handlePianoSample)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/piano/..%2Fetc%2Fpasswd", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}
