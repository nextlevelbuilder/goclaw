package http

import "testing"

func TestExtractOAuthToken(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"sk-ant-oat token", "Success! Your token:\nsk-ant-oat01-AbC_dEf123XyZ_456QwErTyUiOpAsDfGhJkLzXcVbNm09\nDone.", "sk-ant-oat01-AbC_dEf123XyZ_456QwErTyUiOpAsDfGhJkLzXcVbNm09"},
		{"token amid noise", "blah sk-ant-oat01-ZZZ111222333444555666777888999000AaBbCcDdEeFf blah", "sk-ant-oat01-ZZZ111222333444555666777888999000AaBbCcDdEeFf"},
		{"corrupted (dropped o) rejected", "sk-ant-at01-AbC_dEf123XyZ_456QwErTyUiOpAsDfGhJkLzXcVbNm09", ""},
		{"bare long token rejected (must have oat prefix)", "Token:\nAbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-ABCDEFON\n", ""},
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
