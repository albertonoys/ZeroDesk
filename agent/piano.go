package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// Splendid Grand Piano samples for the Piano app (smplr). The browser may not
// reach the sample host, so the device fetches each file once, keeps it in
// ~/.local/share/zerodesk/piano and serves it from there.

const pianoUpstream = "https://smpldsnds.github.io/sfzinstruments-splendid-grand-piano/samples/"

var (
	pianoName   = regexp.MustCompile(`^[A-Za-z]{1,3} [A-G]#?-?[0-9]\.(ogg|m4a)$`)
	pianoLocks  sync.Map                 // name → *sync.Mutex, one download per file
	pianoSlots  = make(chan struct{}, 6) // upstream downloads at a time
	pianoClient = &http.Client{Timeout: 60 * time.Second}
)

func pianoDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "zerodesk", "piano")
}

func handlePianoSample(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !pianoName.MatchString(name) {
		http.Error(w, "bad sample name", http.StatusBadRequest)
		return
	}
	path := filepath.Join(pianoDir(), name)
	if _, err := os.Stat(path); err != nil {
		if err := pianoFetch(r.Context(), name, path); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	if filepath.Ext(name) == ".ogg" {
		w.Header().Set("Content-Type", "audio/ogg")
	} else {
		w.Header().Set("Content-Type", "audio/mp4")
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}

func pianoFetch(ctx context.Context, name, path string) error {
	mu, _ := pianoLocks.LoadOrStore(name, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	if _, err := os.Stat(path); err == nil {
		return nil // another request finished it while we waited
	}

	select {
	case pianoSlots <- struct{}{}:
		defer func() { <-pianoSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// detached from the request: a closed tab should not waste a half-done download
	dctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(dctx, http.MethodGet, pianoUpstream+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	resp, err := pianoClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream %s", resp.Status)
	}
	part := path + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(part)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(part)
		return err
	}
	return os.Rename(part, path)
}
