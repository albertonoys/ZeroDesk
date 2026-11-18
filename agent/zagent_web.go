package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// Zagent's own web page (zagent-web, ZeroAgent repo tools/zagent-web) listens
// on 127.0.0.1:42619. ZeroDesk owns port 80, so it forwards the mDNS name
// zagent.local there; those requests are marked as forwarded and zagent-web
// asks them to sign in with its own code. ZeroDesk's Zagent window reaches the
// chat endpoints through /api/zagent/web/*, behind ZeroDesk's sign-in.

const (
	zagentWebAddr = "127.0.0.1:42619"
	zagentHost    = "zagent.local"
)

var zagentWebTarget = &url.URL{Scheme: "http", Host: zagentWebAddr}

func requestHost(r *http.Request) string {
	host := strings.ToLower(r.Host)
	if h, _, ok := strings.Cut(host, ":"); ok && !strings.HasPrefix(host, "[") {
		return h
	}
	return host
}

// zagentWebHost sends requests for http://zagent.local to zagent-web.
func zagentWebHost(next http.Handler) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(zagentWebTarget)
			pr.SetXForwarded() // zagent-web: not a local caller, sign in
			pr.Out.Host = pr.In.Host
		},
		FlushInterval: -1, // chat replies stream as server-sent events
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestHost(r) == zagentHost {
			proxy.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

var zagentWebPaths = map[string]bool{"chat": true, "chat/approve": true, "chat/stop": true, "status": true}

// handleZagentWeb relays the chat endpoints for ZeroDesk's Zagent window.
func handleZagentWeb() http.HandlerFunc {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(zagentWebTarget)
			pr.Out.URL.Path = "/api/" + pr.In.PathValue("path")
			pr.Out.URL.RawPath = ""
			// The caller already passed ZeroDesk's sign-in: no forwarding
			// headers (zagent-web then treats it as the device itself) and
			// no ZeroDesk cookie downstream.
			pr.Out.Header.Del("X-Forwarded-For")
			pr.Out.Header.Del("Forwarded")
			pr.Out.Header.Del("Cookie")
		},
		FlushInterval: -1,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !zagentWebPaths[r.PathValue("path")] {
			http.NotFound(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	}
}
