package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const cliSecretValue = "cli-secret-value-5521"

type cliVault struct {
	store.SecretVaultStore
	entries []store.SecretVaultEntry
	values  map[uuid.UUID]string
	audit   []string
}

func (c *cliVault) Create(_ context.Context, e *store.SecretVaultEntry, p store.SecretVaultPayload) error {
	for _, x := range c.entries {
		if x.Name == e.Name {
			return store.ErrSecretVaultNameTaken
		}
	}
	e.ID = uuid.New()
	c.entries = append(c.entries, *e)
	c.values[e.ID] = p.Secret
	return nil
}
func (c *cliVault) List(context.Context) ([]store.SecretVaultEntry, error) { return c.entries, nil }
func (c *cliVault) ReplaceValue(_ context.Context, id uuid.UUID, p store.SecretVaultPayload) error {
	c.values[id] = p.Secret
	return nil
}
func (c *cliVault) AppendAudit(_ context.Context, a *store.SecretVaultAuditEntry) error {
	c.audit = append(c.audit, a.Action)
	return nil
}

func TestSecretVaultCLI_SetListNeverPrintValue(t *testing.T) {
	st := &cliVault{values: map[uuid.UUID]string{}}
	ctx := store.WithUserID(context.Background(), "cli")
	o := secretVaultSetOpts{Name: "gh", Kind: store.SecretKindAPIKey, Domains: []string{"api.github.com"}}

	msg, err := setSecretVault(ctx, st, o, cliSecretValue)
	if err != nil || !strings.Contains(msg, "Created") {
		t.Fatalf("create: %q %v", msg, err)
	}
	msg2, err := setSecretVault(ctx, st, o, cliSecretValue+"-2")
	if err != nil || !strings.Contains(msg2, "Replaced") {
		t.Fatalf("replace: %q %v", msg2, err)
	}
	if len(st.entries) != 1 || st.values[st.entries[0].ID] != cliSecretValue+"-2" {
		t.Fatal("value not replaced in place")
	}

	var out bytes.Buffer
	if err := listSecretVault(ctx, st, &out); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{msg, msg2, out.String()} {
		if strings.Contains(s, cliSecretValue) {
			t.Fatalf("output leaks the value: %q", s)
		}
	}
	if !strings.Contains(out.String(), "gh") || strings.Join(st.audit, ",") != "created,updated" {
		t.Errorf("list %q audit %v", out.String(), st.audit)
	}
}

func TestReadSecretLine(t *testing.T) {
	if v, err := readSecretLine(strings.NewReader(cliSecretValue + "\r\nrest")); err != nil || v != cliSecretValue {
		t.Errorf("got %q %v", v, err)
	}
	if v, err := readSecretLine(strings.NewReader(cliSecretValue)); err != nil || v != cliSecretValue {
		t.Errorf("no trailing newline: %q %v", v, err)
	}
	if _, err := readSecretLine(strings.NewReader("")); err == nil {
		t.Error("empty stdin must be an error")
	}
}
