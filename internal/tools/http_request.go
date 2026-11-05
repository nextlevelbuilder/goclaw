package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	httpRequestMaxBodyBytes     = 1 << 20  // request body we will send
	httpRequestMaxReadBytes     = 1 << 20  // response bytes read and redacted
	httpRequestMaxOutputChars   = 64 << 10 // response characters returned
	httpRequestDefaultTimeout   = 30 * time.Second
	httpRequestMaxTimeout       = 60 * time.Second
	httpRequestMaxRedirects     = 5
	httpRequestMaxHeaders       = 40
	httpRequestPlaceholderField = "{{secret:"
)

var httpRequestMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// Headers the transport owns, or that would let a caller smuggle a request.
var httpRequestBlockedHeaders = map[string]bool{
	"host": true, "content-length": true, "transfer-encoding": true, "connection": true,
	"upgrade": true, "te": true, "trailer": true, "proxy-connection": true,
}

// Credentials-bearing headers that a literal value must not carry across hosts.
var httpRequestCredentialHeaders = map[string]bool{"authorization": true, "cookie": true, "proxy-authorization": true}

// Response headers never shown to the model.
var httpRequestHiddenResponseHeaders = map[string]bool{
	"set-cookie": true, "set-cookie2": true, "authorization": true, "proxy-authorization": true,
}

// HTTPRequestTool is a general HTTP client for agents. It understands
// {{secret:name}} placeholders in the URL path/query, headers and body, sends
// each secret only to hosts in its allowed_domains, and always connects to the
// IP it validated (no SSRF, no DNS rebinding).
type HTTPRequestTool struct {
	client *http.Client
}

func NewHTTPRequestTool() *HTTPRequestTool {
	return &HTTPRequestTool{client: security.NewSafeClient(httpRequestMaxTimeout)}
}

// SecretKinds implements secretvault.SecretTarget.
func (t *HTTPRequestTool) SecretKinds() []string { return []string{store.SecretKindAPIKey} }

func (t *HTTPRequestTool) Name() string { return "http_request" }

func (t *HTTPRequestTool) Description() string {
	return "Make an HTTP request (GET, POST, PUT, PATCH, DELETE) to a public API. " +
		"To authenticate, put a vault placeholder such as {{secret:github-token}} in a header, the body, or the URL path/query " +
		"(for example headers {\"Authorization\": \"Bearer {{secret:github-token}}\"}); the real value is substituted at send time " +
		"and only for hosts the secret is allowed for. Placeholders are never allowed in the host. " +
		"Use request_secret first if you do not have the credential."
}

func (t *HTTPRequestTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"method":  map[string]any{"type": "string", "enum": httpRequestMethods, "description": "HTTP method (default GET)."},
			"url":     map[string]any{"type": "string", "description": "https URL. Placeholders allowed in path/query, not in the host."},
			"headers": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Request headers."},
			"body": map[string]any{
				"description": "Request body: a string, or a JSON object/array (sent as JSON). Placeholders allowed.",
			},
			"timeout": map[string]any{"type": "number", "description": "Timeout in seconds (default 30, max 60)."},
		},
		"required": []string{"url"},
	}
}

// httpRequestAllow lists where a placeholder may appear.
func httpRequestAllow(path string) bool {
	return path == "url" || path == "body" || strings.HasPrefix(path, "body.") || strings.HasPrefix(path, "body[") ||
		strings.HasPrefix(path, "headers.")
}

type httpRequestSpec struct {
	method  string
	rawURL  string // as given, may hold placeholders in path/query
	headers map[string]any
	body    any
	timeout time.Duration
}

