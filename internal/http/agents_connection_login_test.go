package http

import "testing"

func TestExtractOAuthToken(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"sk-ant-oat prefix", "Success! Your token:\nsk-ant-oat01-AbC_dEf-123\nDone.", "sk-ant-oat01-AbC_dEf-123"},
		{"prefix amid noise", "blah sk-ant-oat01-XYZ789 blah", "sk-ant-oat01-XYZ789"},
		{"bare long-token fallback", "Token:\nAbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-ABCDEFON\n", "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-ABCDEFON"},
		{"no token", "timed out waiting for token", ""},
		{"empty", "", ""},
		{"short strings ignored", "ok\ndone\nhttps://example.com", ""},
	}
	for _, c := range cases {
		if got := extractOAuthToken(c.in); got != c.want {
			t.Errorf("%s: extractOAuthToken() = %q, want %q", c.name, got, c.want)
		}
	}
}
