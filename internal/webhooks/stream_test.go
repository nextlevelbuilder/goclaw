package webhooks

import "testing"

func TestResolveStream(t *testing.T) {
	tr := true
	fa := false
	cases := []struct {
		name string
		in   *bool
		want bool
	}{
		{"nil uses default (true)", nil, true},
		{"explicit true", &tr, true},
		{"explicit false", &fa, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveStream(c.in); got != c.want {
				t.Fatalf("ResolveStream(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
