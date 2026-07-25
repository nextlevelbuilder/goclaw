package http

import "testing"

func TestExtractOAuthToken(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"sk-ant-oat token", "Success! Your token:\nsk-ant-oat01-AbC_dEf123XyZ_456QwErTyUiOp\nDone.", "sk-ant-oat01-AbC_dEf123XyZ_456QwErTyUiOp"},
		{"token amid noise", "blah sk-ant-api03-ZZZ111222333444555666777 blah", "sk-ant-api03-ZZZ111222333444555666777"},
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
