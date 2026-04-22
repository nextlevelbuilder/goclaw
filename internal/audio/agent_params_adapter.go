package audio

import (
	"fmt"
	"log/slog"
)

// AgentTTSParamsAllowedKeys is the allow-list of generic keys that agents may
// store in other_config.tts_params. Any key outside this set is rejected at
// write time and silently dropped here for defense-in-depth.
// Both internal/http and internal/gateway/methods import this to avoid
// duplicating the literal (Action D: DRY).
var AgentTTSParamsAllowedKeys = map[string]bool{
	"speed":   true,
	"emotion": true,
	"style":   true,
}

// agentOverrideKeys aliases the exported map for internal use in AdaptAgentParams.
var agentOverrideKeys = AgentTTSParamsAllowedKeys

// ValidateAgentTTSParams returns an error if ttsParams contains any key not in
// the allow-list. Values are not type-checked here — providers handle coercion
// at synthesis time.
func ValidateAgentTTSParams(ttsParams map[string]any) error {
	for k := range ttsParams {
		if !AgentTTSParamsAllowedKeys[k] {
			return fmt.Errorf("tts_params key %q is not allowed; valid keys: speed, emotion, style", k)
		}
	}
	return nil
}

// AdaptAgentParams maps generic agent override keys (stored in
// agents.other_config.tts_params) to the provider-specific param keys that
// each provider's Synthesize implementation expects in opts.Params.
//
// Only the three allow-listed generic keys (speed, emotion, style) are
// translated. Unknown generic keys are silently dropped — defense-in-depth
// against DB state from before the allow-list was enforced.
//
// Absent generic keys are never written to the output map, so callers that
// merge the result into opts.Params do not accidentally zero-out provider
// defaults.
//
// CRITICAL (Finding #1): This must be called PER-ATTEMPT inside the fallback
// loop, not once before. Each attempt may use a different provider, and the
// mapping is provider-specific.
//
// Adapter table:
//
//	generic key | openai      | elevenlabs             | edge | minimax | gemini
//	------------|-------------|------------------------|------|---------|-------
//	speed       | speed       | voice_settings.speed   | skip | speed   | skip
//	emotion     | skip        | skip                   | skip | emotion | skip
//	style       | skip        | voice_settings.style   | skip | skip    | skip
func AdaptAgentParams(generic map[string]any, provider string) map[string]any {
	if len(generic) == 0 {
		return nil
	}

	out := make(map[string]any, len(generic))

	switch provider {
	case "openai":
		if v, ok := generic["speed"]; ok {
			out["speed"] = v
		}
	case "elevenlabs":
		if v, ok := generic["speed"]; ok {
			out["voice_settings.speed"] = v
		}
		if v, ok := generic["style"]; ok {
			out["voice_settings.style"] = v
		}
	case "edge":
		// No compatible generic keys — edge uses "rate" in a different semantic range.
	case "minimax":
		if v, ok := generic["speed"]; ok {
			out["speed"] = v
		}
		if v, ok := generic["emotion"]; ok {
			out["emotion"] = v
		}
	case "gemini":
		// No compatible generic keys — Gemini uses audio tags in prompt text.
	default:
		// Unknown provider — return empty map. Log at Info for observability.
		slog.Info("tts.agent.params.dropped", "provider", provider, "reason", "unknown provider")
		return nil
	}

	// Log dropped keys (generic keys present but not mapped for this provider).
	if len(generic) > 0 && len(out) == 0 {
		slog.Info("tts.agent.params.dropped", "provider", provider, "dropped_keys", genericKeys(generic))
	} else if len(generic) > len(out) {
		// Some keys were mapped, some were dropped.
		var dropped []string
		for k := range generic {
			if agentOverrideKeys[k] {
				// Check if it landed in out (by checking the possible output key for this provider)
				found := false
				switch provider {
				case "openai":
					found = k == "speed"
				case "elevenlabs":
					found = k == "speed" || k == "style"
				case "minimax":
					found = k == "speed" || k == "emotion"
				}
				if !found {
					dropped = append(dropped, k)
				}
			}
		}
		if len(dropped) > 0 {
			slog.Info("tts.agent.params.dropped", "provider", provider, "dropped_keys", dropped)
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// genericKeys returns the keys of the map as a slice (for logging).
func genericKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
