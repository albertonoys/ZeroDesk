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

func TestDependsSatisfied(t *testing.T) {
	inst := map[string]string{"libc6": "2.41-12", "python3": "3.13.5-1", "python3-pil": "11.1.0-5"}
	cases := []struct {
		field string
		want  bool
	}{
		{"", true},
		{"libc6 (>= 2.34), python3", true},
		{"python3:any (>= 3.11), python3-pil", true},
		{"python3 (>= 3.14)", false},
		{"python3-numpy", false},
		{"python3-numpy | python3-pil", true},
		{"libc6 (<< 2.30)", false},
		{"libc6 (= 2.41-12)", true},
	}
	for _, c := range cases {
		if got := dependsSatisfied(c.field, inst); got != c.want {
			t.Errorf("%q: got %v, want %v", c.field, got, c.want)
		}
	}
}
