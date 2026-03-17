package main

import (
	"encoding/json"
	"testing"
)

// TestHandleRequest tests the request validation logic.
// Note: Command execution tests are not included here since apk is not available
// in unit test environments. Integration tests would handle actual execution.
func TestHandleRequest(t *testing.T) {
	tests := []struct {
		name         string
		req          request
		wantValidErr bool
		errContains  string
	}{
		// Validation error cases
		{
			name:         "missing package",
			req:          request{Action: "install", Package: ""},
			wantValidErr: true,
			errContains:  "package required",
		},
		{
			name:         "invalid package name (starts with hyphen)",
			req:          request{Action: "install", Package: "-malicious"},
			wantValidErr: true,
			errContains:  "invalid package name",
		},
		{
			name:         "invalid package name (contains semicolon)",
			req:          request{Action: "install", Package: "pkg; rm -rf"},
			wantValidErr: true,
			errContains:  "invalid package name",
		},
		{
			name:         "invalid package name (contains space)",
			req:          request{Action: "install", Package: "pkg name"},
			wantValidErr: true,
			errContains:  "invalid package name",
		},
		{
			name:         "unknown action",
			req:          request{Action: "unknown", Package: "curl"},
			wantValidErr: true,
			errContains:  "unknown action",
		},
		// Validation pass cases (may fail at exec stage, but validation passes)
		{
			name:         "valid install",
			req:          request{Action: "install", Package: "curl"},
			wantValidErr: false,
			errContains:  "", // Validation passed (no validation error)
		},
		{
			name:         "valid uninstall",
			req:          request{Action: "uninstall", Package: "git"},
			wantValidErr: false,
			errContains:  "",
		},
		{
			name:         "valid scoped npm package",
			req:          request{Action: "install", Package: "@scope/pkg"},
			wantValidErr: false,
			errContains:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := handleRequest(tt.req)

			hasValidationErr := contains(resp.Error, "package required") ||
				contains(resp.Error, "invalid package name") ||
				contains(resp.Error, "unknown action")

			if hasValidationErr != tt.wantValidErr {
				t.Errorf("validation error = %v, want %v (error: %q)", hasValidationErr, tt.wantValidErr, resp.Error)
			}

			if tt.wantValidErr && tt.errContains != "" && !contains(resp.Error, tt.errContains) {
				t.Errorf("error = %q, want to contain %q", resp.Error, tt.errContains)
			}
		})
	}
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestValidPkgName tests the package name validation regex.
func TestValidPkgName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		// Valid package names
		{"github-cli", true},
		{"curl", true},
		{"python3", true},
		{"my_package", true},
		{"package.name", true},
		{"c++", true},
		{"@scope/pkg", true},
		{"pkg123", true},
		{"a", true},
		{"A", true},
		{"0abc", true}, // can start with number
		{"abc123def", true},
		{"pkg-with-hyphens", true},
		{"pkg_with_underscores", true},
		{"pkg.with.dots", true},
		// Invalid package names
		{"-invalid", false},           // starts with hyphen
		{"--flag", false},             // starts with hyphen
		{"pkg name", false},           // contains space
		{"pkg;cmd", false},            // contains semicolon
		{"pkg|cmd", false},            // contains pipe
		{"pkg&cmd", false},            // contains ampersand
		{"pkg`cmd`", false},           // contains backtick
		{"pkg$var", false},            // contains dollar sign
		{"pkg<file", false},           // contains angle bracket
		{"pkg>file", false},           // contains angle bracket
		{"pkg'quote", false},          // contains quote
		{"pkg\"quote", false},         // contains quote
		{"pkg(paren)", false},         // contains parens
		{"", false},                   // empty
		{" curl", false},              // starts with space
		{"curl ", false},              // ends with space
		{"--index-url=evil", false},   // flag pattern
		{"-u", false},                 // short flag
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validPkgName.MatchString(tt.name)
			if got != tt.valid {
				t.Errorf("validPkgName.MatchString(%q) = %v, want %v", tt.name, got, tt.valid)
			}
		})
	}
}

