package pg

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestPGProviderStoreReadsNullableOptionalFields(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, _ := seedTenantAndAgent(t, db)
	providerID := uuid.New()
	providerName := "nullable-" + providerID.String()[:8]
	if _, err := db.Exec(
		`INSERT INTO llm_providers
			(id, tenant_id, name, display_name, provider_type, api_base, api_key, enabled, settings)
		 VALUES ($1, $2, $3, NULL, 'openrouter', NULL, 'sk-test', true, '{}')`,
		providerID, tenantID, providerName,
	); err != nil {
		t.Fatalf("seed nullable provider: %v", err)
	}

	providers, err := NewPGProviderStore(db, "").ListAllProviders(context.Background())
	if err != nil {
		t.Fatalf("ListAllProviders with nullable optional fields: %v", err)
	}
	for _, provider := range providers {
		if provider.ID != providerID {
			continue
		}
		if provider.DisplayName != "" || provider.APIBase != "" || provider.APIKey != "sk-test" || string(provider.Settings) != "{}" {
			t.Fatalf("nullable provider was not normalized: %+v", provider)
		}
		return
	}
	t.Fatalf("nullable provider %s not returned", providerName)
}
