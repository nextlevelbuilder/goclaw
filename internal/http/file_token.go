package http

import (
	"crypto/hmac"
	crypto_rand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FileTokenTTL is the default TTL for signed file tokens.
const FileTokenTTL = 5 * time.Minute

var (
	fileSigningKey     string
	fileSigningKeyOnce sync.Once
)

// FileSigningKey returns a random 32-byte key for HMAC file token signing.
// Generated once at startup, lives in memory only. Tokens expire on restart
// which is acceptable for the short TTL — clients re-fetch signed URLs on reconnect.
func FileSigningKey() string {
	fileSigningKeyOnce.Do(func() {
		b := make([]byte, 32)
		crypto_rand.Read(b)
		fileSigningKey = base64.RawURLEncoding.EncodeToString(b)
	})
	return fileSigningKey
}

// SignFileToken creates a short-lived HMAC token for file access.
// Token format: {base64url_hmac_32bytes}.{unix_expiry}.
// The path is bound into the signature so tokens can't be reused for other files.
func SignFileToken(path, secret string, ttl time.Duration) string {
	expiry := time.Now().Add(ttl).Unix()
	sig := fileTokenHMAC(path, secret, expiry)
	return fmt.Sprintf("%s.%d", sig, expiry)
}

// VerifyFileToken validates a signed file token against a path and secret.
// Returns true if the HMAC matches and the token has not expired.
func VerifyFileToken(token, path, secret string) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}
	expiry, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return false
	}
	expected := fileTokenHMAC(path, secret, expiry)
	return hmac.Equal([]byte(parts[0]), []byte(expected))
}

