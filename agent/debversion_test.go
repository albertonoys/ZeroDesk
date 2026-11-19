package main

import "testing"

func TestCompareDebVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0", "1.0", 0},
		{"1.0", "1.1", -1},
		{"1.10", "1.9", 1},
		{"1.0~rc1", "1.0", -1},
		{"1.0", "1.0+b1", -1},
		{"1.0-1", "1.0-2", -1},
		{"1:0.1", "2.0", 1},
		{"0.2.3-1", "0.2.10-1", -1},
		{"1.1.0", "1.1.0-1", -1},
		{"2.0a", "2.0", 1},
		{"1.0~~", "1.0~", -1},
		{"007", "7", 0},
	}
	for _, c := range cases {
		if got := compareDebVersions(c.a, c.b); got != c.want {
			t.Errorf("compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := compareDebVersions(c.b, c.a); got != -c.want {
			t.Errorf("compare(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}
