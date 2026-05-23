package mundane

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var durationRe = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)(ms|s|m|h|d)\s*$`)

var unitMs = map[string]int64{
	"ms": 1,
	"s":  1000,
	"m":  60 * 1000,
	"h":  60 * 60 * 1000,
	"d":  24 * 60 * 60 * 1000,
}

// ParseDurationMs parses "30s", "5m", "2h", "500ms", "1d", "2.5s" into ms.
func ParseDurationMs(s string) (int64, error) {
	m := durationRe.FindStringSubmatch(strings.ToLower(s))
	if m == nil {
		return 0, fmt.Errorf("invalid duration %q: expected e.g. '500ms', '30s', '5m', '2h', '1d'", s)
	}
	mag, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid duration magnitude %q: %w", m[1], err)
	}
	unit, ok := unitMs[m[2]]
	if !ok {
		return 0, fmt.Errorf("invalid duration unit %q", m[2])
	}
	return int64(math.Round(mag * float64(unit))), nil
}
