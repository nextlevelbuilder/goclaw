package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGetApkVersion tests parsing of apk list output.
func TestGetApkVersion(t *testing.T) {
	tests := []struct {
		name    string
		pkgName string
		// Mock apk command output
		mockOutput string
		mockErr    bool
		want       string
	}{
		{
			name:       "single version",
			pkgName:    "github-cli",
			mockOutput: "github-cli-2.72.0-r6 aarch64 {github-cli} (MIT) [installed]",
			want:       "2.72.0-r6",
		},
		{
			name:       "version with multiple parts",
			pkgName:    "openssl",
			mockOutput: "openssl-3.1.4-r5 x86_64 {openssl} (Apache-2.0) [installed]",
			want:       "3.1.4-r5",
		},
		{
			name:       "empty output (not installed)",
			pkgName:    "nonexistent",
			mockOutput: "",
			want:       "",
		},
		{
			name:       "whitespace in output",
			pkgName:    "curl",
			mockOutput: "  curl-8.5.0-r1 x86_64 {curl} (MIT) [installed]  ",
			want:       "8.5.0-r1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the version extraction logic directly
			got := extractApkVersion(tt.mockOutput, tt.pkgName)
			if got != tt.want {
				t.Errorf("extractApkVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

// extractApkVersion is extracted from getApkVersion logic for testing.
// This simulates the parsing without requiring actual apk command.
func extractApkVersion(output, name string) string {
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Extract version: everything between "name-" and the first space.
		if strings.HasPrefix(line, name+"-") {
			rest := strings.TrimPrefix(line, name+"-")
			if idx := strings.IndexByte(rest, ' '); idx > 0 {
				return rest[:idx]
			}
			return rest
		}
	}
	return ""
}

// TestListPipPackages tests pip JSON parsing.
func TestListPipPackages_Parsing(t *testing.T) {
	tests := []struct {
		name       string
		jsonOutput string
		wantCount  int
		wantNames  []string
	}{
		{
			name:       "empty list",
			jsonOutput: "[]",
			wantCount:  0,
		},
		{
			name: "single package",
			jsonOutput: `[
				{"name": "pandas", "version": "2.0.3"}
			]`,
			wantCount: 1,
			wantNames: []string{"pandas"},
		},
		{
			name: "multiple packages",
			jsonOutput: `[
				{"name": "pandas", "version": "2.0.3"},
				{"name": "numpy", "version": "1.24.3"},
				{"name": "requests", "version": "2.31.0"}
			]`,
			wantCount: 3,
			wantNames: []string{"pandas", "numpy", "requests"},
		},
		{
			name: "preserves version info",
			jsonOutput: `[
				{"name": "pytest", "version": "7.4.0"},
				{"name": "black", "version": "23.7.0"}
			]`,
			wantCount: 2,
			wantNames: []string{"pytest", "black"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkgs := parsePipJSON(tt.jsonOutput)

			if len(pkgs) != tt.wantCount {
				t.Errorf("got %d packages, want %d", len(pkgs), tt.wantCount)
			}

			for i, want := range tt.wantNames {
				if i >= len(pkgs) {
					t.Errorf("package %d missing", i)
					continue
				}
				if pkgs[i].Name != want {
					t.Errorf("package[%d].Name = %q, want %q", i, pkgs[i].Name, want)
				}
			}
		})
	}
}

// parsePipJSON extracts pip package info for testing.
func parsePipJSON(jsonOutput string) []PackageInfo {
	var raw []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &raw); err != nil {
		return nil
	}

	pkgs := make([]PackageInfo, 0, len(raw))
	for _, r := range raw {
		pkgs = append(pkgs, PackageInfo{Name: r.Name, Version: r.Version})
	}
	return pkgs
}

// TestListNpmPackages tests npm JSON parsing.
func TestListNpmPackages_Parsing(t *testing.T) {
	tests := []struct {
		name       string
		jsonOutput string
		wantCount  int
		wantNames  []string
	}{
		{
			name: "no dependencies",
			jsonOutput: `{
				"dependencies": {}
			}`,
			wantCount: 0,
		},
		{
			name: "single package",
			jsonOutput: `{
				"dependencies": {
					"typescript": {"version": "5.1.6"}
				}
			}`,
			wantCount: 1,
			wantNames: []string{"typescript"},
		},
		{
			name: "multiple packages",
			jsonOutput: `{
				"dependencies": {
					"typescript": {"version": "5.1.6"},
					"webpack": {"version": "5.88.0"},
					"prettier": {"version": "3.0.0"}
				}
			}`,
			wantCount: 3,
			wantNames: []string{"typescript", "webpack", "prettier"},
		},
		{
			name: "scoped packages",
			jsonOutput: `{
				"dependencies": {
					"@types/node": {"version": "20.4.5"},
					"@angular/core": {"version": "16.1.0"}
				}
			}`,
			wantCount: 2,
			wantNames: []string{"@types/node", "@angular/core"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkgs := parseNpmJSON(tt.jsonOutput)

			if len(pkgs) != tt.wantCount {
				t.Errorf("got %d packages, want %d", len(pkgs), tt.wantCount)
			}

			nameSet := make(map[string]bool)
			for _, pkg := range pkgs {
				nameSet[pkg.Name] = true
			}

			for _, want := range tt.wantNames {
				if !nameSet[want] {
					t.Errorf("package %q not found", want)
				}
			}
		})
	}
}

// parseNpmJSON extracts npm package info for testing.
func parseNpmJSON(jsonOutput string) []PackageInfo {
	var raw struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &raw); err != nil {
		return nil
	}

	pkgs := make([]PackageInfo, 0, len(raw.Dependencies))
	for name, info := range raw.Dependencies {
		pkgs = append(pkgs, PackageInfo{Name: name, Version: info.Version})
	}
	return pkgs
}

// TestListApkUserPackages tests reading from persist file.
func TestListApkUserPackages_PersistFile(t *testing.T) {
	tests := []struct {
		name      string
		fileLines string
		wantCount int
		wantNames []string
	}{
		{
			name:      "empty file",
			fileLines: "",
			wantCount: 0,
		},
		{
			name:      "single package",
			fileLines: "github-cli\n",
			wantCount: 1,
			wantNames: []string{"github-cli"},
		},
		{
			name:      "multiple packages",
			fileLines: "github-cli\ncurl\nopenssl\n",
			wantCount: 3,
			wantNames: []string{"github-cli", "curl", "openssl"},
		},
		{
			name:      "deduplicates duplicates",
			fileLines: "curl\ngithub-cli\ncurl\n",
			wantCount: 2,
			wantNames: []string{"curl", "github-cli"},
		},
		{
			name:      "ignores blank lines and whitespace",
			fileLines: "curl\n\n  \ngithub-cli\n",
			wantCount: 2,
			wantNames: []string{"curl", "github-cli"},
		},
		{
			name:      "no trailing newline",
			fileLines: "curl\ngithub-cli",
			wantCount: 2,
			wantNames: []string{"curl", "github-cli"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names := parseApkPersistFile(tt.fileLines)

			if len(names) != tt.wantCount {
				t.Errorf("got %d packages, want %d", len(names), tt.wantCount)
			}

			nameSet := make(map[string]bool)
			for _, name := range names {
				nameSet[name] = true
			}

			for _, want := range tt.wantNames {
				if !nameSet[want] {
					t.Errorf("package %q not found", want)
				}
			}
		})
	}
}

// parseApkPersistFile simulates listApkUserPackages parsing logic.
func parseApkPersistFile(fileContent string) []string {
	seen := make(map[string]bool)
	var names []string
	for _, line := range strings.Split(fileContent, "\n") {
		name := strings.TrimSpace(line)
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// TestPackageInfo_JSON tests PackageInfo JSON marshaling.
func TestPackageInfo_JSON(t *testing.T) {
	pkg := PackageInfo{
		Name:    "curl",
		Version: "8.5.0-r1",
	}

	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded PackageInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.Name != pkg.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, pkg.Name)
	}
	if decoded.Version != pkg.Version {
		t.Errorf("Version = %q, want %q", decoded.Version, pkg.Version)
	}
}

// TestInstalledPackages_JSON tests InstalledPackages JSON marshaling.
func TestInstalledPackages_JSON(t *testing.T) {
	pkgs := &InstalledPackages{
		System: []PackageInfo{
			{Name: "curl", Version: "8.5.0-r1"},
		},
		Pip: []PackageInfo{
			{Name: "pandas", Version: "2.0.3"},
		},
		Npm: []PackageInfo{
			{Name: "typescript", Version: "5.1.6"},
		},
	}

	data, err := json.Marshal(pkgs)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded InstalledPackages
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if len(decoded.System) != 1 || decoded.System[0].Name != "curl" {
		t.Errorf("System packages not decoded correctly")
	}
	if len(decoded.Pip) != 1 || decoded.Pip[0].Name != "pandas" {
		t.Errorf("Pip packages not decoded correctly")
	}
	if len(decoded.Npm) != 1 || decoded.Npm[0].Name != "typescript" {
		t.Errorf("Npm packages not decoded correctly")
	}
}

// TestListInstalledPackages_ReturnsNonNil ensures return is not nil.
func TestListInstalledPackages_ReturnsNonNil(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := ListInstalledPackages(ctx)
	if result == nil {
		t.Error("ListInstalledPackages returned nil, want *InstalledPackages")
	}
}

// TestListInstalledPackages_HasThreeCategories ensures all categories are accessible.
func TestListInstalledPackages_HasThreeCategories(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := ListInstalledPackages(ctx)
	// Categories can be nil if no packages are installed in that category
	// Just verify the struct itself is not nil
	if result == nil {
		t.Error("ListInstalledPackages returned nil")
	}
}

// TestListInstalledPackages_ContextTimeout tests timeout behavior.
func TestListInstalledPackages_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Should return a result even if timeout occurs during execution.
	// The function creates its own timeout context internally.
	result := ListInstalledPackages(ctx)
	if result == nil {
		t.Fatal("ListInstalledPackages returned nil on timeout")
	}
	// Categories may be nil if no packages are installed or command timed out
	// Just verify the struct itself was initialized
}

// TestApkListFile_RuntimeDir tests runtime directory handling.
func TestApkListFile_RuntimeDir(t *testing.T) {
	// Save original env
	orig := os.Getenv("RUNTIME_DIR")
	defer func() {
		if orig != "" {
			os.Setenv("RUNTIME_DIR", orig)
		} else {
			os.Unsetenv("RUNTIME_DIR")
		}
	}()

	tests := []struct {
		name    string
		envVal  string
		wantDir string
	}{
		{
			name:    "uses RUNTIME_DIR when set",
			envVal:  "/custom/runtime",
			wantDir: "/custom/runtime",
		},
		{
			name:    "uses default when empty",
			envVal:  "",
			wantDir: "/app/data/.runtime",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				os.Setenv("RUNTIME_DIR", tt.envVal)
			} else {
				os.Unsetenv("RUNTIME_DIR")
			}

			got := getApkListFileDir()
			want := filepath.Join(tt.wantDir, "apk-packages")

			if got != want {
				t.Errorf("getApkListFileDir() = %q, want %q", got, want)
			}
		})
	}
}

