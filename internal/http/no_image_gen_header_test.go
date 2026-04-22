package http

import (
	"net/http"
	"testing"
)

func TestParseNoImageGen(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"1", true},
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"yes", true},
		{"YES", true},
		{"", false},
		{"0", false},
		{"false", false},
		{"False", false},
		{"no", false},
		{"off", false},
	}

	for _, c := range cases {
		r, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		if c.header != "" {
			r.Header.Set("X-Goclaw-No-Image-Gen", c.header)
		}
		got := parseNoImageGen(r)
		if got != c.want {
			t.Errorf("header %q: parseNoImageGen = %v, want %v", c.header, got, c.want)
		}
	}
}
