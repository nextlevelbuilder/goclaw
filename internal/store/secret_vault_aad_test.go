package store

import (
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
)

func aadTestEntry() *SecretVaultEntry {
	return &SecretVaultEntry{
		ID:              uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		TenantID:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Name:            "github-token",
		Kind:            SecretKindAPIKey,
		AllowedDomains:  []string{"b.example.com", "A.example.com"},
		RequireApproval: true,
		Revision:        3,
	}
}

// Golden vector computed independently (Python) from the layout in the design:
// label || 0x00 || uint32_be(len) || field, for each bound field in order.
// PG store, SQLite store and the rewrap command must all produce these bytes.
const secretAADGoldenHex = "676f636c61772f7365637265742d7661756c742f7632" +
	"000000002431313131313131312d313131312d313131312d313131312d313131313131313131313131" +
	"000000002432323232323232322d323232322d323232322d323232322d323232323232323232323232" +
	"000000000c6769746875622d746f6b656e" +
	"00000000076170695f6b6579" +
	"000000004035633632666362343435353939663566656137333730616539663262653139623432316164613936333831656237303663306135623139643632356264313732" +
	"00000000013100000000013" + "3"

func TestSecretPayloadAAD_GoldenVector(t *testing.T) {
	got := hex.EncodeToString(SecretPayloadAAD(aadTestEntry()))
	if got != secretAADGoldenHex {
		t.Fatalf("AAD layout changed:\n got  %s\n want %s", got, secretAADGoldenHex)
	}
}

func TestSecretPayloadAAD_BindsEveryField(t *testing.T) {
	base := hex.EncodeToString(SecretPayloadAAD(aadTestEntry()))
	mutations := map[string]func(*SecretVaultEntry){
		"tenant":   func(e *SecretVaultEntry) { e.TenantID = uuid.New() },
		"id":       func(e *SecretVaultEntry) { e.ID = uuid.New() },
		"name":     func(e *SecretVaultEntry) { e.Name = "other" },
		"kind":     func(e *SecretVaultEntry) { e.Kind = SecretKindLogin },
		"domains":  func(e *SecretVaultEntry) { e.AllowedDomains = append(e.AllowedDomains, "attacker.example") },
		"approval": func(e *SecretVaultEntry) { e.RequireApproval = false },
		"revision": func(e *SecretVaultEntry) { e.Revision = 4 },
	}
	for field, mutate := range mutations {
		e := aadTestEntry()
		mutate(e)
		if hex.EncodeToString(SecretPayloadAAD(e)) == base {
			t.Errorf("changing %s does not change the AAD", field)
		}
	}
}

func TestSecretPayloadAAD_IgnoresHintsAndDomainOrder(t *testing.T) {
	base := hex.EncodeToString(SecretPayloadAAD(aadTestEntry()))
	e := aadTestEntry()
	e.Hints = "advisory text"
	e.AllowedDomains = []string{" a.EXAMPLE.com", "b.example.com "}
	if hex.EncodeToString(SecretPayloadAAD(e)) != base {
		t.Fatal("hints, order and case/whitespace of domains must not change the AAD")
	}
}