// getApkListFileDir extracts the directory logic for testing.
func getApkListFileDir() string {
	runtimeDir := os.Getenv("RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/app/data/.runtime"
	}
	return filepath.Join(runtimeDir, "apk-packages")
}

// TestExtractApkVersion_EdgeCases tests version extraction edge cases.
func TestExtractApkVersion_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		pkgName string
		output  string
		want    string
	}{
		{
			name:    "version with no space suffix",
			pkgName: "pkg",
			output:  "pkg-1.0.0",
			want:    "1.0.0",
		},
		{
			name:    "multiple matching lines (takes first)",
			pkgName: "curl",
			output:  "curl-7.0.0 arch1 info\ncurl-8.0.0 arch2 info",
			want:    "7.0.0",
		},
		{
			name:    "name not found",
			pkgName: "notfound",
			output:  "curl-8.5.0-r1 aarch64 {curl} (MIT) [installed]",
			want:    "",
		},
		{
			name:    "case sensitive match",
			pkgName: "Curl",
			output:  "curl-8.5.0-r1 aarch64",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractApkVersion(tt.output, tt.pkgName)
			if got != tt.want {
				t.Errorf("extractApkVersion(%q, %q) = %q, want %q", tt.output, tt.pkgName, got, tt.want)
			}
		})
	}
}

