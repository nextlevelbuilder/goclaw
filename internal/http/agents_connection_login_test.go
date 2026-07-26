package http

import "testing"

func TestExtractOAuthToken(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// The REAL setup-token prefix is sk-ant-at<NN>- — this is the valid token.
		{"sk-ant-at token (real format)", "Your OAuth token (valid for 1 year):\nsk-ant-at01-AbC_dEf123XyZ_456QwErTyUiOpAsDfGhJkLzXcVbNm09\nDone.", "sk-ant-at01-AbC_dEf123XyZ_456QwErTyUiOpAsDfGhJkLzXcVbNm09"},
		{"token amid noise", "blah sk-ant-at01-ZZZ111222333444555666777888999000AaBbCcDdEeFf blah", "sk-ant-at01-ZZZ111222333444555666777888999000AaBbCcDdEeFf"},
		// Tag-agnostic: an oat/api-style prefix would also match (defensive).
		{"sk-ant-oat token also matches", "sk-ant-oat01-AbC_dEf123XyZ_456QwErTyUiOpAsDfGhJkLzXcVbNm09", "sk-ant-oat01-AbC_dEf123XyZ_456QwErTyUiOpAsDfGhJkLzXcVbNm09"},
		{"bare long token rejected (needs sk-ant- prefix)", "Token:\nAbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-ABCDEFON\n", ""},
		{"short body rejected (partial mid-render frame)", "sk-ant-at01-tooShort", ""},
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
