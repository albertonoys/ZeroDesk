package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Where the App Store may fetch from. The registry carries each package's
// download URL and sha256, so every request, and every hop of a redirect,
// must be HTTPS to an official host: a registry read over plain HTTP could
// be swapped on the network for one that points at someone else's package
// with a matching checksum. (cardputerzero.github.io answers with a
// redirect to http://cardputer.cc, which is why the registry is read from
// https://cardputer.cc directly.)

// storeOrg is the GitHub organisation that publishes the Store's packages,
// icons and screenshots.
const storeOrg = "cardputerzero"

// hosts any path of which belongs to the Store
var storeHostsAnyPath = map[string]bool{
	"cardputer.cc":            true, // the registry site (GitHub Pages)
	"cardputerzero.github.io": true, // the same site's GitHub name
	"cardputer-zero-repo.oss-cn-shenzhen.aliyuncs.com": true, // China mirror
}

// GitHub hosts shared by everyone: only paths under the organisation
var storeHostsOrgPath = map[string]bool{
	"github.com":                true,
	"raw.githubusercontent.com": true,
}

// GitHub's signed download hosts: only reached as the redirect of a
// github.com release URL, never named by the registry itself
var storeRedirectHosts = map[string]bool{
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

// storeURLAllowed reports whether u may start an App Store request.
func storeURLAllowed(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if p := u.Port(); p != "" && p != "443" {
		return false
	}
	if storeHostsAnyPath[host] {
		return true
	}
	if storeHostsOrgPath[host] {
		first, _, _ := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
		return strings.EqualFold(first, storeOrg)
	}
	return false
}

// storeCheckRedirect follows a redirect only to an allowed HTTPS address.
func storeCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	u := req.URL
	if storeURLAllowed(u) || (u.Scheme == "https" && u.User == nil && (u.Port() == "" || u.Port() == "443") && storeRedirectHosts[strings.ToLower(u.Hostname())]) {
		return nil
	}
	return fmt.Errorf("refusing to follow a redirect to %s://%s", u.Scheme, u.Host)
}