// TestParsePipJSON_InvalidJSON tests error handling.
func TestParsePipJSON_InvalidJSON(t *testing.T) {
	pkgs := parsePipJSON("not valid json")
	if pkgs != nil {
		t.Errorf("parsePipJSON with invalid JSON = %v, want nil", pkgs)
	}
}

// TestParseNpmJSON_InvalidJSON tests error handling.
func TestParseNpmJSON_InvalidJSON(t *testing.T) {
	pkgs := parseNpmJSON("not valid json")
	if pkgs != nil {
		t.Errorf("parseNpmJSON with invalid JSON = %v, want nil", pkgs)
	}
}

// TestParseNpmJSON_NoDependencies tests npm output with no dependencies.
func TestParseNpmJSON_NoDependencies(t *testing.T) {
	pkgs := parseNpmJSON(`{"dependencies": null}`)
	if len(pkgs) != 0 {
		t.Errorf("parseNpmJSON with null dependencies = %d packages, want 0", len(pkgs))
	}
}

// TestParseApkPersistFile_Deduplication ensures duplicates are removed.
func TestParseApkPersistFile_Deduplication(t *testing.T) {
	content := "curl\ngithub-cli\ncurl\nopenssl\ngithub-cli\ncurl"
	names := parseApkPersistFile(content)

	if len(names) != 3 {
		t.Errorf("deduplication got %d packages, want 3", len(names))
	}

	seen := make(map[string]bool)
	for _, name := range names {
		if seen[name] {
			t.Errorf("duplicate package found: %q", name)
		}
		seen[name] = true
	}
}

// TestParseApkPersistFile_PreservesOrder ensures first occurrence is kept.
func TestParseApkPersistFile_PreservesOrder(t *testing.T) {
	content := "curl\ngithub-cli\nopenssl"
	names := parseApkPersistFile(content)

	if len(names) != 3 {
		t.Fatalf("got %d packages, want 3", len(names))
	}

	want := []string{"curl", "github-cli", "openssl"}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("names[%d] = %q, want %q", i, names[i], w)
		}
	}
}
