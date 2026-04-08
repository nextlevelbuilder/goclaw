package nodehost

import "unicode"

// doubleQuoteEscapes are the characters that can be escaped inside double quotes.
var doubleQuoteEscapes = map[byte]bool{
	'\\': true, '"': true, '$': true, '`': true, '\n': true, '\r': true,
}

// SplitShellArgs tokenizes a POSIX shell command string into argv tokens.
// Returns nil if the input has unterminated quotes or trailing escapes.
func SplitShellArgs(raw string) []string {
	var tokens []string
	var buf []byte
	inSingle := false
	inDouble := false
	escaped := false

	pushToken := func() {
		if len(buf) > 0 {
			tokens = append(tokens, string(buf))
			buf = buf[:0]
		}
	}

	for i := 0; i < len(raw); i++ {
		ch := raw[i]

		if escaped {
			buf = append(buf, ch)
			escaped = false
			continue
		}
		if !inSingle && !inDouble && ch == '\\' {
			escaped = true
			continue
		}
		if inSingle {
			if ch == '\'' {
				inSingle = false
			} else {
				buf = append(buf, ch)
			}
			continue
		}
		if inDouble {
			if ch == '\\' && i+1 < len(raw) && doubleQuoteEscapes[raw[i+1]] {
				buf = append(buf, raw[i+1])
				i++
				continue
			}
			if ch == '"' {
				inDouble = false
			} else {
				buf = append(buf, ch)
			}
			continue
		}
		if ch == '\'' {
			inSingle = true
			continue
		}
		if ch == '"' {
			inDouble = true
			continue
		}
		// In POSIX shells, "#" starts a comment only when it begins a word.
		if ch == '#' && len(buf) == 0 {
			break
		}
		if unicode.IsSpace(rune(ch)) {
			pushToken()
			continue
		}
		buf = append(buf, ch)
	}

	if escaped || inSingle || inDouble {
		return nil
	}
	pushToken()
	return tokens
}
