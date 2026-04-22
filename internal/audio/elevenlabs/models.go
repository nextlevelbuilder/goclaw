package elevenlabs

import (
	"errors"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
)

// AllowedElevenLabsModels is the allowlist of ElevenLabs TTS model IDs the
// gateway will forward. Keeping this explicit prevents operators from pasting
// model aliases that silently fall back to the provider's default model.
//
// Refs: https://elevenlabs.io/docs/api-reference/models
var AllowedElevenLabsModels = map[string]struct{}{
	"eleven_v3":              {}, // latest flagship (Nov 2025)
	"eleven_flash_v2_5":      {}, // low-latency, 32 languages
	"eleven_multilingual_v2": {}, // high-quality multilingual
	"eleven_turbo_v2_5":      {}, // cost-optimized, fast
}

// ValidateModel checks that id is on the allowlist. Empty id is treated as
// "use configured default" and returns nil. Unknown ids surface an i18n
// error keyed by i18n.MsgTtsUnknownModel so the gateway can localize the
// message before returning it to the caller.
func ValidateModel(id string) error {
	if id == "" {
		return nil
	}
	if _, ok := AllowedElevenLabsModels[id]; ok {
		return nil
	}
	// English fallback — callers with a locale should wrap via i18n.T at the
	// boundary. Model id is embedded so operators can spot typos in logs.
	return errors.New(i18n.T("en", i18n.MsgTtsUnknownModel, id))
}
