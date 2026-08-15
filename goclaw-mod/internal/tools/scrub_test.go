package tools

import "testing"

func TestScrubCredentials_OpenAI(t *testing.T) {
	input := "Found key: sk-abcdefghijklmnopqrstuvwxyz1234567890 in env"
	got := ScrubCredentials(input)
	if got != "Found key: [REDACTED] in env" {
		t.Errorf("OpenAI key not scrubbed: %s", got)
	}
}

func TestScrubCredentials_Anthropic(t *testing.T) {
	input := "key=sk-ant-abc123-def456-ghi789-jkl012"
	got := ScrubCredentials(input)
	if got != "key=[REDACTED]" {
		t.Errorf("Anthropic key not scrubbed: %s", got)
	}
}

func TestScrubCredentials_GitHub(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"ghp", "token ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij done"},
		{"gho", "token gho_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij done"},
		{"ghu", "token ghu_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij done"},
		{"ghs", "token ghs_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij done"},
		{"ghr", "token ghr_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij done"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScrubCredentials(tt.input)
			want := "token [REDACTED] done"
			if got != want {
				t.Errorf("GitHub %s not scrubbed: got %q, want %q", tt.name, got, want)
			}
		})
	}
}

func TestScrubCredentials_AWS(t *testing.T) {
	input := "aws_key: AKIAIOSFODNN7EXAMPLE"
	got := ScrubCredentials(input)
	want := "aws_key: [REDACTED]"
	if got != want {
		t.Errorf("AWS key scrub: got %q, want %q", got, want)
	}
}

func TestScrubCredentials_GenericKeyValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"api_key", "api_key=supersecretvalue123", "[REDACTED]"},
		{"token", "token: mysecrettoken12345", "[REDACTED]"},
		{"password", "password=MyStr0ngP@ssword!", "[REDACTED]"},
		{"bearer", "bearer: eyJhbGciOiJIUzI1NiJ9.abc", "[REDACTED]"},
		{"authorization", "authorization=eyJhbGciOiJIUzI1NiJ9abcdef", "[REDACTED]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScrubCredentials(tt.input)
			if got != tt.want {
				t.Errorf("generic pattern %q: got %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestScrubCredentials_NoFalsePositive(t *testing.T) {
	inputs := []string{
		"hello world",
		"sk-short",       // too short for OpenAI pattern
		"ghp_tooshort",   // too short for GitHub pattern
		"normal text with no secrets",
		"AKIA1234",       // too short for AWS (needs 16 chars after AKIA)
	}
	for _, input := range inputs {
		got := ScrubCredentials(input)
		if got != input {
			t.Errorf("false positive on %q: got %q", input, got)
		}
	}
}

func TestScrubCredentials_MultiplePatterns(t *testing.T) {
	input := "openai=sk-abcdefghijklmnopqrstuvwxyz, github=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	got := ScrubCredentials(input)
	want := "openai=[REDACTED], github=[REDACTED]"
	if got != want {
		t.Errorf("multiple patterns scrub: got %q, want %q", got, want)
	}
}

func TestScrubCredentials_EmptyString(t *testing.T) {
	got := ScrubCredentials("")
	if got != "" {
		t.Errorf("empty string changed: %q", got)
	}
}

func TestScrubCredentials_DynamicServerIPs(t *testing.T) {
	ResetDynamicScrubValues()
	defer ResetDynamicScrubValues()

	AddDynamicScrubValues("18.141.232.136", "34.160.111.145")

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"public IP", "Your IP: 18.141.232.136", "Your IP: [SERVER_IP]"},
		{"forwarded chain", "X-Forwarded-For: 18.141.232.136, 34.160.111.145", "X-Forwarded-For: [SERVER_IP], [SERVER_IP]"},
		{"unrelated IP", "Hello 1.2.3.4", "Hello 1.2.3.4"},
		{"IP in URL", "http://18.141.232.136:8080/api", "http://[SERVER_IP]:8080/api"},
		{"multiple occurrences", "src=18.141.232.136 dst=18.141.232.136", "src=[SERVER_IP] dst=[SERVER_IP]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScrubCredentials(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAddDynamicScrubValues_Dedup(t *testing.T) {
	ResetDynamicScrubValues()
	defer ResetDynamicScrubValues()

	AddDynamicScrubValues("10.0.0.1", "10.0.0.1", "10.0.0.2")
	if count := DynamicScrubCount(); count != 2 {
		t.Errorf("expected 2 unique values, got %d", count)
	}
}

func TestAddDynamicScrubValues_IgnoresEmpty(t *testing.T) {
	ResetDynamicScrubValues()
	defer ResetDynamicScrubValues()

	AddDynamicScrubValues("", "", "10.0.0.1")
	if count := DynamicScrubCount(); count != 1 {
		t.Errorf("expected 1 value, got %d", count)
	}
}

func TestDetectLocalIPs_NoLoopback(t *testing.T) {
	ips := detectLocalIPs()
	for _, ipStr := range ips {
		if ipStr == "127.0.0.1" || ipStr == "::1" {
			t.Errorf("loopback IP should be excluded: %s", ipStr)
		}
	}
}
