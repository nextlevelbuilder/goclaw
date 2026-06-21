package config

import "encoding/json"

const (
	channelTypeTelegram            = "telegram"
	reasoningDeliveryKey           = "reasoning_delivery"
	legacyReasoningStreamKey       = "reasoning_stream"
	reasoningDeliveryOff           = "off"
	reasoningDeliveryStreamingOnly = "streaming_only"
	reasoningDeliveryAlwaysBubbles = "always_bubbles"
)

// NormalizeChannelInstanceConfigValue applies compatibility rewrites for
// channel-instance config payloads before they are persisted.
func NormalizeChannelInstanceConfigValue(channelType string, value any) any {
	if channelType != channelTypeTelegram || value == nil {
		return value
	}
	switch cfg := value.(type) {
	case json.RawMessage:
		return normalizeChannelConfigRaw(cfg)
	case []byte:
		return []byte(normalizeChannelConfigRaw(cfg))
	case map[string]any:
		return normalizeTelegramReasoningDeliveryMap(cfg)
	default:
		return value
	}
}

func NormalizeChannelInstanceConfigRaw(channelType string, raw json.RawMessage) json.RawMessage {
	if channelType != channelTypeTelegram {
		return raw
	}
	return normalizeChannelConfigRaw(raw)
}

func normalizeChannelConfigRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return raw
	}
	cfg = normalizeTelegramReasoningDeliveryMap(cfg)
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return raw
	}
	return encoded
}

func normalizeTelegramReasoningDeliveryMap(cfg map[string]any) map[string]any {
	if cfg == nil {
		return nil
	}
	_, hasMode := cfg[reasoningDeliveryKey]
	_, hasLegacy := cfg[legacyReasoningStreamKey]
	if !hasMode && !hasLegacy {
		return cfg
	}
	cfg[reasoningDeliveryKey] = resolveReasoningDeliveryMode(cfg)
	delete(cfg, legacyReasoningStreamKey)
	return cfg
}

func resolveReasoningDeliveryMode(cfg map[string]any) string {
	if mode, ok := cfg[reasoningDeliveryKey].(string); ok {
		switch mode {
		case reasoningDeliveryOff, reasoningDeliveryStreamingOnly, reasoningDeliveryAlwaysBubbles:
			return mode
		}
	}
	if legacy, ok := cfg[legacyReasoningStreamKey].(bool); ok && !legacy {
		return reasoningDeliveryOff
	}
	return reasoningDeliveryStreamingOnly
}