// TestHandleRequest_AllActionsValidated tests both install and uninstall actions.
func TestHandleRequest_AllActionsValidated(t *testing.T) {
	tests := []struct {
		action string
	}{
		{"install"},
		{"uninstall"},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			resp := handleRequest(request{
				Action:  tt.action,
				Package: "valid-package",
			})

			// Validation should pass (no "invalid package" or "package required" error)
			if contains(resp.Error, "package required") || contains(resp.Error, "invalid package name") {
				t.Errorf("action %q validation failed: %q", tt.action, resp.Error)
			}
		})
	}
}

// TestHandleRequest_EmptyActionFails tests that empty action is rejected.
func TestHandleRequest_EmptyActionFails(t *testing.T) {
	resp := handleRequest(request{
		Action:  "",
		Package: "curl",
	})

	if resp.Error == "" {
		t.Error("empty action should fail validation")
	}
	if !contains(resp.Error, "unknown action") {
		t.Errorf("error = %q, want to contain 'unknown action'", resp.Error)
	}
}

// TestHandleRequest_PackageValidationCatchesInjection tests shell injection attempts.
func TestHandleRequest_PackageValidationCatchesInjection(t *testing.T) {
	injections := []string{
		"-malicious",
		"pkg; rm -rf /",
		"pkg && evil",
		"pkg || evil",
		"pkg | evil",
		"pkg`evil`",
		"pkg$(evil)",
		"pkg\nevil",
		"--allow-untrusted",
		"--key=value",
	}

	for _, inj := range injections {
		t.Run(inj, func(t *testing.T) {
			resp := handleRequest(request{
				Action:  "install",
				Package: inj,
			})

			if resp.OK || resp.Error == "" {
				t.Errorf("injection %q should be rejected, got OK=%v err=%q", inj, resp.OK, resp.Error)
			}
		})
	}
}

// TestRequest_JSON tests request struct JSON unmarshaling.
func TestRequest_JSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    request
		wantErr bool
	}{
		{
			name:    "valid request",
			json:    `{"action":"install","package":"curl"}`,
			want:    request{Action: "install", Package: "curl"},
			wantErr: false,
		},
		{
			name:    "empty action",
			json:    `{"action":"","package":"curl"}`,
			want:    request{Action: "", Package: "curl"},
			wantErr: false, // JSON parsing succeeds, validation fails later
		},
		{
			name:    "empty package",
			json:    `{"action":"install","package":""}`,
			want:    request{Action: "install", Package: ""},
			wantErr: false, // JSON parsing succeeds, validation fails later
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req request
			err := unmarshalRequest(tt.json, &req)

			if (err != nil) != tt.wantErr {
				t.Errorf("unmarshalRequest() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && (req.Action != tt.want.Action || req.Package != tt.want.Package) {
				t.Errorf("unmarshalRequest() = %+v, want %+v", req, tt.want)
			}
		})
	}
}

// unmarshalRequest is a helper for testing JSON unmarshaling.
func unmarshalRequest(jsonStr string, req *request) error {
	return json.Unmarshal([]byte(jsonStr), req)
}

// TestResponse_JSON tests response struct JSON marshaling.
func TestResponse_JSON(t *testing.T) {
	tests := []struct {
		name     string
		resp     response
		wantOK   bool
		wantErr  string
		omitErr  bool
	}{
		{
			name:    "success response",
			resp:    response{OK: true},
			wantOK:  true,
			wantErr: "",
			omitErr: true,
		},
		{
			name:    "error response",
			resp:    response{OK: false, Error: "package not found"},
			wantOK:  false,
			wantErr: "package not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := marshalResponse(tt.resp)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}

			var decoded response
			if err := unmarshalResponse(data, &decoded); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}

			if decoded.OK != tt.wantOK {
				t.Errorf("OK = %v, want %v", decoded.OK, tt.wantOK)
			}
			if decoded.Error != tt.wantErr {
				t.Errorf("Error = %q, want %q", decoded.Error, tt.wantErr)
			}
		})
	}
}

