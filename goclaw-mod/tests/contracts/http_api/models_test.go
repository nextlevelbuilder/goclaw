//go:build integration

package http_api

import "testing"

// CONTRACT: /v1/providers response MUST include providers array.
func TestContract_HTTP_ProvidersList(t *testing.T) {
	baseURL, token := getTestServer(t)
	client := newHTTPClient(baseURL, token)

	resp := client.get(t, "/v1/providers")

	assertField(t, resp, "providers", "array")

	providers, ok := resp["providers"].([]any)
	if !ok || len(providers) == 0 {
		t.Log("No providers - skipping field checks")
		return
	}

	provider := providers[0].(map[string]any)
	assertField(t, provider, "id", "string")
	assertField(t, provider, "name", "string")
	assertField(t, provider, "type", "string")
}
