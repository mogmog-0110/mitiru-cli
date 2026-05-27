package engine

import "testing"

func TestParseSemver(t *testing.T) {
	cases := []struct {
		in   string
		want Semver
		ok   bool
	}{
		{"0.7.0", Semver{0, 7, 0}, true},
		{"v0.7.0", Semver{0, 7, 0}, true},
		{"V1.2.3", Semver{1, 2, 3}, true},
		{" 0.10.2 ", Semver{0, 10, 2}, true},
		{"0.7", Semver{}, false},
		{"0.7.0-rc1", Semver{}, false},
		{"latest", Semver{}, false},
		{"x.y.z", Semver{}, false},
	}
	for _, c := range cases {
		got, ok := ParseSemver(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseSemver(%q) = %v,%v; want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestSemverCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.6.0", "0.7.0", -1},
		{"0.7.0", "0.6.0", 1},
		{"0.7.0", "0.7.0", 0},
		{"0.7.0", "0.7.1", -1},
		{"0.9.0", "0.10.0", -1}, // numeric, not lexical
		{"1.0.0", "0.99.99", 1},
	}
	for _, c := range cases {
		a, _ := ParseSemver(c.a)
		b, _ := ParseSemver(c.b)
		if got := a.Compare(b); got != c.want {
			t.Errorf("Compare(%s,%s) = %d; want %d", c.a, c.b, got, c.want)
		}
	}
}
