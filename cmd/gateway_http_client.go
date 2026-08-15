package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// gatewayHTTPError represents a structured error from the gateway HTTP API.
type gatewayHTTPError struct {
	StatusCode int
	Message    string
}

func (e *gatewayHTTPError) Error() string {
	return fmt.Sprintf("gateway error (%d): %s", e.StatusCode, e.Message)
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// healthClient has a shorter timeout for quick health checks.
var healthClient = &http.Client{Timeout: 3 * time.Second}

const gatewayHTTPResponseLimit = 1 << 20

// gatewayHTTPDo sends an HTTP request to the gateway with auth and returns the parsed JSON response.
func gatewayHTTPDo(method, path string, body any) (map[string]any, error) {
	raw, status, err := gatewayHTTPDoRaw(method, path, body)
	if err != nil {
		return nil, err
	}

	// DELETE with 204 No Content
	if status == http.StatusNoContent {
		return nil, nil
	}

	if status >= 400 {
		return nil, parseHTTPError(raw, status)
	}

	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("invalid JSON response from gateway: %s", string(raw))
	}

	return result, nil
}

// Convenience wrappers

func gatewayHTTPGet(path string) (map[string]any, error) {
	return gatewayHTTPDo(http.MethodGet, path, nil)
}

func gatewayHTTPPost(path string, body any) (map[string]any, error) {
	return gatewayHTTPDo(http.MethodPost, path, body)
}

func gatewayHTTPPut(path string, body any) (map[string]any, error) {
	return gatewayHTTPDo(http.MethodPut, path, body)
}

func gatewayHTTPPatch(path string, body any) (map[string]any, error) {
	return gatewayHTTPDo(http.MethodPatch, path, body)
}

func gatewayHTTPDelete(path string) error {
	_, err := gatewayHTTPDo(http.MethodDelete, path, nil)
	return err
}

// gatewayHTTPDoRaw executes an HTTP request and returns the raw response bytes.
// Shared by both map-based and typed response functions.
func gatewayHTTPDoRaw(method, path string, body any) ([]byte, int, error) {
	return gatewayHTTPDoRawWithLimit(method, path, body, gatewayHTTPResponseLimit)
}

func gatewayHTTPDoRawWithLimit(method, path string, body any, limit int64) ([]byte, int, error) {
	base := resolveGatewayBaseURL()

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, base+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GoClaw-User-Id", "system")
	if token := resolveGatewayToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("cannot reach gateway at %s: %w", base, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read gateway response: %w", err)
	}
	if int64(len(raw)) > limit {
		return nil, resp.StatusCode, fmt.Errorf("gateway response exceeds %d bytes", limit)
	}
	return raw, resp.StatusCode, nil
}

// parseHTTPError extracts an error message from a gateway error response.
func parseHTTPError(raw []byte, statusCode int) error {
	var errBody map[string]any
	if json.Unmarshal(raw, &errBody) == nil {
		if errVal, ok := errBody["error"]; ok {
			switch v := errVal.(type) {
			case string:
				return &gatewayHTTPError{StatusCode: statusCode, Message: v}
			case map[string]any:
				if m, ok := v["message"].(string); ok {
					return &gatewayHTTPError{StatusCode: statusCode, Message: m}
				}
			}
		}
	}
	return &gatewayHTTPError{StatusCode: statusCode, Message: string(raw)}
}

// gatewayHTTPGetTyped sends a GET request and unmarshals the response into the typed struct.
func gatewayHTTPGetTyped[T any](path string) (T, error) {
	var zero T
	raw, status, err := gatewayHTTPDoRaw(http.MethodGet, path, nil)
	if err != nil {
		return zero, err
	}
	if status >= 400 {
		return zero, parseHTTPError(raw, status)
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		return zero, fmt.Errorf("unmarshal response: %w", err)
	}
	return result, nil
}

// gatewayHTTPPostTyped sends a POST request and unmarshals the response into the typed struct.
func gatewayHTTPPostTyped[T any](path string, body any) (T, error) {
	var zero T
	raw, status, err := gatewayHTTPDoRaw(http.MethodPost, path, body)
	if err != nil {
		return zero, err
	}
	if status >= 400 {
		return zero, parseHTTPError(raw, status)
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		return zero, fmt.Errorf("unmarshal response: %w", err)
	}
	return result, nil
}

// requireRunningGatewayHTTP checks /health endpoint, exits with message if gateway is down.
func requireRunningGatewayHTTP() {
	base := resolveGatewayBaseURL()
	req, err := http.NewRequest(http.MethodGet, base+"/health", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: cannot build health check request.")
		os.Exit(1)
	}

	resp, err := healthClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: the gateway is not running.")
		fmt.Fprintf(os.Stderr, "Start it first:  goclaw\n")
		fmt.Fprintf(os.Stderr, "  (tried %s/health)\n", base)
		os.Exit(1)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: gateway health check returned %d.\n", resp.StatusCode)
		os.Exit(1)
	}
}
