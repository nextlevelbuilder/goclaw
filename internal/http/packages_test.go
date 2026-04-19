package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestParseAndValidatePackage tests input validation with table-driven approach.
func TestParseAndValidatePackage(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		wantEmpty     bool
		wantStatusErr int
	}{
		// Valid cases
		{
			name:      "simple alphanumeric",
			body:      `{"package":"github-cli"}`,
			wantEmpty: false,
		},
		{
			name:      "with hyphens",
			body:      `{"package":"my-package"}`,
			wantEmpty: false,
		},
		{
			name:      "with underscores",
			body:      `{"package":"my_package"}`,
			wantEmpty: false,
		},
		{
			name:      "with dots",
			body:      `{"package":"package.name"}`,
			wantEmpty: false,
		},
		{
			name:      "pip prefix",
			body:      `{"package":"pip:pandas"}`,
			wantEmpty: false,
		},
		{
			name:      "npm prefix",
			body:      `{"package":"npm:typescript"}`,
			wantEmpty: false,
		},
		{
			name:      "scoped npm package",
			body:      `{"package":"npm:@scope/package"}`,
			wantEmpty: false,
		},
		{
			name:      "with plus sign",
			body:      `{"package":"c++"}`,
			wantEmpty: false,
		},
		{
			name:      "numbers in name",
			body:      `{"package":"python3"}`,
			wantEmpty: false,
		},
		// Invalid cases
		{
			name:          "empty package field",
			body:          `{"package":""}`,
			wantEmpty:     true,
			wantStatusErr: http.StatusBadRequest,
		},
		{
			name:          "missing package field",
			body:          `{}`,
			wantEmpty:     true,
			wantStatusErr: http.StatusBadRequest,
		},
		{
			name:          "malformed json",
			body:          `{invalid json}`,
			wantEmpty:     true,
			wantStatusErr: http.StatusBadRequest,
		},
		{
			name:          "starts with hyphen (injection risk)",
			body:          `{"package":"-malicious"}`,
			wantEmpty:     true,
			wantStatusErr: http.StatusBadRequest,
		},
		{
			name:          "contains semicolon (shell injection)",
			body:          `{"package":"pkg; rm -rf"}`,
			wantEmpty:     true,
			wantStatusErr: http.StatusBadRequest,
		},
		{
			name:          "contains space",
			body:          `{"package":"pkg name"}`,
			wantEmpty:     true,
			wantStatusErr: http.StatusBadRequest,
		},
		{
			name:          "contains pipe (shell)",
			body:          `{"package":"pkg|cat"}`,
			wantEmpty:     true,
			wantStatusErr: http.StatusBadRequest,
		},
		{
			name:          "contains backtick (command injection)",
			body:          "{\"package\":\"pkg`command`\"}",
			wantEmpty:     true,
			wantStatusErr: http.StatusBadRequest,
		},
		{
			name:          "index-url prefix (pip attack)",
			body:          `{"package":"--index-url=evil"}`,
			wantEmpty:     true,
			wantStatusErr: http.StatusBadRequest,
		},
		{
			name:      "starts with @",
			body:      `{"package":"@scope/pkg"}`,
			wantEmpty: false,
		},
		// github: scheme — full-spec form (install + uninstall)
		{
			name:      "github full spec",
			body:      `{"package":"github:cli/cli"}`,
			wantEmpty: false,
		},
		{
			name:      "github full spec with tag",
			body:      `{"package":"github:cli/cli@v2.45.0"}`,
			wantEmpty: false,
		},
		// github: scheme — bare-name form (uninstall path: UI sends github:${pkg.name})
		{
			name:      "github bare name (uninstall path)",
			body:      `{"package":"github:gh"}`,
			wantEmpty: false,
		},
		{
			name:      "github bare name with dots/hyphens",
			body:      `{"package":"github:ripgrep-13.0"}`,
			wantEmpty: false,
		},
		// github: scheme — rejected forms (injection vectors)
		{
			name:          "github empty after prefix",
			body:          `{"package":"github:"}`,
			wantEmpty:     true,
			wantStatusErr: http.StatusBadRequest,
		},
		{
			name:          "github traversal",
			body:          `{"package":"github:../evil"}`,
			wantEmpty:     true,
			wantStatusErr: http.StatusBadRequest,
		},
		{
			name:          "github with shell injection",
			body:          `{"package":"github:evil;rm -rf"}`,
			wantEmpty:     true,
			wantStatusErr: http.StatusBadRequest,
		},
		{
			name:          "github with space",
			body:          `{"package":"github:evil tool"}`,
			wantEmpty:     true,
			wantStatusErr: http.StatusBadRequest,
		},
		{
			name:          "github starts with hyphen",
			body:          `{"package":"github:-flag"}`,
			wantEmpty:     true,
			wantStatusErr: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/packages/install", bytes.NewBufferString(tt.body))
			w := httptest.NewRecorder()

			result := parseAndValidatePackage(w, req)

			if tt.wantEmpty && result != "" {
				t.Errorf("parseAndValidatePackage() = %q, want empty", result)
			}
			if !tt.wantEmpty && result == "" {
				t.Errorf("parseAndValidatePackage() = empty, want non-empty")
			}
			if tt.wantStatusErr > 0 && w.Code != tt.wantStatusErr {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatusErr)
			}
		})
	}
}

