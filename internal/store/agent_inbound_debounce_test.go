package store

import "testing"

func TestParseInboundDebounceMsFromOtherConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  []byte
		want int
		ok   bool
	}{
		{name: "missing", raw: []byte(`{"prompt_mode":"full"}`), ok: false},
		{name: "zero", raw: []byte(`{"inbound_debounce_ms":0}`), want: 0, ok: true},
		{name: "positive", raw: []byte(`{"inbound_debounce_ms":500}`), want: 500, ok: true},
		{name: "malformed", raw: []byte(`{"inbound_debounce_ms":`), ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseInboundDebounceMsFromOtherConfig(tc.raw)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("ParseInboundDebounceMsFromOtherConfig() = (%d, %t), want (%d, %t)", got, ok, tc.want, tc.ok)
			}
		})
	}
}
