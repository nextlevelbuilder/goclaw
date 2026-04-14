//go:build integration

// Package http_api tests HTTP API contracts (OpenAI-compatible endpoints).
package http_api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

var (
	testBaseURL string
	testToken   string
)

// getTestServer returns HTTP base URL and token from environment.
func getTestServer(t *testing.T) (baseURL string, token string) {
	t.Helper()

	baseURL = os.Getenv("CONTRACT_TEST_HTTP_URL")
	token = os.Getenv("CONTRACT_TEST_TOKEN")

	if baseURL == "" {
		t.Skip("CONTRACT_TEST_HTTP_URL not set - skipping contract test")
	}
	if token == "" {
		t.Skip("CONTRACT_TEST_TOKEN not set - skipping contract test")
	}

	testBaseURL = baseURL
	testToken = token
	return baseURL, token
}

// httpClient wraps HTTP client for contract testing.
type httpClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func newHTTPClient(baseURL, token string) *httpClient {
	return &httpClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *httpClient) post(t *testing.T, path string, body any) map[string]any {
	t.Helper()

	bodyJSON, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", c.baseURL+path, bytes.NewReader(bodyJSON))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		t.Fatalf("request failed: %d - %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	return result
}

func (c *httpClient) get(t *testing.T, path string) map[string]any {
	t.Helper()

	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		t.Fatalf("request failed: %d - %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	return result
}

// assertField verifies a field exists with expected type.
func assertField(t *testing.T, data map[string]any, path string, expectedType string) {
	t.Helper()

	parts := strings.Split(path, ".")
	current := data

	for i, part := range parts {
		if i == len(parts)-1 {
			val, ok := current[part]
			if !ok {
				t.Errorf("CONTRACT VIOLATION: missing field %q", path)
				return
			}
			actualType := typeOf(val)
			if actualType != expectedType {
				t.Errorf("CONTRACT VIOLATION: field %q type mismatch: got %s, want %s", path, actualType, expectedType)
			}
			return
		}
		nested, ok := current[part].(map[string]any)
		if !ok {
			t.Errorf("CONTRACT VIOLATION: field %q is not an object", strings.Join(parts[:i+1], "."))
			return
		}
		current = nested
	}
}

func typeOf(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}
