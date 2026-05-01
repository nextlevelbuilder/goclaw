package providers

import (
	"encoding/base64"
	"fmt"
	"regexp"
)

// dataURLRe matches data URLs of the form data:<mime>;base64,<b64data>.
var dataURLRe = regexp.MustCompile(`^data:([^;]+);base64,(.+)$`)

// parseDataURL extracts the MIME type and base64 payload from a data URL.
// It validates that the base64 string is decodable and returns it verbatim
// (not re-encoded) so callers can store it in ImageContent.Data as-is.
//
// Returns an error if:
//   - the URL does not match data:<mime>;base64,<b64> format
//   - the base64 payload cannot be decoded
func parseDataURL(s string) (mimeType string, b64Data string, err error) {
	m := dataURLRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", fmt.Errorf("invalid data URL format (expected data:<mime>;base64,<b64>)")
	}
	mimeType = m[1]
	b64Data = m[2]

	// Validate the base64 payload without retaining the decoded bytes.
	// Try standard encoding first, fall back to raw URL-safe encoding.
	if _, decErr := base64.StdEncoding.DecodeString(b64Data); decErr != nil {
		if _, decErr2 := base64.RawURLEncoding.DecodeString(b64Data); decErr2 != nil {
			return "", "", fmt.Errorf("base64 decode failed: %w", decErr)
		}
	}

	return mimeType, b64Data, nil
}
