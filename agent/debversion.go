package main

import "strings"

// compareDebVersions orders two Debian version strings the way
// `dpkg --compare-versions` does (deb-version(7)): epoch, then upstream
// version, then revision, each compared with dpkg's mixed lexical/numeric
// rule where "~" sorts before everything, even the end of the string.
func compareDebVersions(a, b string) int {
	ea, ua, ra := splitDebVersion(a)
	eb, ub, rb := splitDebVersion(b)
	if c := compareNum(ea, eb); c != 0 {
		return c
	}
	if c := compareDebPart(ua, ub); c != 0 {
		return c
	}
	return compareDebPart(ra, rb)
}

func splitDebVersion(v string) (epoch, upstream, revision string) {
	v = strings.TrimSpace(v)
	if i := strings.IndexByte(v, ':'); i >= 0 {
		epoch, v = v[:i], v[i+1:]
	}
	if i := strings.LastIndexByte(v, '-'); i >= 0 {
		v, revision = v[:i], v[i+1:]
	}
	return epoch, v, revision
}

func compareNum(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

// order is dpkg's weight of one non-digit character
func order(c byte) int {
	switch {
	case c == '~':
		return -1
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		return int(c)
	case c == 0:
		return 0
	default:
		return int(c) + 256
	}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func compareDebPart(a, b string) int {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		// non-digit prefix
		for (i < len(a) && !isDigit(a[i])) || (j < len(b) && !isDigit(b[j])) {
			var ca, cb byte
			if i < len(a) && !isDigit(a[i]) {
				ca = a[i]
			}
			if j < len(b) && !isDigit(b[j]) {
				cb = b[j]
			}
			if oa, ob := order(ca), order(cb); oa != ob {
				if oa < ob {
					return -1
				}
				return 1
			}
			if ca != 0 {
				i++
			}
			if cb != 0 {
				j++
			}
			if ca == 0 && cb == 0 {
				break
			}
		}
		// digit run
		si := i
		for i < len(a) && isDigit(a[i]) {
			i++
		}
		sj := j
		for j < len(b) && isDigit(b[j]) {
			j++
		}
		if c := compareNum(a[si:i], b[sj:j]); c != 0 {
			return c
		}
	}
	return 0
}
