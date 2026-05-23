package mundane

import "testing"

func TestParseDurationMs(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"500ms", 500, false},
		{"1s", 1000, false},
		{"30s", 30000, false},
		{"5m", 5 * 60 * 1000, false},
		{"2h", 2 * 60 * 60 * 1000, false},
		{"1d", 24 * 60 * 60 * 1000, false},
		{"2.5s", 2500, false},
		{"  10s  ", 10000, false},
		{"", 0, true},
		{"10", 0, true},
		{"10x", 0, true},
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := ParseDurationMs(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseDurationMs(%q): want error, got %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDurationMs(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseDurationMs(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
