package providers

import "testing"

func TestOpenAIProvider_isDashScope(t *testing.T) {
	cases := []struct {
		name    string
		apiBase string
		ptype   string
		pname   string
		want    bool
	}{
		{"coding-intl URL", "https://coding-intl.dashscope.aliyuncs.com/v1", "openai_compat", "qwen-richard", true},
		{"providerType=bailian", "https://custom-proxy.example.com/v1", "bailian", "internal-qwen", true},
		{"providerType=dashscope", "https://proxy.example.com/v1", "dashscope", "x", true},
		{"name contains dashscope", "https://proxy.com/v1", "openai_compat", "my-dashscope-mirror", true},
		{"name contains bailian", "https://proxy.com/v1", "openai_compat", "company-bailian-relay", true},
		{"openai native", "https://api.openai.com/v1", "openai", "gpt", false},
		{"anthropic", "https://api.anthropic.com", "anthropic", "claude", false},
		{"openrouter", "https://openrouter.ai/api/v1", "openai_compat", "openrouter", false},
		{"empty", "", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &OpenAIProvider{apiBase: tc.apiBase, providerType: tc.ptype, name: tc.pname}
			if got := p.isDashScope(); got != tc.want {
				t.Errorf("isDashScope() = %v, want %v (apiBase=%q ptype=%q name=%q)",
					got, tc.want, tc.apiBase, tc.ptype, tc.pname)
			}
		})
	}
}

func TestOpenAIProvider_dashScopePassthroughKeys(t *testing.T) {
	cases := []struct {
		name    string
		apiBase string
		ptype   string
		pname   string
		want    bool
	}{
		{"dashscope URL", "https://coding-intl.dashscope.aliyuncs.com/v1", "openai_compat", "qwen", true},
		{"providerType=dashscope", "https://proxy.example.com/v1", "dashscope", "qwen", true},
		{"providerType=bailian", "https://proxy.example.com/v1", "bailian", "qwen", true},
		{"name contains dashscope", "https://proxy.example.com/v1", "openai_compat", "team-dashscope", true},
		{"name contains bailian", "https://proxy.example.com/v1", "openai_compat", "team-bailian", true},
		{"openrouter", "https://openrouter.ai/api/v1", "openai_compat", "openrouter", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &OpenAIProvider{apiBase: tc.apiBase, providerType: tc.ptype, name: tc.pname}
			if got := p.dashScopePassthroughKeys(); got != tc.want {
				t.Errorf("dashScopePassthroughKeys() = %v, want %v", got, tc.want)
			}
		})
	}
}