// marshalResponse marshals a response (for testing).
func marshalResponse(resp response) ([]byte, error) {
	return json.Marshal(resp)
}

// unmarshalResponse unmarshals a response (for testing).
func unmarshalResponse(data []byte, resp *response) error {
	return json.Unmarshal(data, resp)
}

// TestValidPkgNameRegex_Compliance tests compliance with validation rules.
func TestValidPkgNameRegex_Compliance(t *testing.T) {
	// Test that regex enforces security constraints
	tests := []struct {
		name      string
		isValid   bool
		riskLevel string
	}{
		// Safe names
		{"curl", true, ""},
		{"github-cli", true, ""},
		{"python3", true, ""},
		{"@scope/pkg", true, ""},

		// Attack patterns that MUST be rejected
		{"-flag", false, "flag injection"},
		{"--option=value", false, "option injection"},
		{"pkg; cmd", false, "command injection"},
		{"pkg && cmd", false, "command injection"},
		{"pkg || cmd", false, "command injection"},
		{"pkg | cmd", false, "pipe injection"},
		{"pkg`cmd`", false, "command substitution"},
		{"pkg$(cmd)", false, "command substitution"},
		{"pkg${var}", false, "variable injection"},
		{"pkg'x'", false, "quote breaking"},
		{"pkg\"x\"", false, "quote breaking"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validPkgName.MatchString(tt.name)
			if got != tt.isValid {
				t.Errorf("validPkgName.MatchString(%q) = %v, want %v (risk: %s)", tt.name, got, tt.isValid, tt.riskLevel)
			}
		})
	}
}

// TestHandleRequest_ErrorMessages tests that error messages are clear.
func TestHandleRequest_ErrorMessages(t *testing.T) {
	tests := []struct {
		name        string
		req         request
		wantErrText string
	}{
		{
			name:        "missing package error text",
			req:         request{Action: "install", Package: ""},
			wantErrText: "package required",
		},
		{
			name:        "invalid package error text",
			req:         request{Action: "install", Package: "-bad"},
			wantErrText: "invalid package name",
		},
		{
			name:        "unknown action error text",
			req:         request{Action: "rebuild", Package: "curl"},
			wantErrText: "unknown action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := handleRequest(tt.req)
			if resp.Error == "" {
				t.Errorf("handleRequest() error = empty, want %q", tt.wantErrText)
			}
			if !contains(resp.Error, tt.wantErrText) {
				t.Errorf("handleRequest() error = %q, want to contain %q", resp.Error, tt.wantErrText)
			}
		})
	}
}

// TestHandleRequest_SuccessPath tests that valid requests pass validation.
// Note: Actual apk command execution will fail in test environment (no apk available),
// but validation should pass.
func TestHandleRequest_SuccessPath(t *testing.T) {
	tests := []struct {
		action string
		pkg    string
	}{
		{"install", "curl"},
		{"uninstall", "git"},
		{"install", "github-cli"},
		{"uninstall", "openssl"},
	}

	for _, tt := range tests {
		t.Run(tt.action+"-"+tt.pkg, func(t *testing.T) {
			resp := handleRequest(request{
				Action:  tt.action,
				Package: tt.pkg,
			})

			// Should not fail validation (may fail at exec stage without real apk)
			// We're testing that validation logic passes, not that apk exists
			validationErrors := []string{"package required", "invalid package name", "unknown action"}
			for _, validErr := range validationErrors {
				if contains(resp.Error, validErr) {
					t.Errorf("handleRequest(%q, %q) failed validation: %q", tt.action, tt.pkg, resp.Error)
				}
			}
		})
	}
}
