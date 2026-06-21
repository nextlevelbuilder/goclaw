package cmd

import "testing"

func TestSetupAgentCreatePayloadIncludesGatewayOperatorConsentOnlyWhenTrue(t *testing.T) {
	withoutConsent := setupAgentCreatePayload("assistant", "Assistant", "predefined", "anthropic", "claude", false)
	if _, ok := withoutConsent["grant_gateway_operator_access"]; ok {
		t.Fatalf("grant_gateway_operator_access must be absent without explicit consent: %#v", withoutConsent)
	}

	withConsent := setupAgentCreatePayload("assistant", "Assistant", "predefined", "anthropic", "claude", true)
	if got, ok := withConsent["grant_gateway_operator_access"].(bool); !ok || !got {
		t.Fatalf("grant_gateway_operator_access missing after consent: %#v", withConsent)
	}
}
