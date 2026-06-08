package bitrix24

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// captureRT is a RoundTripper that records requests and returns canned JSON responses.
// It sidesteps HTTPS/TLS and host issues by intercepting at the transport layer,
// making it ideal for unit tests that need to verify request parameters without
// running a real server or setting up complex Portal bindings.
type captureRT struct {
	reqs   []url.Values        // parsed form bodies, in order
	paths  []string            // r.URL.Path in order
	result string              // JSON to return as the response body; default: {"result":{"fileId":1}}
	status int
}

// RoundTrip implements http.RoundTripper, capturing the request and returning a canned response.
func (rt *captureRT) RoundTrip(r *http.Request) (*http.Response, error) {
	// Capture the request body
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(b))
		rt.reqs = append(rt.reqs, form)
		// Restore body for any potential re-read (though typically not used)
		r.Body = io.NopCloser(strings.NewReader(string(b)))
	} else {
		rt.reqs = append(rt.reqs, url.Values{})
	}

	// Capture the path
	rt.paths = append(rt.paths, r.URL.Path)

	// Prepare response
	status := rt.status
	if status == 0 {
		status = http.StatusOK
	}
	body := rt.result
	if body == "" {
		body = `{"result":{"fileId":1}}`
	}

	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header: http.Header{
			"Content-Type": {"application/json"},
		},
	}, nil
}

// newStubClient creates a Client with a fake Portal so AccessToken() returns
// without network. The Portal is bound with an in-memory valid token that will
// pass the time.Until(expiry) > expiryBuffer check.
func newStubClient(domain string, rt http.RoundTripper) *Client {
	httpClient := &http.Client{Transport: rt}
	c := NewClient(domain, httpClient)

	// Manually set a Portal with a valid, non-expired token.
	// This mimics what Portal.bindClient() would do in production.
	c.SetPortal(&Portal{
		state: store.BitrixPortalState{
			AccessToken:  "test-token",
			RefreshToken: "test-refresh",
			ExpiresAt:    time.Now().Add(time.Hour), // Valid for 1 hour
		},
	})

	return c
}

// captureRTPartialFail is a RoundTripper that succeeds on first request and fails on second.
// Used to test partial upload failure scenarios.
type captureRTPartialFail struct {
	reqs  []url.Values
	paths []string
	calls int
}

// RoundTrip implements http.RoundTripper, returning success on first call and error on second.
func (rt *captureRTPartialFail) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.calls++

	// Capture request
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(b))
		rt.reqs = append(rt.reqs, form)
		r.Body = io.NopCloser(strings.NewReader(string(b)))
	} else {
		rt.reqs = append(rt.reqs, url.Values{})
	}
	rt.paths = append(rt.paths, r.URL.Path)

	// Return different responses based on call count
	var body string
	if rt.calls == 1 {
		body = `{"result":{"fileId":1}}`
	} else {
		body = `{"error":"UPLOAD_FAILED"}`
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header: http.Header{
			"Content-Type": {"application/json"},
		},
	}, nil
}
