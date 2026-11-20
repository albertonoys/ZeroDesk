package main

import (
	"net/http"
	"net/url"
	"testing"
)

func TestStoreURLAllowed(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://cardputer.cc/generated/registry.json":                                       true,
		"https://cardputer-zero-repo.oss-cn-shenzhen.aliyuncs.com/packages/cn/registry.json": true,
		"https://github.com/CardputerZero/packages/releases/download/apt-pool/x_1_arm64.deb": true,
		"https://github.com/cardputerzero/packages/releases/download/apt-pool/x.deb":         true,
		"https://raw.githubusercontent.com/CardputerZero/packages/main/pool/main/x/icon.png": true,
		// someone else's release on the same host
		"https://github.com/attacker/evil/releases/download/v1/x.deb":           false,
		"https://raw.githubusercontent.com/attacker/repo/main/icon.png":         false,
		"https://github.com/CardputerZeroEvil/packages/releases/download/x.deb": false,
		"http://cardputer.cc/generated/registry.json":                           false,
		"https://cardputer.cc:8443/generated/registry.json":                     false,
		"https://user@cardputer.cc/generated/registry.json":                     false,
		"https://objects.githubusercontent.com/whatever":                        false, // redirect target only
		"https://cardputer.cc.evil.example/generated/registry.json":             false,
		"https://evil.example/https://cardputer.cc/generated/registry.json":     false,
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := storeURLAllowed(u); got != want {
			t.Errorf("%s: got %v, want %v", raw, got, want)
		}
	}
}

func TestStoreCheckRedirect(t *testing.T) {
	req := func(raw string) *http.Request {
		r, _ := http.NewRequest(http.MethodGet, raw, nil)
		return r
	}
	ok := []string{
		"https://cardputer.cc/generated/registry.json",
		"https://release-assets.githubusercontent.com/github-production-release-asset/1?sig=x",
		"https://objects.githubusercontent.com/github-production-release-asset-2e65be/1",
	}
	for _, raw := range ok {
		if err := storeCheckRedirect(req(raw), nil); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
	bad := []string{
		"http://cardputer.cc/generated/registry.json", // what cardputerzero.github.io answers with
		"http://release-assets.githubusercontent.com/x",
		"https://github.com/attacker/evil/releases/download/v1/x.deb",
		"https://evil.example/x.deb",
	}
	for _, raw := range bad {
		if err := storeCheckRedirect(req(raw), nil); err == nil {
			t.Errorf("%s: redirect must be refused", raw)
		}
	}
	if err := storeCheckRedirect(req(ok[0]), make([]*http.Request, 10)); err == nil {
		t.Error("an 11th redirect must be refused")
	}
}

// The client refuses an HTTP hop, and every registry source passes the rules.
func TestStoreClientRefusesHTTPRedirect(t *testing.T) {
	c := &http.Client{CheckRedirect: storeCheckRedirect}
	r, _ := http.NewRequest(http.MethodGet, "https://cardputer.cc/x", nil)
	via := []*http.Request{r}
	next, _ := http.NewRequest(http.MethodGet, "http://cardputer.cc/x", nil)
	if err := c.CheckRedirect(next, via); err == nil {
		t.Fatal("http hop accepted")
	}
	for _, u := range storeSources {
		pu, _ := url.Parse(u)
		if !storeURLAllowed(pu) {
			t.Errorf("registry source %s is not an allowed store URL", u)
		}
	}
}
