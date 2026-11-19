package main

import (
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQRKeyWorksOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zerodesk-qr")
	now := time.Unix(1_800_000_000, 0)
	k, err := currentQRKey(path, now)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode: %v %v", fi, err)
	}
	if takeQRKey(path, "wrongwrongwrongw", now) {
		t.Fatal("wrong key accepted")
	}
	if !takeQRKey(path, k.key, now) {
		t.Fatal("key rejected")
	}
	if takeQRKey(path, k.key, now) {
		t.Fatal("key accepted twice")
	}
}

func TestQRKeyExpiresAndRenews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zerodesk-qr")
	now := time.Unix(1_800_000_000, 0)
	k1, _ := currentQRKey(path, now)
	if k, _ := currentQRKey(path, now.Add(time.Minute)); k.key != k1.key {
		t.Fatal("renewed too early")
	}
	k2, _ := currentQRKey(path, now.Add(qrKeyRenew))
	if k2.key == k1.key {
		t.Fatal("not renewed")
	}
	// the previous code still works until it expires
	if !takeQRKey(path, k1.key, now.Add(qrKeyRenew+time.Second)) {
		t.Fatal("previous key rejected")
	}
	k3, _ := currentQRKey(path, now.Add(qrKeyRenew))
	if takeQRKey(path, k3.key, now.Add(qrKeyRenew+qrKeyLifetime)) {
		t.Fatal("expired key accepted")
	}
}

func TestQRLoginURL(t *testing.T) {
	if got := qrLoginURL("192.0.2.10", true, "k"); got != "http://192.0.2.10/q/k" {
		t.Fatal(got)
	}
	if got := qrLoginURL("192.0.2.10", false, "k"); got != "http://192.0.2.10:8686/q/k" {
		t.Fatal(got)
	}
}

func TestDrawQRFits(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 170))
	k := newQRKey(time.Now())
	box, err := drawQR(img, image.Rect(162, 12, 308, 158), qrLoginURL("192.168.100.200", false, k.key), 2)
	if err != nil {
		t.Fatal(err)
	}
	if !box.In(image.Rect(162, 12, 308, 158)) || box.Dx() < 100 {
		t.Fatalf("box %v", box)
	}
}
