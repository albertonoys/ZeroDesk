package main

import "testing"

func TestStaticProgram(t *testing.T) {
	// the test binary itself is an ELF on Linux; elsewhere it is not ELF
	if staticProgram("/no/such/program --flag") {
		t.Fatal("missing program reported static")
	}
	if staticProgram("") {
		t.Fatal("empty exec reported static")
	}
	if staticProgram("env A=1 /no/such/program") {
		t.Fatal("env prefix not skipped")
	}
}
