// Package render turns plain values into strings that fit a 32x8 matrix.
package render

import (
	"fmt"
	"strconv"
	"strings"
)

// Count formats a counter compactly: 793, 4521, 12.3K, 128K, 1.2M.
func Count(v int64) string {
	switch {
	case v < 0:
		return "-" + Count(-v)
	case v < 10_000:
		return strconv.FormatInt(v, 10)
	case v < 100_000:
		return trimZero(fmt.Sprintf("%.1f", float64(v)/1000)) + "K"
	case v < 1_000_000:
		return strconv.FormatInt(v/1000, 10) + "K"
	case v < 10_000_000:
		return trimZero(fmt.Sprintf("%.1f", float64(v)/1_000_000)) + "M"
	default:
		return strconv.FormatInt(v/1_000_000, 10) + "M"
	}
}

// Delta formats a daily variation: "+18", "-3", "" for zero.
func Delta(d int64) string {
	switch {
	case d > 0:
		return "+" + Count(d)
	case d < 0:
		return Count(d)
	}
	return ""
}

func trimZero(s string) string { return strings.TrimSuffix(s, ".0") }
