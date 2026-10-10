package render

import "testing"

func TestCount(t *testing.T) {
	for v, want := range map[int64]string{
		0: "0", 793: "793", 4521: "4521", 10000: "10K", 12345: "12.3K",
		128_400: "128K", 1_260_000: "1.3M", 12_000_000: "12M", -42: "-42",
	} {
		if got := Count(v); got != want {
			t.Errorf("Count(%d) = %q, want %q", v, got, want)
		}
	}
}

func TestDelta(t *testing.T) {
	for v, want := range map[int64]string{0: "", 18: "+18", -3: "-3", 12_500: "+12.5K"} {
		if got := Delta(v); got != want {
			t.Errorf("Delta(%d) = %q, want %q", v, got, want)
		}
	}
}
