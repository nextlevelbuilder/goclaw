package http

// bailianModels returns a hardcoded list of models available on the
// Bailian Coding platform (coding-intl.dashscope.aliyuncs.com).
// The platform does not expose a /v1/models endpoint.
func bailianModels() []ModelInfo {
	return []ModelInfo{
		{ID: "qwen3.6-plus", Name: "Qwen 3.6 Plus"},
		{ID: "qwen3.5-plus", Name: "Qwen 3.5 Plus"},
		{ID: "kimi-k2.5", Name: "Kimi K2.5"},
		{ID: "GLM-5", Name: "GLM-5"},
		{ID: "MiniMax-M2.5", Name: "MiniMax M2.5"},
		{ID: "qwen3-max-2026-01-23", Name: "Qwen 3 Max (2026-01-23)"},
		{ID: "qwen3-coder-next", Name: "Qwen 3 Coder Next"},
		{ID: "qwen3-coder-plus", Name: "Qwen 3 Coder Plus"},
		{ID: "glm-4.7", Name: "GLM 4.7"},
	}
}

// minimaxModels returns a hardcoded list of MiniMax models.
// MiniMax does not expose a /v1/models endpoint.
func minimaxModels() []ModelInfo {
	return []ModelInfo{
		// Chat / text
		{ID: "MiniMax-Text-01", Name: "MiniMax Text 01"},
		{ID: "MiniMax-M1", Name: "MiniMax M1"},
		{ID: "MiniMax-M2.7", Name: "MiniMax M2.7"},
		{ID: "MiniMax-M2.5", Name: "MiniMax M2.5"},
		// Image generation
		{ID: "image-01", Name: "Image 01"},
		// Video generation
		{ID: "MiniMax-Hailuo-2.3", Name: "Hailuo Video 2.3"},
		{ID: "MiniMax-Hailuo-2", Name: "Hailuo Video 2"},
		{ID: "T2V-01-Director", Name: "T2V-01 Director"},
		// Music generation
		{ID: "music-2.5+", Name: "Music 2.5+"},
		{ID: "music-2.5", Name: "Music 2.5"},
		// TTS
		{ID: "speech-02-hd", Name: "Speech 02 HD"},
		{ID: "speech-02-turbo", Name: "Speech 02 Turbo"},
	}
}

// dashScopeModels returns a hardcoded list of DashScope (Qwen) models.
// DashScope does not expose a standard /v1/models endpoint.
func dashScopeModels() []ModelInfo {
	return []ModelInfo{
		// Qwen3.6 series — Agentic Coding + 1M context
		{ID: "qwen3.6-plus", Name: "Qwen 3.6 Plus"},
		// Qwen3.5 series — Text Generation + Deep Thinking + Visual Understanding
		{ID: "qwen3.5-plus", Name: "Qwen 3.5 Plus"},
		{ID: "qwen3.5-flash", Name: "Qwen 3.5 Flash"},
		{ID: "qwen3.5-turbo", Name: "Qwen 3.5 Turbo"},
		// Qwen3 hosted series — Text + Thinking
		{ID: "qwen3-max", Name: "Qwen 3 Max"},
		{ID: "qwen3-plus", Name: "Qwen 3 Plus"},
		{ID: "qwen3-turbo", Name: "Qwen 3 Turbo"},
		// Image generation
		{ID: "wan2.6-image", Name: "Wan 2.6 Image"},
		{ID: "wan2.1-image", Name: "Wan 2.1 Image"},
		// Video generation
		{ID: "wan2.6-video", Name: "Wan 2.6 Video"},
	}
}

// claudeCLIModels returns the model aliases accepted by the Claude CLI.
func claudeCLIModels() []ModelInfo {
	return []ModelInfo{
		{ID: "sonnet", Name: "Sonnet"},
		{ID: "opus", Name: "Opus"},
		{ID: "haiku", Name: "Haiku"},
	}
}

// acpModels returns the model aliases for ACP-compatible coding agents.
func acpModels() []ModelInfo {
	return []ModelInfo{
		{ID: "claude", Name: "Claude"},
		{ID: "codex", Name: "Codex"},
		{ID: "gemini", Name: "Gemini"},
	}
}

// chatGPTOAuthModels returns models available via ChatGPT OAuth integration.
func chatGPTOAuthModels() []ModelInfo {
	return withReasoningCapabilities([]ModelInfo{
		{ID: "gpt-5.4", Name: "GPT-5.4"},
		{ID: "gpt-5.4-mini", Name: "GPT-5.4 Mini"},
		{ID: "gpt-5.3-codex", Name: "GPT-5.3 Codex"},
		{ID: "gpt-5.3-codex-spark", Name: "GPT-5.3 Codex Spark"},
		{ID: "gpt-5.2-codex", Name: "GPT-5.2 Codex"},
		{ID: "gpt-5.2", Name: "GPT-5.2"},
		{ID: "gpt-5.1-codex", Name: "GPT-5.1 Codex"},
		{ID: "gpt-5.1-codex-max", Name: "GPT-5.1 Codex Max"},
		{ID: "gpt-5.1-codex-mini", Name: "GPT-5.1 Codex Mini"},
		{ID: "gpt-5.1", Name: "GPT-5.1"},
	})
}
