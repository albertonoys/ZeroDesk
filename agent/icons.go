package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"

	_ "golang.org/x/image/webp"
)

// Caching for icons and the embedded web files, so the Dock and Apps paint
// every icon in their first frame instead of re-downloading them over Wi-Fi.

// ---- embedded files: content-hash ETags (the embed FS has no modtime) ----

var embedETags sync.Map // path -> `"<hash>"`

func embedETag(root fs.FS, p string) string {
	if v, ok := embedETags.Load(p); ok {
		return v.(string)
	}
	b, err := fs.ReadFile(root, p)
	if err != nil {
		return ""
	}
	sum := sha1.Sum(b)
	tag := `"` + hex.EncodeToString(sum[:8]) + `"`
	embedETags.Store(p, tag)
	return tag
}

func etagMatches(r *http.Request, tag string) bool {
	inm := r.Header.Get("If-None-Match")
	if inm == "" || tag == "" {
		return false
	}
	for _, t := range strings.Split(inm, ",") {
		t = strings.TrimSpace(t)
		if t == tag || t == "W/"+tag || t == "*" {
			return true
		}
	}
	return false
}

// ---- device app icons ----

type iconInfo struct {
	version string // changes when the file changes
	opaque  bool   // a full square (clip to the tile) or a shape on transparency
}

var iconInfos sync.Map // path -> iconInfo, keyed with mtime+size inside

// deviceIconInfo inspects an icon file once per version: the Dock and Apps
// need to know how to draw it before the image arrives.
func deviceIconInfo(p string) (iconInfo, bool) {
	st, err := os.Stat(p)
	if err != nil {
		return iconInfo{}, false
	}
	version := fmt.Sprintf("%x-%x", st.ModTime().Unix(), st.Size())
	if v, ok := iconInfos.Load(p); ok && v.(iconInfo).version == version {
		return v.(iconInfo), true
	}
	info := iconInfo{version: version, opaque: true}
	if f, err := os.Open(p); err == nil {
		img, _, err := image.Decode(f)
		f.Close()
		if err == nil {
			b := img.Bounds()
			if b.Dx() > 3 && b.Dy() > 3 {
				corner := func(x, y int) bool { _, _, _, a := img.At(x, y).RGBA(); return a>>8 > 200 }
				info.opaque = corner(b.Min.X+1, b.Min.Y+1) && corner(b.Max.X-2, b.Min.Y+1) && corner(b.Min.X+1, b.Max.Y-2)
			}
		}
	}
	iconInfos.Store(p, info)
	return info, true
}
