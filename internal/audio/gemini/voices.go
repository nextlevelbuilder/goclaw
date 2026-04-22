package gemini

import "github.com/nextlevelbuilder/goclaw/internal/audio"

// geminiVoices is the static catalog of 30 Gemini prebuilt voices.
// Source: https://ai.google.dev/gemini-api/docs/speech-generation (April 2026)
//
// Google does NOT publish gender for these voices — by design, to keep voice
// selection inclusive. Only style descriptors are official. We surface the
// style as a label so users can pick by character (Bright/Firm/Smooth/etc.).
func styleLabel(style string) map[string]string { return map[string]string{"style": style} }

var geminiVoices = []audio.VoiceOption{
	{VoiceID: "Zephyr", Name: "Zephyr", Labels: styleLabel("Bright")},
	{VoiceID: "Puck", Name: "Puck", Labels: styleLabel("Upbeat")},
	{VoiceID: "Charon", Name: "Charon", Labels: styleLabel("Informative")},
	{VoiceID: "Kore", Name: "Kore", Labels: styleLabel("Firm")},
	{VoiceID: "Fenrir", Name: "Fenrir", Labels: styleLabel("Excitable")},
	{VoiceID: "Leda", Name: "Leda", Labels: styleLabel("Youthful")},
	{VoiceID: "Orus", Name: "Orus", Labels: styleLabel("Firm")},
	{VoiceID: "Aoede", Name: "Aoede", Labels: styleLabel("Breezy")},
	{VoiceID: "Callirrhoe", Name: "Callirrhoe", Labels: styleLabel("Easy-going")},
	{VoiceID: "Autonoe", Name: "Autonoe", Labels: styleLabel("Bright")},
	{VoiceID: "Enceladus", Name: "Enceladus", Labels: styleLabel("Breathy")},
	{VoiceID: "Iapetus", Name: "Iapetus", Labels: styleLabel("Clear")},
	{VoiceID: "Umbriel", Name: "Umbriel", Labels: styleLabel("Easy-going")},
	{VoiceID: "Algieba", Name: "Algieba", Labels: styleLabel("Smooth")},
	{VoiceID: "Despina", Name: "Despina", Labels: styleLabel("Smooth")},
	{VoiceID: "Erinome", Name: "Erinome", Labels: styleLabel("Clear")},
	{VoiceID: "Algenib", Name: "Algenib", Labels: styleLabel("Gravelly")},
	{VoiceID: "Rasalgethi", Name: "Rasalgethi", Labels: styleLabel("Informative")},
	{VoiceID: "Laomedeia", Name: "Laomedeia", Labels: styleLabel("Upbeat")},
	{VoiceID: "Achernar", Name: "Achernar", Labels: styleLabel("Soft")},
	{VoiceID: "Alnilam", Name: "Alnilam", Labels: styleLabel("Firm")},
	{VoiceID: "Schedar", Name: "Schedar", Labels: styleLabel("Even")},
	{VoiceID: "Gacrux", Name: "Gacrux", Labels: styleLabel("Mature")},
	{VoiceID: "Pulcherrima", Name: "Pulcherrima", Labels: styleLabel("Forward")},
	{VoiceID: "Achird", Name: "Achird", Labels: styleLabel("Friendly")},
	{VoiceID: "Zubenelgenubi", Name: "Zubenelgenubi", Labels: styleLabel("Casual")},
	{VoiceID: "Vindemiatrix", Name: "Vindemiatrix", Labels: styleLabel("Gentle")},
	{VoiceID: "Sadachbia", Name: "Sadachbia", Labels: styleLabel("Lively")},
	{VoiceID: "Sadaltager", Name: "Sadaltager", Labels: styleLabel("Knowledgeable")},
	{VoiceID: "Sulafat", Name: "Sulafat", Labels: styleLabel("Warm")},
}

// defaultVoice is the voice used when none is specified.
const defaultVoice = "Kore"

// validVoiceSet is a fast-lookup set built from geminiVoices at init time.
var validVoiceSet map[string]struct{}

func init() {
	validVoiceSet = make(map[string]struct{}, len(geminiVoices))
	for _, v := range geminiVoices {
		validVoiceSet[v.VoiceID] = struct{}{}
	}
}

// isValidVoice reports whether name is a known Gemini prebuilt voice.
func isValidVoice(name string) bool {
	_, ok := validVoiceSet[name]
	return ok
}
