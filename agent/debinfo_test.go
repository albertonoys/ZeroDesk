package main

import "testing"

func TestParseControl(t *testing.T) {
	f := parseControl([]byte("Package: zerodesk\nVersion: 0.2.2-1\nInstalled-Size: 15000\nDescription: Desktop\n first line\n .\n second\n"))
	if f["Package"] != "zerodesk" || f["Version"] != "0.2.2-1" || f["Installed-Size"] != "15000" {
		t.Fatalf("%v", f)
	}
	if f["Description"] != "Desktop\nfirst line\n\nsecond" {
		t.Fatalf("description %q", f["Description"])
	}
}
