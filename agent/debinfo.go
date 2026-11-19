package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
)

// What the Installer window shows before installing a .deb: the package's
// control fields, the launcher entry it adds (name and icon) and the version
// already installed, if any. Nothing here needs administrator rights.

type debInfo struct {
	Package     string `json:"package"`
	Name        string `json:"name"` // the launcher entry's Name=, else the package name
	Version     string `json:"version"`
	Description string `json:"description"`
	Maintainer  string `json:"maintainer"`
	SizeKB      int    `json:"size_kb"` // Installed-Size
	Installed   string `json:"installed,omitempty"`
	HasIcon     bool   `json:"has_icon"`
}

const maxDebIcon = 1 << 20

func debControl(ctx context.Context, p string) (map[string]string, error) {
	out, err := exec.CommandContext(ctx, "dpkg-deb", "-f", p).Output()
	if err != nil {
		return nil, err
	}
	return parseControl(out), nil
}

// parseControl reads RFC 822 style fields; continuation lines are joined to
// the field before them ("." stands for an empty line).
func parseControl(b []byte) map[string]string {
	f := map[string]string{}
	key := ""
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if key != "" {
				t := strings.TrimSpace(line)
				if t == "." {
					t = ""
				}
				f[key] += "\n" + t
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(k)
		f[key] = strings.TrimSpace(v)
	}
	return f
}

// debLauncherEntry finds the launcher entry the package installs and returns
// its Name= and the bytes of its icon.
func debLauncherEntry(ctx context.Context, p string) (name string, icon []byte) {
	cmd := exec.CommandContext(ctx, "dpkg-deb", "--fsys-tarfile", p)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", nil
	}
	if err := cmd.Start(); err != nil {
		return "", nil
	}
	defer cmd.Wait()
	defer io.Copy(io.Discard, out)

	var desktop []byte
	images := map[string][]byte{}
	tr := tar.NewReader(out)
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		n := path.Clean("/" + strings.TrimPrefix(h.Name, "./"))
		switch {
		case desktop == nil && strings.HasPrefix(n, "/usr/share/APPLaunch/applications/") && strings.HasSuffix(n, ".desktop") && h.Size < 64<<10:
			desktop, _ = io.ReadAll(tr)
		case strings.HasSuffix(strings.ToLower(n), ".png") && h.Size < maxDebIcon &&
			(strings.HasPrefix(n, "/usr/share/APPLaunch/") || strings.HasPrefix(n, "/usr/share/icons/") || strings.HasPrefix(n, "/usr/share/pixmaps/")):
			b, _ := io.ReadAll(tr)
			images[n] = b
		}
	}
	if desktop == nil {
		return "", nil
	}
	var iconRef string
	for _, line := range strings.Split(string(desktop), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "Name":
			if name == "" {
				name = strings.TrimSpace(v)
			}
		case "Icon":
			iconRef = strings.TrimSpace(v)
		}
	}
	if iconRef != "" {
		// absolute, relative to /usr/share/APPLaunch, or a bare file name that
		// the launcher looks up in share/images
		for _, c := range []string{iconRef, "/usr/share/APPLaunch/" + iconRef, "/usr/share/APPLaunch/share/images/" + iconRef} {
			if b, ok := images[path.Clean(c)]; ok {
				return name, b
			}
		}
	}
	return name, nil
}

func installedVersion(ctx context.Context, pkg string) string {
	out, err := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${Status}\t${Version}", pkg).Output()
	if err != nil {
		return ""
	}
	status, version, _ := strings.Cut(string(out), "\t")
	if !strings.HasSuffix(status, " installed") {
		return ""
	}
	return version
}

func debPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	p, err := cleanPath(r.URL.Query().Get("path"))
	if err != nil || !strings.HasSuffix(p, ".deb") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path must be an absolute .deb file"})
		return "", false
	}
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return "", false
	}
	return p, true
}

// GET /api/deb/info?path=
func handleDebInfo(w http.ResponseWriter, r *http.Request) {
	p, ok := debPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	f, err := debControl(ctx, p)
	if err != nil || f["Package"] == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "not a Debian package"})
		return
	}
	info := debInfo{
		Package:     f["Package"],
		Version:     f["Version"],
		Description: f["Description"],
		Maintainer:  f["Maintainer"],
		Installed:   installedVersion(ctx, f["Package"]),
	}
	info.SizeKB, _ = strconv.Atoi(f["Installed-Size"])
	name, icon := debLauncherEntry(ctx, p)
	info.Name, info.HasIcon = name, icon != nil
	if info.Name == "" {
		info.Name = info.Package
	}
	writeJSON(w, http.StatusOK, info)
}

// GET /api/deb/icon?path=
func handleDebIcon(w http.ResponseWriter, r *http.Request) {
	p, ok := debPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	_, icon := debLauncherEntry(ctx, p)
	if icon == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write(icon)
}
