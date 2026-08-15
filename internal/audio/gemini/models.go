package gemini

import "slices"

// geminiModels is the static catalog of supported Gemini TTS model IDs.
// Source: https://ai.google.dev/gemini-api/docs/speech-generation (April 2026)
var geminiModels = []string{
	"gemini-3.1-flash-tts-preview",
	"gemini-2.5-flash-preview-tts",
	"gemini-2.5-pro-preview-tts",
}

// defaultModel is the model used when none is specified.
const defaultModel = "gemini-3.1-flash-tts-preview"

// isValidModel reports whether id is in the static model catalog.
func isValidModel(id string) bool {
	return slices.Contains(geminiModels, id)
}