// TestParseAndValidatePackage_BodySizeLimit tests the MaxBytesReader limit.
func TestParseAndValidatePackage_BodySizeLimit(t *testing.T) {
	// Create a body larger than 4096 bytes
	largeBody := `{"package":"` + string(bytes.Repeat([]byte("x"), 5000)) + `"}`
	req := httptest.NewRequest("POST", "/v1/packages/install", bytes.NewBufferString(largeBody))
	w := httptest.NewRecorder()

	result := parseAndValidatePackage(w, req)

	if result != "" {
		t.Errorf("parseAndValidatePackage() with large body = %q, want empty (rejected)", result)
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (request entity too large)", w.Code, http.StatusBadRequest)
	}
}

// TestNewPackagesHandler creates a handler.
func TestNewPackagesHandler(t *testing.T) {
	h := NewPackagesHandler()
	if h == nil {
		t.Fatal("NewPackagesHandler() returned nil")
	}
}

// TestPackagesHandler_RegisterRoutes ensures routes are registered without panic.
func TestPackagesHandler_RegisterRoutes(t *testing.T) {
	h := NewPackagesHandler()
	mux := http.NewServeMux()

	// Should not panic
	h.RegisterRoutes(mux)
}

// TestParseAndValidatePackage_StripsPrefixesCorrectly tests that prefixes are stripped for validation.
func TestParseAndValidatePackage_StripsPrefixesCorrectly(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantFail bool
	}{
		{
			name:     "pip: with valid name",
			input:    `{"package":"pip:pandas"}`,
			wantFail: false,
		},
		{
			name:     "npm: with valid name",
			input:    `{"package":"npm:typescript"}`,
			wantFail: false,
		},
		{
			name:     "pip: with invalid name after prefix",
			input:    `{"package":"pip:-badname"}`,
			wantFail: true,
		},
		{
			name:     "npm: with invalid name after prefix",
			input:    `{"package":"npm:-badname"}`,
			wantFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/packages/install", bytes.NewBufferString(tt.input))
			w := httptest.NewRecorder()

			result := parseAndValidatePackage(w, req)

			isEmpty := result == ""
			if isEmpty != tt.wantFail {
				t.Errorf("empty = %v, wantFail = %v", isEmpty, tt.wantFail)
			}
		})
	}
}

// TestParseAndValidatePackage_ReturnsOriginalPackageString tests that returned string includes prefix.
func TestParseAndValidatePackage_ReturnsOriginalPackageString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`{"package":"github-cli"}`, "github-cli"},
		{`{"package":"pip:pandas"}`, "pip:pandas"},
		{`{"package":"npm:typescript"}`, "npm:typescript"},
		{`{"package":"npm:@scope/pkg"}`, "npm:@scope/pkg"},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("POST", "/v1/packages/install", bytes.NewBufferString(tt.input))
		w := httptest.NewRecorder()

		result := parseAndValidatePackage(w, req)

		if result != tt.want {
			t.Errorf("parseAndValidatePackage() = %q, want %q", result, tt.want)
		}
	}
}

// TestValidPkgNameRegex tests the regex directly.
func TestValidPkgNameRegex(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		// Valid
		{"github-cli", true},
		{"my_package", true},
		{"package.name", true},
		{"python3", true},
		{"c++", true},
		{"@scope/pkg", true},
		{"pkg123", true},
		{"a", true},
		{"A", true},
		{"0abc", true}, // can start with number
		// Invalid
		{"-invalid", false},      // starts with hyphen
		{"--flag", false},        // starts with hyphen
		{"pkg name", false},      // contains space
		{"pkg;cmd", false},       // contains semicolon
		{"pkg|cmd", false},       // contains pipe
		{"pkg&cmd", false},       // contains ampersand
		{"pkg`cmd`", false},      // contains backtick
		{"pkg$var", false},       // contains dollar sign
		{"pkg<file", false},      // contains angle bracket
		{"pkg>file", false},      // contains angle bracket
		{"", false},              // empty
	}

	for _, tt := range tests {
		got := validPkgName.MatchString(tt.name)
		if got != tt.valid {
			t.Errorf("validPkgName.MatchString(%q) = %v, want %v", tt.name, got, tt.valid)
		}
	}
}

// TestParseAndValidatePackage_ErrorResponseFormat tests error response JSON format.
func TestParseAndValidatePackage_ErrorResponseFormat(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/packages/install", bytes.NewBufferString(`{"package":""}`))
	w := httptest.NewRecorder()

	parseAndValidatePackage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if errMsg, ok := errResp["error"]; !ok || errMsg == "" {
		t.Error("response missing 'error' field")
	}
}