// fileTokenHMAC computes the HMAC signature component.
func fileTokenHMAC(path, secret string, expiry int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(fmt.Appendf(nil, "%s:%d", path, expiry))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// SignMediaPath converts a media ref path to a signed /v1/files/ URL.
// Handles legacy data where paths may already contain /v1/files/ prefixes
// and stale ?ft= tokens from prior signing bugs.
func SignMediaPath(rawPath, secret string) string {
	if rawPath == "" {
		return ""
	}
	// Defense-in-depth: reject path traversal (also blocked by handleServe)
	if strings.Contains(rawPath, "..") {
		return ""
	}
	// Strip stale ?ft= tokens
	cleanPath := filepathToURLPath(rawPath)
	cleanPath = staleTokenRe.ReplaceAllString(cleanPath, "")
	cleanPath = strings.TrimRight(cleanPath, "?&")
	// Strip all /v1/files/ and /v1/media/ prefixes (may be stacked from legacy bugs)
	for strings.Contains(cleanPath, "/v1/files/") {
		cleanPath = strings.Replace(cleanPath, "/v1/files/", "/", 1)
	}
	for strings.Contains(cleanPath, "/v1/media/") {
		cleanPath = strings.Replace(cleanPath, "/v1/media/", "/", 1)
	}
	cleanPath = path.Clean(cleanPath)
	urlPath := "/v1/files/" + strings.TrimPrefix(cleanPath, "/")
	ft := SignFileToken(urlPath, secret, FileTokenTTL)
	return escapedFileURL(urlPath) + "?ft=" + ft
}

func filepathToURLPath(rawPath string) string {
	return strings.ReplaceAll(rawPath, `\`, "/")
}

// fileURLRe matches /v1/files/... and /v1/media/... URLs in markdown and plain text.
// Captures the full URL path (stops at whitespace, closing paren, quote, or angle bracket).
var fileURLRe = regexp.MustCompile(`(/v1/(?:files|media)/[^\s)"'<>]+)`)

// SignFileURLs finds all /v1/files/ and /v1/media/ URLs in content and appends
// a signed ?ft= token. Used at delivery time (WS events, HTTP responses) to avoid
// persisting tokens in session messages. Also heals legacy data issues:
//   - Strips stale ?ft= tokens and re-signs with fresh tokens
//   - Fixes double /v1/files/v1/files/ prefix from old media mutation bug
//   - Cleans ?ft= from markdown link display text [name?ft=xxx](url) → [name](url)
func SignFileURLs(content, secret string) string {
	if secret == "" || !strings.Contains(content, "/v1/") {
		return content
	}
	// Heal legacy: deduplicate /v1/files/v1/files/ → /v1/files/ and /v1/media/v1/media/ → /v1/media/
	content = strings.ReplaceAll(content, "/v1/files/v1/files/", "/v1/files/")
	content = strings.ReplaceAll(content, "/v1/files/v1/media/", "/v1/media/")
	content = strings.ReplaceAll(content, "/v1/media/v1/media/", "/v1/media/")
	// Heal legacy: convert bare absolute paths in markdown links to /v1/files/ URLs.
	// Agent text may embed ![img](/app/workspace/...) — these need the /v1/files/ prefix to be served.
	content = barePathInLinkRe.ReplaceAllString(content, "](/v1/files$1)")
	// Heal legacy: clean ?ft=... from markdown link display text [name?ft=xxx](...) → [name](...)
	content = linkTextFtRe.ReplaceAllString(content, "[$1]")
	// Encode spaces inside markdown link targets so fileURLRe can match the full
	// path (Windows user dirs like "Bruce Le" used to be truncated at the space).
	content = encodeSpacesInLinkTargets(content)
	// Sign URLs
	return fileURLRe.ReplaceAllStringFunc(content, func(rawURL string) string {
		// Strip stale ft= token if present (legacy data may have persisted tokens).
		cleanURL := stripFileToken(rawURL)
		// The signature must be computed over the same canonical form that
		// handleServe rebuilds from r.PathValue("path"): separators normalized
		// to '/' and percent-decoded (net/http decodes before the handler runs).
		signPath := filepathToURLPath(cleanURL)
		if decoded, err := url.PathUnescape(signPath); err == nil {
			signPath = decoded
		}
		ft := SignFileToken(signPath, secret, FileTokenTTL)
		sep := "?"
		if strings.Contains(cleanURL, "?") {
			sep = "&"
		}
		return cleanURL + sep + "ft=" + ft
	})
}

// mdLinkTargetRe matches any markdown link target "]...)". It is used only to
// percent-encode spaces inside targets that reference the files API, so the
// stricter fileURLRe (which stops at whitespace) can match the complete path.
var mdLinkTargetRe = regexp.MustCompile(`\]\(([^)\n]+)\)`)

// encodeSpacesInLinkTargets percent-encodes spaces inside markdown link targets
// that point at the files API. Chat links are emitted as full URLs
// (http://127.0.0.1:18800/v1/files/C:\Users\Bruce Le\...) and fileURLRe stops at
// whitespace, so without this pass only the prefix up to the first space gets a
// signature and the download ends in 401 unauthorized.
func encodeSpacesInLinkTargets(content string) string {
	if !strings.Contains(content, " ") || !strings.Contains(content, "/v1/") {
		return content
	}
	return mdLinkTargetRe.ReplaceAllStringFunc(content, func(m string) string {
		if !strings.Contains(m, " ") || !strings.Contains(m, "/v1/") {
			return m
		}
		inner := m[2 : len(m)-1] // strip "](" and trailing ")"
		return "](" + strings.ReplaceAll(inner, " ", "%20") + ")"
	})
}

// linkTextFtRe matches markdown link text containing ?ft=... e.g. [filename.md?ft=xxx](...)
// Captures the clean name before ?ft= for replacement.
var linkTextFtRe = regexp.MustCompile(`\[([^\]\s?]+)\?ft=[^\]]*\]`)

// barePathInLinkRe matches markdown link URLs that are bare absolute paths (not /v1/ URLs).
// Captures the absolute path so we can prepend /v1/files. Only matches inside ](...)
// to avoid false positives in prose text.
var barePathInLinkRe = regexp.MustCompile(`\]\((/(?:app|home|tmp|opt|data|workspace)[^\s)"'<>]*)\)`)

// staleTokenRe matches ?ft=... or &ft=... query parameter (greedy to end of URL segment).
var staleTokenRe = regexp.MustCompile(`[?&]ft=[^\s)"'<>&]*`)

// stripFileToken removes any ft= query parameter from a URL, cleaning up
// legacy session data that may have persisted signed tokens.
func stripFileToken(url string) string {
	cleaned := staleTokenRe.ReplaceAllString(url, "")
	// If we stripped ?ft=... (the only param), a trailing "?" might remain — remove it.
	cleaned = strings.TrimRight(cleaned, "?&")
	return cleaned
}
