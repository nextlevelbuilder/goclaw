package config

import "encoding/json"

const secretMask = "***"

// MaskedCopy returns a deep copy of the config with all secret fields masked.
// Used by config.get to avoid exposing secrets to WebSocket clients.
func (c *Config) MaskedCopy() *Config {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Deep copy via JSON round-trip
	data, err := json.Marshal(c)
	if err != nil {
		return &Config{}
	}
	cp := Default()
	if err := json.Unmarshal(data, cp); err != nil {
		return &Config{}
	}

	// Mask provider API keys
	maskNonEmpty(&cp.Providers.Anthropic.APIKey)
	maskNonEmpty(&cp.Providers.OpenAI.APIKey)
	maskNonEmpty(&cp.Providers.OpenRouter.APIKey)
	maskNonEmpty(&cp.Providers.Groq.APIKey)
	maskNonEmpty(&cp.Providers.DeepSeek.APIKey)
	maskNonEmpty(&cp.Providers.Gemini.APIKey)
	maskNonEmpty(&cp.Providers.Mistral.APIKey)
	maskNonEmpty(&cp.Providers.XAI.APIKey)
	maskNonEmpty(&cp.Providers.MiniMax.APIKey)
	maskNonEmpty(&cp.Providers.Cohere.APIKey)
	maskNonEmpty(&cp.Providers.Perplexity.APIKey)
	maskNonEmpty(&cp.Providers.DashScope.APIKey)
	maskNonEmpty(&cp.Providers.Bailian.APIKey)
	maskNonEmpty(&cp.Providers.Zai.APIKey)
	maskNonEmpty(&cp.Providers.ZaiCoding.APIKey)
	maskNonEmpty(&cp.Providers.OllamaCloud.APIKey)

	// Mask gateway token
	maskNonEmpty(&cp.Gateway.Token)

	// Mask channel secrets
	maskNonEmpty(&cp.Channels.Telegram.Token)
	maskNonEmpty(&cp.Channels.Discord.Token)
	maskNonEmpty(&cp.Channels.Slack.BotToken)
	maskNonEmpty(&cp.Channels.Slack.AppToken)
	maskNonEmpty(&cp.Channels.Zalo.Token)
	maskNonEmpty(&cp.Channels.Zalo.WebhookSecret)
	maskNonEmpty(&cp.Channels.Feishu.AppID)
	maskNonEmpty(&cp.Channels.Feishu.AppSecret)
	maskNonEmpty(&cp.Channels.Feishu.EncryptKey)
	maskNonEmpty(&cp.Channels.Feishu.VerificationToken)

	// Mask TTS API keys
	maskNonEmpty(&cp.Tts.OpenAI.APIKey)
	maskNonEmpty(&cp.Tts.ElevenLabs.APIKey)
	maskNonEmpty(&cp.Tts.MiniMax.APIKey)

	// Mask Tailscale auth key
	maskNonEmpty(&cp.Tailscale.AuthKey)

	return cp
}

// StripSecrets zeros out all secret fields in the config.
// Used before saving to disk to ensure secrets never persist in config.json.
func (c *Config) StripSecrets() {
	// Provider API keys
	c.Providers.Anthropic.APIKey = ""
	c.Providers.OpenAI.APIKey = ""
	c.Providers.OpenRouter.APIKey = ""
	c.Providers.Groq.APIKey = ""
	c.Providers.DeepSeek.APIKey = ""
	c.Providers.Gemini.APIKey = ""
	c.Providers.Mistral.APIKey = ""
	c.Providers.XAI.APIKey = ""
	c.Providers.MiniMax.APIKey = ""
	c.Providers.Cohere.APIKey = ""
	c.Providers.Perplexity.APIKey = ""
	c.Providers.DashScope.APIKey = ""
	c.Providers.Bailian.APIKey = ""
	c.Providers.Zai.APIKey = ""
	c.Providers.ZaiCoding.APIKey = ""
	c.Providers.OllamaCloud.APIKey = ""

	// Gateway token
	c.Gateway.Token = ""

	// Channel secrets
	c.Channels.Telegram.Token = ""
	c.Channels.Discord.Token = ""
	c.Channels.Slack.BotToken = ""
	c.Channels.Slack.AppToken = ""
	c.Channels.Zalo.Token = ""
	c.Channels.Zalo.WebhookSecret = ""
	c.Channels.Feishu.AppID = ""
	c.Channels.Feishu.AppSecret = ""
	c.Channels.Feishu.EncryptKey = ""
	c.Channels.Feishu.VerificationToken = ""

	// TTS API keys
	c.Tts.OpenAI.APIKey = ""
	c.Tts.ElevenLabs.APIKey = ""
	c.Tts.MiniMax.APIKey = ""

	// Tailscale auth key
	c.Tailscale.AuthKey = ""
}

