// Package secretvault resolves {{secret:name}} placeholders at tool execution
// time and keeps the resolved values from reaching the LLM, sessions, traces,
// events or logs.
package secretvault

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"unicode/utf8"
)

// Variants returns the forms in which a secret value commonly reappears in
// tool output: as-is, URL-encoded, base64 (std/url, padded and raw) and
// JSON-escaped. Empty input yields nothing. The result is de-duplicated.
//
// Limitation: a secret embedded in a larger base64 payload at a non-aligned
// offset cannot be recognised; only the standalone encodings are covered.
func Variants(value string) []string {
	if value == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(v string) {
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}

	add(value)
	add(url.QueryEscape(value))
	add(url.PathEscape(value))
	raw := []byte(value)
	add(base64.StdEncoding.EncodeToString(raw))
	add(base64.URLEncoding.EncodeToString(raw))
	add(base64.RawStdEncoding.EncodeToString(raw))
	add(base64.RawURLEncoding.EncodeToString(raw))
	if b, err := json.Marshal(value); err == nil && len(b) >= 2 {
		add(string(b[1 : len(b)-1]))
	}
	if q := strconv.Quote(value); len(q) >= 2 {
		add(q[1 : len(q)-1])
	}
	return out
}

// PayloadVariants returns the redaction needles for a login or API key
// payload: variants of the secret, of the username, and of the
// "username:secret" pair used by HTTP Basic authentication.
func PayloadVariants(username, secret string) []string {
	out := Variants(secret)
	if username != "" {
		out = append(out, Variants(username)...)
		out = append(out, Variants(username+":"+secret)...)
	}
	return out
}

// isWordRune reports whether r is part of an identifier-like token. Short
// secrets are only redacted when not surrounded by such characters.
func isWordRune(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r > 127
}

func hasBoundaryAt(s string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(s[:start])
		if isWordRune(r) {
			return false
		}
	}
	if end < len(s) {
		r, _ := utf8.DecodeRuneInString(s[end:])
		if isWordRune(r) {
			return false
		}
	}
	return true
}
