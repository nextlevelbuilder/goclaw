package sandbox

import (
	"strings"
	"testing"
)

func TestDefaultConfig_Runtime(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Runtime != RuntimeDocker {
		t.Errorf("expected RuntimeDocker default, got %s", cfg.Runtime)
	}
}

func TestDefaultConfig_Namespace(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Namespace != "goclaw-sandbox" {
		t.Errorf("expected 'goclaw-sandbox', got %s", cfg.Namespace)
	}
}

func TestSanitizeK8sName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"agent:main:telegram:direct:123", "agent-main-telegram-direct-123"},
		{"UPPERCASE", "uppercase"},
		{"has/slash", "has-slash"},
		{"has space", "has-space"},
		{"has.dot", "has-dot"},
		{"has_underscore", "has-underscore"},
		{"---leading-dashes", "leading-dashes"},
		{"trailing-dashes---", "trailing-dashes"},
		{"", "default"},
		{strings.Repeat("x", 100), strings.Repeat("x", 50)},
	}
	for _, tc := range tests {
		got := sanitizeK8sName(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeK8sName(%q) = %q, want %q", tc.input, got, tc.expected)
		}
		// Verify DNS-1123 compliance
		for _, c := range got {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				t.Errorf("sanitizeK8sName(%q) = %q contains invalid char %c", tc.input, got, c)
			}
		}
		if len(got) > 0 && (got[0] == '-' || got[len(got)-1] == '-') {
			t.Errorf("sanitizeK8sName(%q) = %q starts or ends with dash", tc.input, got)
		}
	}
}

func TestConfig_RuntimeK8s(t *testing.T) {
	cfg := Config{
		Runtime:   RuntimeK8s,
		Namespace: "test-ns",
		Image:     "alpine:latest",
		Mode:      ModeAll,
	}
	if cfg.Runtime != RuntimeK8s {
		t.Errorf("expected RuntimeK8s, got %s", cfg.Runtime)
	}
	if cfg.Namespace != "test-ns" {
		t.Errorf("expected 'test-ns', got %s", cfg.Namespace)
	}
	if cfg.Image != "alpine:latest" {
		t.Errorf("expected 'alpine:latest', got %s", cfg.Image)
	}
	if !cfg.ShouldSandbox("worker") {
		t.Error("ModeAll should sandbox any agent")
	}
}

func TestConfig_RuntimeConstants(t *testing.T) {
	if RuntimeDocker != "docker" {
		t.Errorf("expected RuntimeDocker = 'docker', got %s", RuntimeDocker)
	}
	if RuntimeK8s != "k8s" {
		t.Errorf("expected RuntimeK8s = 'k8s', got %s", RuntimeK8s)
	}
}

func TestImagePullPolicy(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Always", "Always"},
		{"Never", "Never"},
		{"IfNotPresent", "IfNotPresent"},
		{"", "IfNotPresent"},
		{"invalid", "IfNotPresent"},
	}
	for _, tc := range tests {
		got := string(imagePullPolicy(tc.input))
		if got != tc.expected {
			t.Errorf("imagePullPolicy(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestBuildResourceRequirements(t *testing.T) {
	cfg := Config{MemoryMB: 512, CPUs: 1.0}
	reqs := buildResourceRequirements(cfg)

	// Guaranteed QoS: Requests == Limits
	if reqs.Requests.Memory().String() != reqs.Limits.Memory().String() {
		t.Errorf("memory requests (%s) != limits (%s)", reqs.Requests.Memory(), reqs.Limits.Memory())
	}
	if reqs.Requests.Cpu().String() != reqs.Limits.Cpu().String() {
		t.Errorf("cpu requests (%s) != limits (%s)", reqs.Requests.Cpu(), reqs.Limits.Cpu())
	}
}
