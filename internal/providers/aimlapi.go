package providers

const (
	AIMLAPIDefaultAPIBase = "https://api.aimlapi.com/v1"
	AIMLAPIDefaultModel   = "openai/gpt-5-chat"
)

var aimlapiChatModels = []string{
	AIMLAPIDefaultModel,
	"openai/gpt-4o-mini",
	"anthropic/claude-sonnet-4.5",
	"google/gemini-2.5-flash",
}

// AIMLAPIChatModels returns the curated text model catalog shown in provider setup.
func AIMLAPIChatModels() []string {
	return append([]string(nil), aimlapiChatModels...)
}

// NewAIMLAPIProvider creates the branded OpenAI-compatible provider and applies
// the attribution headers required on every inference request.
func NewAIMLAPIProvider(name, apiKey, apiBase string) *OpenAIProvider {
	if apiBase == "" {
		apiBase = AIMLAPIDefaultAPIBase
	}
	return NewOpenAIProvider(name, apiKey, apiBase, AIMLAPIDefaultModel).
		WithExtraHeaders(map[string]string{
			"X-AIMLAPI-Partner-ID":          "nextlevelbuilder",
			"X-AIMLAPI-Integration-Repo":    "nextlevelbuilder/goclaw",
			"X-AIMLAPI-Integration-Version": "1.0.0",
		})
}
