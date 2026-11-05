package cmd

import (
	"bytes"
	"context"
	"errors"
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

func (c *cliVault) Approve(_ context.Context, id uuid.UUID, domains []string) error {
	norm, err := store.NormalizeSecretDomains(domains)
	if err != nil {
		return err
	}
	for i := range c.entries {
		if c.entries[i].ID != id {
			continue
		}
		if c.entries[i].Status != store.SecretVaultStatusPending {
			return store.ErrSecretVaultNotPending
		}
		c.entries[i].Status, c.entries[i].AllowedDomains = store.SecretVaultStatusActive, norm
		c.audit = append(c.audit, store.SecretAuditApproved)
		return nil
	}
	return store.ErrSecretVaultNotFound
}

func TestSecretVaultCLI_ListShowsStatusAndApprove(t *testing.T) {
	agent := uuid.New()
	st := &cliVault{values: map[uuid.UUID]string{}, entries: []store.SecretVaultEntry{
		{ID: uuid.New(), Name: "mine", Kind: store.SecretKindAPIKey, AllowedDomains: []string{"a.com"},
			Status: store.SecretVaultStatusActive, CreatedVia: store.SecretVaultCreatedViaUser},
		{ID: uuid.New(), Name: "captured", Kind: store.SecretKindAPIKey, AllowedDomains: []string{"proposed.com"},
			Status: store.SecretVaultStatusPending, CreatedVia: store.SecretVaultCreatedViaAgent, CreatedByAgent: &agent},
	}}
	ctx := store.WithUserID(context.Background(), "cli")

	var out bytes.Buffer
	if err := listSecretVault(ctx, st, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if !strings.Contains(lines[0], "STATUS") || !strings.Contains(lines[0], "AGENT") {
		t.Errorf("header lacks status/agent: %q", lines[0])
	}
	if !strings.Contains(out.String(), "pending") || !strings.Contains(out.String(), agent.String()) {
		t.Errorf("list must show the pending entry and its agent:\n%s", out.String())
	}

	if _, err := approveSecretVault(ctx, st, "captured", nil); err == nil {
		t.Error("approve without domains must fail")
	}
	if _, err := approveSecretVault(ctx, st, "missing", []string{"b.com"}); err == nil {
		t.Error("approve of an unknown name must fail")
	}
	msg, err := approveSecretVault(ctx, st, "captured", []string{"B.com"})
	if err != nil || !strings.Contains(msg, "Approved") {
		t.Fatalf("approve: %q %v", msg, err)
	}
	if st.entries[1].Status != store.SecretVaultStatusActive || strings.Join(st.entries[1].AllowedDomains, ",") != "b.com" {
		t.Errorf("entry after approve = %+v", st.entries[1])
	}
	if _, err := approveSecretVault(ctx, st, "mine", []string{"a.com"}); !errors.Is(err, store.ErrSecretVaultNotPending) {
		t.Errorf("approving an active entry = %v, want ErrSecretVaultNotPending", err)
	}
	// The store writes the approved audit row; the CLI must not add another.
	if strings.Join(st.audit, ",") != "approved" {
		t.Errorf("audit = %v", st.audit)
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