func (t *HTTPRequestTool) Execute(ctx context.Context, args map[string]any) *Result {
	spec, err := parseHTTPRequestArgs(args)
	if err != nil {
		return ErrorResult("Invalid request: " + err.Error())
	}
	if strings.Contains(urlAuthority(spec.rawURL), httpRequestPlaceholderField) {
		return ErrorResult("Invalid request: a secret placeholder is not allowed in the host")
	}
	u, err := url.Parse(spec.rawURL)
	if err != nil || u.Hostname() == "" {
		return ErrorResult("Invalid request: url is not valid")
	}
	hasSecret := secretvault.ContainsPlaceholder(map[string]any{"url": spec.rawURL, "headers": spec.headers, "body": spec.body})
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && !hasSecret:
	default:
		return ErrorResult("Invalid request: only https URLs are allowed when the request uses a secret")
	}

	ctx, cancel := context.WithTimeout(ctx, spec.timeout)
	defer cancel()

	resp, finalURL, err := t.send(ctx, spec, u, hasSecret)
	if err != nil {
		return ErrorResult(redactMessage(ctx, "Request failed: "+err.Error()))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, httpRequestMaxReadBytes+1))
	if err != nil {
		return ErrorResult(redactMessage(ctx, "Reading the response failed: "+err.Error()))
	}
	readTruncated := len(raw) > httpRequestMaxReadBytes
	if readTruncated {
		raw = raw[:httpRequestMaxReadBytes]
	}
	return NewResult(formatHTTPResponse(ctx, resp, finalURL, raw, readTruncated))
}

func parseHTTPRequestArgs(args map[string]any) (httpRequestSpec, error) {
	spec := httpRequestSpec{method: "GET", timeout: httpRequestDefaultTimeout}
	if m, _ := args["method"].(string); m != "" {
		spec.method = strings.ToUpper(strings.TrimSpace(m))
	}
	if !slices.Contains(httpRequestMethods, spec.method) {
		return spec, fmt.Errorf("method %q is not allowed (use %s)", spec.method, strings.Join(httpRequestMethods, ", "))
	}
	spec.rawURL, _ = args["url"].(string)
	spec.rawURL = strings.TrimSpace(spec.rawURL)
	if spec.rawURL == "" || !strings.Contains(spec.rawURL, "//") {
		return spec, errors.New("url is required")
	}
	switch h := args["headers"].(type) {
	case nil:
	case map[string]any:
		spec.headers = h
	case map[string]string:
		spec.headers = make(map[string]any, len(h))
		for k, v := range h {
			spec.headers[k] = v
		}
	default:
		return spec, errors.New("headers must be an object of strings")
	}
	if len(spec.headers) > httpRequestMaxHeaders {
		return spec, fmt.Errorf("at most %d headers", httpRequestMaxHeaders)
	}
	for k, v := range spec.headers {
		if _, ok := v.(string); !ok {
			return spec, fmt.Errorf("header %q must be a string", k)
		}
		if httpRequestBlockedHeaders[strings.ToLower(k)] || strings.ContainsAny(k, " :\r\n") {
			return spec, fmt.Errorf("header %q is not allowed", k)
		}
	}
	spec.body = args["body"]
	if secs, ok := args["timeout"].(float64); ok && secs > 0 {
		spec.timeout = min(time.Duration(secs*float64(time.Second)), httpRequestMaxTimeout)
	}
	return spec, nil
}

