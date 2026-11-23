package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Trash follows the freedesktop.org Trash spec (~/.local/share/Trash with
// files/ and info/*.trashinfo), the same trash the Zero's own Linux desktop
// (pcmanfm) uses, so items show up in both places.

func trashDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "Trash")
}

type trashItem struct {
	Name         string    `json:"name"` // name inside the trash (unique)
	OriginalPath string    `json:"originalPath"`
	Deleted      time.Time `json:"deleted"`
	Dir          bool      `json:"dir"`
	Size         int64     `json:"size"`
}

// moveToTrash moves path into the trash and writes its .trashinfo.
func moveToTrash(path string) (string, error) {
	base := trashDir()
	files, info := filepath.Join(base, "files"), filepath.Join(base, "info")
	if err := os.MkdirAll(files, 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(info, 0o700); err != nil {
		return "", err
	}
	name := filepath.Base(path)
	stem, ext := name, ""
	if i := strings.LastIndex(name, "."); i > 0 {
		stem, ext = name[:i], name[i:]
	}
	// reserve a unique name by creating the .trashinfo exclusively
	var infoPath, target string
	for n := 1; ; n++ {
		candidate := name
		if n > 1 {
			candidate = fmt.Sprintf("%s %d%s", stem, n, ext)
		}
		infoPath = filepath.Join(info, candidate+".trashinfo")
		f, err := os.OpenFile(infoPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		u := (&url.URL{Path: path}).EscapedPath()
		fmt.Fprintf(f, "[Trash Info]\nPath=%s\nDeletionDate=%s\n", u, time.Now().Format("2006-01-02T15:04:05"))
		f.Close()
		target = filepath.Join(files, candidate)
		break
	}
	if err := os.Rename(path, target); err != nil {
		os.Remove(infoPath)
		return "", fmt.Errorf("move to trash: %w", err)
	}
	return filepath.Base(target), nil
}

func readTrashInfo(path string) (orig string, deleted time.Time) {
	f, err := os.Open(path)
	if err != nil {
		return "", time.Time{}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, "Path="); ok {
			if p, err := url.PathUnescape(v); err == nil {
				orig = p
			} else {
				orig = v
			}
		} else if v, ok := strings.CutPrefix(line, "DeletionDate="); ok {
			deleted, _ = time.ParseInLocation("2006-01-02T15:04:05", v, time.Local)
		}
	}
	return orig, deleted
}

func listTrash() []trashItem {
	base := trashDir()
	ents, _ := os.ReadDir(filepath.Join(base, "files"))
	out := make([]trashItem, 0, len(ents))
	for _, e := range ents {
		orig, deleted := readTrashInfo(filepath.Join(base, "info", e.Name()+".trashinfo"))
		it := trashItem{Name: e.Name(), OriginalPath: orig, Deleted: deleted}
		if st, err := os.Stat(filepath.Join(base, "files", e.Name())); err == nil {
			it.Dir, it.Size = st.IsDir(), st.Size()
		}
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Deleted.After(out[j].Deleted) })
	return out
}

func safeTrashName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00")
}

func handleTrashList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, listTrash())
}

func handleTrashOp(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Op   string `json:"op"` // restore | delete | empty
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	base := trashDir()
	switch body.Op {
	case "empty":
		for _, sub := range []string{"files", "info"} {
			ents, _ := os.ReadDir(filepath.Join(base, sub))
			for _, e := range ents {
				_ = os.RemoveAll(filepath.Join(base, sub, e.Name()))
			}
		}
	case "delete", "restore":
		if !safeTrashName(body.Name) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad name"})
			return
		}
		file := filepath.Join(base, "files", body.Name)
		info := filepath.Join(base, "info", body.Name+".trashinfo")
		if body.Op == "delete" {
			if err := os.RemoveAll(file); err != nil {
				fsError(w, err)
				return
			}
			os.Remove(info)
			break
		}
		orig, _ := readTrashInfo(info)
		if orig == "" || !filepath.IsAbs(orig) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "original location unknown"})
			return
		}
		if _, err := os.Lstat(orig); err == nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "an item with that name already exists at the original location"})
			return
		}
		if err := os.MkdirAll(filepath.Dir(orig), 0o755); err != nil {
			fsError(w, err)
			return
		}
		if err := os.Rename(file, orig); err != nil {
			fsError(w, err)
			return
		}
		os.Remove(info)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown op"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
