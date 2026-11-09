package providers

import "net/url"

// isMiniMaxEndpoint keeps native-only wire options away from gateways and
// unrelated OpenAI-compatible providers, even when their display name matches.
func (p *OpenAIProvider) isMiniMaxEndpoint() bool {
	if p.providerType == "minimax_native" {
		return true
	}
	u, err := url.Parse(p.apiBase)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "api.minimax.io", "api.minimaxi.com":
		return true
	}
	return false
}
