package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a per-user web app in a temporary HOME
func setupWebApp(t *testing.T, manifest string) (*webApps, *http.ServeMux) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".local", "share", "zerodesk", "apps", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"manifest.webmanifest": manifest,
		"index.html":           "<p>hi</p>",
		"srv":                  "#!/bin/sh\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// a file outside the app directory
	_ = os.WriteFile(filepath.Join(home, "secret.txt"), []byte("nope"), 0o644)
	wa := newWebApps(newAppManager())
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/webapps", wa.handleList)
	mux.HandleFunc("POST /api/webapps/{id}/session", wa.handleSessionOpen)
	mux.HandleFunc("DELETE /api/webapps/session/{cap}", wa.handleSessionClose)
	mux.HandleFunc("GET /api/webapps/{id}/storage", wa.handleStorageGet)
	mux.HandleFunc("PUT /api/webapps/{id}/storage", wa.handleStoragePut)
	// same registration as main.go, next to the static catch-all
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		mux.HandleFunc(m+" /x/", wa.handleCap)
	}
	mux.HandleFunc("GET /", http.NotFound)
	return wa, mux
}

func do(mux *http.ServeMux, method, url, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, url, strings.NewReader(body)))
	return rec
}

const demoManifest = `{"name":"Demo App","short_name":"Demo","zerodesk":{"spec":1,
 "window":{"width":500,"height":400,"titlebar":"app"},
 "permissions":["storage","bogus"],
 "backend":{"exec":["srv"]}}}`

func TestWebAppListAndManifest(t *testing.T) {
	_, mux := setupWebApp(t, demoManifest)
	rec := do(mux, "GET", "/api/webapps", "")
	var list []webApp
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatalf("list: %v %s", err, rec.Body)
	}
	a := list[0]
	if a.ID != "demo" || a.Name != "Demo" || a.Titlebar != "app" || a.Width != 500 || !a.Backend {
		t.Fatalf("unexpected app %+v", a)
	}
	if len(a.Permissions) != 1 || a.Permissions[0] != "storage" {
		t.Fatalf("unknown permissions must be dropped: %v", a.Permissions)
	}
}

func TestWebAppRejectsNewerSpec(t *testing.T) {
	_, mux := setupWebApp(t, `{"name":"X","zerodesk":{"spec":2}}`)
	if body := do(mux, "GET", "/api/webapps", "").Body.String(); strings.TrimSpace(body) != "[]" {
		t.Fatalf("spec 2 must be ignored, got %s", body)
	}
}

func TestWebAppCapability(t *testing.T) {
	_, mux := setupWebApp(t, demoManifest)
	if rec := do(mux, "GET", "/x/deadbeef/index.html", ""); rec.Code != http.StatusGone {
		t.Fatalf("unknown capability: got %d", rec.Code)
	}
	rec := do(mux, "POST", "/api/webapps/demo/session", "")
	var s struct{ Base, Start string }
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil || !strings.HasPrefix(s.Base, "/x/") || s.Start != "index.html" {
		t.Fatalf("session: %v %s", err, rec.Body)
	}
	page := do(mux, "GET", s.Base+"index.html", "")
	if page.Code != 200 || page.Body.String() != "<p>hi</p>" {
		t.Fatalf("page: %d %q", page.Code, page.Body)
	}
	if page.Header().Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(page.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") {
		t.Fatalf("missing isolation headers: %v", page.Header())
	}
	if rec := do(mux, "GET", s.Base+"_zd/sdk.js", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "window.zd") {
		t.Fatalf("sdk: %d", rec.Code)
	}
	for _, p := range []string{"../../secret.txt", "..%2F..%2Fsecret.txt", "srv", "_zd/../../secret.txt"} {
		if rec := do(mux, "GET", s.Base+p, ""); rec.Code == 200 {
			t.Errorf("%s must not be served", p)
		}
	}
	do(mux, "DELETE", "/api/webapps/session/"+strings.Split(s.Base, "/")[2], "")
	if rec := do(mux, "GET", s.Base+"index.html", ""); rec.Code != http.StatusGone {
		t.Fatalf("closed window must lose access, got %d", rec.Code)
	}
}

func TestWebAppStorage(t *testing.T) {
	_, mux := setupWebApp(t, demoManifest)
	if rec := do(mux, "PUT", "/api/webapps/demo/storage", `{"a":1,"b":{"c":true}}`); rec.Code != http.StatusNoContent {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	do(mux, "PUT", "/api/webapps/demo/storage", `{"a":null}`)
	got := do(mux, "GET", "/api/webapps/demo/storage", "").Body.String()
	if got != `{"b":{"c":true}}` {
		t.Fatalf("storage = %s", got)
	}
	if rec := do(mux, "PUT", "/api/webapps/other/storage", `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown app: %d", rec.Code)
	}
}
