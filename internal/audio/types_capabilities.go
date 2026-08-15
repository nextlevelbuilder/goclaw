package audio

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ParamType identifies how a ParamSchema field should be rendered in the UI.
type ParamType string

const (
	ParamTypeRange   ParamType = "range"   // float slider with Min/Max/Step
	ParamTypeNumber  ParamType = "number"  // free-form float input
	ParamTypeInteger ParamType = "integer" // integer input
	ParamTypeEnum    ParamType = "enum"    // dropdown from Enum list
	ParamTypeBoolean ParamType = "boolean" // toggle
	ParamTypeString  ParamType = "string"  // single-line text
	ParamTypeText    ParamType = "text"    // multi-line text area
)

// EnumOption is a single option in a ParamTypeEnum field.
type EnumOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Dependency declares a conditional visibility rule for a ParamSchema field.
// A field is visible only when ALL its DependsOn entries are satisfied (AND semantics).
type Dependency struct {
	// Field is the sibling param key this dependency references.
	Field string `json:"field"`
	// Op is the comparison operator; currently only "eq" is supported.
	Op string `json:"op"`
	// Value is the expected value (string comparison after fmt.Sprint serialization).
	Value any `json:"value"`
}

// VoiceOption is a static voice entry exposed in ProviderCapabilities.
//
// Labels carries provider-specific descriptors (e.g. {"style": "Bright"} for
// Gemini, {"gender": "female", "accent": "american"} for ElevenLabs). Frontend
// renders the first 1–2 entries as badges next to the voice name.
type VoiceOption struct {
	VoiceID  string            `json:"voice_id"`
	Name     string            `json:"name"`
	Language string            `json:"language,omitempty"`
	Gender   string            `json:"gender,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// ParamSchema describes a single configurable parameter for a TTS provider.
// The frontend DynamicParamForm uses this to render the appropriate input control.
type ParamSchema struct {
	// Key is the dot-separated param path, e.g. "stability" or "voice_settings.stability".
	Key         string       `json:"key"`
	Type        ParamType    `json:"type"`
	Label       string       `json:"label"`
	Description string       `json:"description,omitempty"`
	Default     any          `json:"default,omitempty"`
	Min         *float64     `json:"min,omitempty"`  // for range/number
	Max         *float64     `json:"max,omitempty"`  // for range/number
	Step        *float64     `json:"step,omitempty"` // for range
	Enum        []EnumOption `json:"enum,omitempty"`
	DependsOn   []Dependency `json:"depends_on,omitempty"`
	// Group categorises the param for collapsible UI sections.
	// "" (empty) = basic (always visible); "advanced" = collapsed by default.
	Group string `json:"group,omitempty"`
	// AgentOverridableAs, when non-empty, is the generic key alias that this
	// param maps to in agents.other_config.tts_params (e.g. "speed", "emotion",
	// "style"). Empty string means this param is NOT agent-overridable.
	// Frontend filters to non-empty AgentOverridableAs params only, so it never
	// shows non-overridable knobs in the agent override UI.
	// The value doubles as the conversion key: UI uses it to map between the
	// native capability key (e.g. "voice_settings.speed") and the generic stored
	// key (e.g. "speed") without needing a separate hard-coded lookup table.
	AgentOverridableAs string `json:"agent_overridable_as,omitempty"`
}

// ProviderCapabilities is the catalog entry for a single TTS provider.
// Returned by GET /v1/tts/capabilities in a { providers: [] } envelope.
type ProviderCapabilities struct {
	// Provider is the stable machine identifier (e.g. "openai", "elevenlabs").
	Provider string `json:"provider"`
	// DisplayName is the human-readable label shown in the UI.
	DisplayName string `json:"display_name"`
	// RequiresAPIKey indicates whether the provider needs an API key configured.
	RequiresAPIKey bool `json:"requires_api_key"`
	// Models is the list of model IDs supported by this provider.
	Models []string `json:"models,omitempty"`
	// Voices is a static catalog of voices (nil = dynamic / fetched separately).
	Voices []VoiceOption `json:"voices,omitempty"`
	// Params is the ordered list of configurable parameters (nil = no custom params).
	Params []ParamSchema `json:"params,omitempty"`
	// CustomFeatures is an opaque map for provider-specific UI hints.
	CustomFeatures map[string]any `json:"custom_features,omitempty"`
}

// DescribableProvider is the optional interface that TTS providers implement
// to expose their capability schema. Providers that do not implement this
// contribute a minimal stub { Provider, DisplayName } to ListCapabilities.
type DescribableProvider interface {
	TTSProvider
	Capabilities() ProviderCapabilities
}

// evaluateDependsOn returns true if all deps are satisfied by formState (AND semantics).
// An empty deps slice always returns true (unconditionally visible).
func evaluateDependsOn(deps []Dependency, formState map[string]any) bool {
	for _, d := range deps {
		v, ok := formState[d.Field]
		if !ok {
			return false
		}
		if fmt.Sprint(v) != fmt.Sprint(d.Value) {
			return false
		}
	}
	return true
}

// parseKeyPath splits a dot-separated key path into segments.
// Returns an error if the path is empty or contains empty segments.
func parseKeyPath(key string) ([]string, error) {
	if key == "" {
		return nil, errors.New("key path must not be empty")
	}
	parts := strings.Split(key, ".")
	if slices.Contains(parts, "") {
		return nil, errors.New("key path must not contain empty segments: " + key)
	}
	return parts, nil
}