// send performs the request, following redirects one hop at a time. Every hop
// is validated against SSRF rules and dialed on the validated IP. When the
// request carries secrets, each hop re-resolves them for the new host, so a
// redirect to a host outside a secret's allowed_domains fails before any value
// is sent there.
func (t *HTTPRequestTool) send(ctx context.Context, spec httpRequestSpec, first *url.URL, hasSecret bool) (*http.Response, string, error) {
	cur, curRaw := first, spec.rawURL // hop 0 keeps the raw text: url.String would mangle placeholders
	method, body := spec.method, spec.body
	for hop := 0; ; hop++ {
		resolved := map[string]any{"url": curRaw, "headers": spec.headers, "body": body}
		if hasSecret {
			var err error
			resolved, err = secretvault.ResolveArgs(ctx, resolved, secretvault.ArgOptions{
				Kind: store.SecretKindAPIKey, Host: cur.Hostname(), Allow: httpRequestAllow,
			})
			if err != nil {
				return nil, "", err
			}
		}

		target, pinned, err := security.Validate(resolved["url"].(string))
		if err != nil {
			return nil, "", err
		}
		payload, contentType, err := encodeHTTPBody(resolved["body"])
		if err != nil {
			return nil, "", err
		}
		if method == "GET" && len(payload) > 0 {
			return nil, "", errors.New("a GET request cannot have a body")
		}

		req, err := http.NewRequestWithContext(security.WithPinnedIP(ctx, pinned), method, target.String(), bytes.NewReader(payload))
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("User-Agent", "GoClaw-HTTP/1.0")
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		hdrs, _ := resolved["headers"].(map[string]any)
		for k, v := range hdrs {
			if s, ok := v.(string); ok {
				if hop > 0 && httpRequestCredentialHeaders[strings.ToLower(k)] && !headerHasPlaceholder(spec.headers[k]) && target.Hostname() != first.Hostname() {
					continue // a literal credential stays on the host it was written for
				}
				req.Header.Set(k, s)
			}
		}

		resp, err := t.client.Do(req)
		if err != nil {
			return nil, "", err
		}
		if resp.StatusCode < 300 || resp.StatusCode > 399 || resp.StatusCode == 304 {
			return resp, target.String(), nil
		}
		loc := resp.Header.Get("Location")
		resp.Body.Close()
		if loc == "" {
			return resp, target.String(), nil
		}
		if hop >= httpRequestMaxRedirects {
			return nil, "", fmt.Errorf("stopped after %d redirects", httpRequestMaxRedirects)
		}
		next, err := target.Parse(loc)
		if err != nil {
			return nil, "", errors.New("redirect target is not a valid URL")
		}
		if next.Scheme != "https" && (hasSecret || next.Scheme != "http") {
			return nil, "", errors.New("redirect to a non-https URL is not allowed")
		}
		if resp.StatusCode != http.StatusTemporaryRedirect && resp.StatusCode != http.StatusPermanentRedirect {
			method, body = "GET", nil
		}
		cur, curRaw = next, next.String()
	}
}

func headerHasPlaceholder(v any) bool { return secretvault.ContainsPlaceholder(v) }

// encodeHTTPBody turns the (already resolved) body into bytes.
func encodeHTTPBody(body any) ([]byte, string, error) {
	var (
		b  []byte
		ct string
	)
	switch v := body.(type) {
	case nil:
		return nil, "", nil
	case string:
		b = []byte(v)
	default:
		var err error
		if b, err = json.Marshal(v); err != nil {
			return nil, "", errors.New("body is not valid JSON")
		}
		ct = "application/json"
	}
	if len(b) > httpRequestMaxBodyBytes {
		return nil, "", fmt.Errorf("request body exceeds %d bytes", httpRequestMaxBodyBytes)
	}
	return b, ct, nil
}

// redactMessage removes secret values from error text before it is returned.
func redactMessage(ctx context.Context, s string) string {
	if rt := secretvault.RuntimeFrom(ctx); rt != nil {
		return rt.Redactor.Redact(s)
	}
	return s
}

// formatHTTPResponse builds the model-visible result. The whole bytes read are
// redacted first and only then cut, so a secret straddling the limit cannot
// slip through half-masked.
func formatHTTPResponse(ctx context.Context, resp *http.Response, finalURL string, raw []byte, readTruncated bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP %s\nURL: %s\n", resp.Status, redactMessage(ctx, finalURL))

	names := make([]string, 0, len(resp.Header))
	for k := range resp.Header {
		if !httpRequestHiddenResponseHeaders[strings.ToLower(k)] {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Fprintf(&b, "%s: %s\n", k, redactMessage(ctx, strings.Join(resp.Header.Values(k), ", ")))
	}
	b.WriteString("\n")

	if !utf8.Valid(raw) {
		fmt.Fprintf(&b, "[binary response, %d bytes not shown]", len(raw))
		return b.String()
	}
	text := redactMessage(ctx, string(raw))
	cut := false
	if r := []rune(text); len(r) > httpRequestMaxOutputChars {
		text, cut = string(r[:httpRequestMaxOutputChars]), true
	}
	b.WriteString(text)
	if cut || readTruncated {
		b.WriteString("\n[response truncated]")
	}
	return b.String()
}

// urlAuthority returns the text between "//" and the first "/", "?" or "#".
func urlAuthority(raw string) string {
	_, rest, ok := strings.Cut(raw, "//")
	if !ok {
		return ""
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		return rest[:i]
	}
	return rest
}
