package providers

import "testing"

func TestProfileForKnownModels(t *testing.T) {
	cases := []struct {
		name        string
		model       string
		toolCalling CapabilityLevel
		reasoning   CapabilityLevel
		vision      CapabilityLevel
		maxContext  int
	}{
		{name: "gpt-4o family", model: "gpt-4o-2024-08-06", toolCalling: CapabilityStrong, reasoning: CapabilityMedium, vision: CapabilityStrong, maxContext: 128000},
		{name: "claude-3-5-sonnet", model: "claude-3-5-sonnet-20241022", toolCalling: CapabilityStrong, reasoning: CapabilityStrong, vision: CapabilityMedium, maxContext: 200000},
		{name: "claude-sonnet-4", model: "claude-sonnet-4-5", toolCalling: CapabilityStrong, reasoning: CapabilityStrong, vision: CapabilityMedium, maxContext: 200000},
		{name: "deepseek-reasoner", model: "deepseek-reasoner", toolCalling: CapabilityNone, reasoning: CapabilityStrong, vision: CapabilityNone, maxContext: 128000},
		{name: "phi-3 tool calling banned", model: "phi-3-mini-4k-instruct", toolCalling: CapabilityNone, reasoning: CapabilityMedium, vision: CapabilityNone, maxContext: 128000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ProfileFor(tc.model)
			if got.ToolCalling != tc.toolCalling {
				t.Errorf("ProfileFor(%q).ToolCalling = %q, want %q", tc.model, got.ToolCalling, tc.toolCalling)
			}
			if got.Reasoning != tc.reasoning {
				t.Errorf("ProfileFor(%q).Reasoning = %q, want %q", tc.model, got.Reasoning, tc.reasoning)
			}
			if got.Vision != tc.vision {
				t.Errorf("ProfileFor(%q).Vision = %q, want %q", tc.model, got.Vision, tc.vision)
			}
			if got.MaxContext != tc.maxContext {
				t.Errorf("ProfileFor(%q).MaxContext = %d, want %d", tc.model, got.MaxContext, tc.maxContext)
			}
		})
	}
}

func TestProfileForUnknownModelDefaultsToPermissiveProfile(t *testing.T) {
	got := ProfileFor("totally-unknown-model-42")
	want := defaultCapabilityProfile
	if got != want {
		t.Errorf("ProfileFor(unknown) = %+v, want default %+v", got, want)
	}
	if !got.SupportsToolCalls() {
		t.Error("default profile SupportsToolCalls() = false, want true (must never block tool calls)")
	}
	if !got.SupportsStreaming() {
		t.Error("default profile SupportsStreaming() = false, want true")
	}
	if got.MaxContext != 0 {
		t.Errorf("default profile MaxContext = %d, want 0 (unknown)", got.MaxContext)
	}
}

func TestProfileForPrefixMatching(t *testing.T) {
	// Sibling models in a family must match via the family prefix.
	if got := ProfileFor("gemini-2.5-pro"); got.MaxContext != 1048576 {
		t.Errorf("ProfileFor(gemini-2.5-pro).MaxContext = %d, want 1048576", got.MaxContext)
	}
	if got := ProfileFor("deepseek-chat-v3.2"); got.Reasoning != CapabilityMedium {
		t.Errorf("ProfileFor(deepseek-chat-v3.2).Reasoning = %q, want medium", got.Reasoning)
	}
	if got := ProfileFor("codex-2"); got.ToolCalling != CapabilityStrong {
		t.Errorf("ProfileFor(codex-2).ToolCalling = %q, want strong", got.ToolCalling)
	}
}

func TestProfileForQwenVisionVariants(t *testing.T) {
	cases := []struct {
		model string
		vision CapabilityLevel
	}{
		{model: "qwen-vl-max", vision: CapabilityStrong},
		{model: "qwen2.5-vl-72b", vision: CapabilityStrong},
		{model: "qwen-max", vision: CapabilityNone},
		{model: "qwen2.5-72b", vision: CapabilityNone},
	}
	for _, tc := range cases {
		if got := ProfileFor(tc.model); got.Vision != tc.vision {
			t.Errorf("ProfileFor(%q).Vision = %q, want %q", tc.model, got.Vision, tc.vision)
		}
	}
}

func TestProfileForDoesNotMatchUnrelatedPrefixes(t *testing.T) {
	// "gpt-4o" must not capture "gpt-4o-mini-unknown-suffix-typo" style
	// matches for unrelated ids; and vision must not leak into non-vision
	// families via the qwen "vl" check.
	if got := ProfileFor("gpt-4-turbo"); got.Vision != CapabilityNone {
		t.Errorf("ProfileFor(gpt-4-turbo).Vision = %q, want none (falls back to default)", got.Vision)
	}
	if got := ProfileFor("qwenvl"); got.Vision != CapabilityStrong {
		t.Errorf("ProfileFor(qwenvl).Vision = %q, want strong (vl prefix on qwen)", got.Vision)
	}
}