// StripMaskedSecrets strips only fields that still contain the mask value "***".
// Real values (user-entered via UI) are preserved, so that secrets entered
// via the config UI persist in config.json.
func (c *Config) StripMaskedSecrets() {
	stripIfMasked := func(s *string) {
		if *s == secretMask {
			*s = ""
		}
	}

	// Provider API keys
	stripIfMasked(&c.Providers.Anthropic.APIKey)
	stripIfMasked(&c.Providers.OpenAI.APIKey)
	stripIfMasked(&c.Providers.OpenRouter.APIKey)
	stripIfMasked(&c.Providers.Groq.APIKey)
	stripIfMasked(&c.Providers.DeepSeek.APIKey)
	stripIfMasked(&c.Providers.Gemini.APIKey)
	stripIfMasked(&c.Providers.Mistral.APIKey)
	stripIfMasked(&c.Providers.XAI.APIKey)
	stripIfMasked(&c.Providers.MiniMax.APIKey)
	stripIfMasked(&c.Providers.Cohere.APIKey)
	stripIfMasked(&c.Providers.Perplexity.APIKey)
	stripIfMasked(&c.Providers.DashScope.APIKey)
	stripIfMasked(&c.Providers.Bailian.APIKey)
	stripIfMasked(&c.Providers.Zai.APIKey)
	stripIfMasked(&c.Providers.ZaiCoding.APIKey)
	stripIfMasked(&c.Providers.OllamaCloud.APIKey)

	// Gateway token
	stripIfMasked(&c.Gateway.Token)

	// Channel secrets
	stripIfMasked(&c.Channels.Telegram.Token)
	stripIfMasked(&c.Channels.Discord.Token)
	stripIfMasked(&c.Channels.Slack.BotToken)
	stripIfMasked(&c.Channels.Slack.AppToken)
	stripIfMasked(&c.Channels.Zalo.Token)
	stripIfMasked(&c.Channels.Zalo.WebhookSecret)
	stripIfMasked(&c.Channels.Feishu.AppID)
	stripIfMasked(&c.Channels.Feishu.AppSecret)
	stripIfMasked(&c.Channels.Feishu.EncryptKey)
	stripIfMasked(&c.Channels.Feishu.VerificationToken)

	// TTS API keys
	stripIfMasked(&c.Tts.OpenAI.APIKey)
	stripIfMasked(&c.Tts.ElevenLabs.APIKey)
	stripIfMasked(&c.Tts.MiniMax.APIKey)

	// Tailscale auth key
	stripIfMasked(&c.Tailscale.AuthKey)
}

// ApplyDBSecrets overlays secrets from the config_secrets table onto the config.
// Called before ApplyEnvOverrides() — env vars take highest precedence.
// Precedence chain: config.json defaults → DB secrets → env vars.
func (c *Config) ApplyDBSecrets(secrets map[string]string) {
	apply := func(key string, dst *string) {
		if v, ok := secrets[key]; ok && v != "" {
			*dst = v
		}
	}

	apply("gateway.token", &c.Gateway.Token)
	apply("tts.openai.api_key", &c.Tts.OpenAI.APIKey)
	apply("tts.elevenlabs.api_key", &c.Tts.ElevenLabs.APIKey)
	apply("tts.minimax.api_key", &c.Tts.MiniMax.APIKey)
	apply("tts.minimax.group_id", &c.Tts.MiniMax.GroupID)
	apply("tailscale.auth_key", &c.Tailscale.AuthKey)
}

// ExtractDBSecrets returns the config_secrets key-value pairs from the config.
// Saves secrets to the config_secrets table.
func (c *Config) ExtractDBSecrets() map[string]string {
	secrets := make(map[string]string)

	collect := func(key, value string) {
		if value != "" && value != secretMask {
			secrets[key] = value
		}
	}

	collect("gateway.token", c.Gateway.Token)
	collect("tts.openai.api_key", c.Tts.OpenAI.APIKey)
	collect("tts.elevenlabs.api_key", c.Tts.ElevenLabs.APIKey)
	collect("tts.minimax.api_key", c.Tts.MiniMax.APIKey)
	collect("tts.minimax.group_id", c.Tts.MiniMax.GroupID)
	collect("tailscale.auth_key", c.Tailscale.AuthKey)

	return secrets
}

func maskNonEmpty(s *string) {
	if *s != "" {
		*s = secretMask
	}
}
