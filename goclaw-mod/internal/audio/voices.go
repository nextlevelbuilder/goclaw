package audio

// Voice represents a single TTS voice as returned by a provider's voice-list
// endpoint (e.g. ElevenLabs GET /v1/voices). Kept provider-agnostic so HTTP/WS
// handlers can depend on the audio package without importing sub-providers.
type Voice struct {
	ID         string            `json:"voice_id"`
	Name       string            `json:"name"`
	Labels     map[string]string `json:"labels"`
	PreviewURL string            `json:"preview_url"`
	Category   string            `json:"category"` // "premade" | "cloned" | "generated"
}
