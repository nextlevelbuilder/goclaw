package agent

import "testing"

// TestDynamicSummaryMax validates boundary cases for dynamicSummaryMax.
// Formula: out = in/25, clamped to [1024, 8192].
func TestDynamicSummaryMax(t *testing.T) {
	cases := []struct {
		input int
		want  int
	}{
		{0, 1024},       // zero → floor
		{20000, 1024},   // 20000/25=800 → below floor, clamped
		{25000, 1024},   // 25000/25=1000 → below floor, clamped
		{26000, 1040},   // 26000/25=1040 → just above floor
		{100000, 4000},  // 100000/25=4000 → mid-range
		{204800, 8192},  // 204800/25=8192 → exactly at cap
		{500000, 8192},  // 500000/25=20000 → above cap, clamped
	}
	for _, tc := range cases {
		got := dynamicSummaryMax(tc.input)
		if got != tc.want {
			t.Errorf("dynamicSummaryMax(%d) = %d, want %d", tc.input, got, tc.want)
		}
	}
}
