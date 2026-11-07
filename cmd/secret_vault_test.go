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
func (c *cliVault) ReplaceValue(_ context.Context, id uuid.UUID, p store.SecretVaultPayload, _ int64) (int64, error) {
	c.values[id] = p.Secret
	return 2, nil
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

type rewrapVault struct {
	store.SecretVaultStore
	report store.SecretRewrapReport
	dryRun bool
}

func (r *rewrapVault) RewrapSecrets(_ context.Context, dryRun bool) (store.SecretRewrapReport, error) {
	r.dryRun = dryRun
	return r.report, nil
}

func TestSecretVaultCLI_RewrapReportsCountsAndFailsOnUnreadable(t *testing.T) {
	bad := uuid.New()
	st := &rewrapVault{report: store.SecretRewrapReport{Total: 4, Rewrapped: 2, Current: 1, Unreadable: []uuid.UUID{bad}}}
	var out strings.Builder
	err := rewrapSecretVault(context.Background(), st, true, &out)
	if err == nil {
		t.Fatal("unreadable secrets must produce a non-zero exit")
	}
	if !st.dryRun {
		t.Error("dry-run flag was not passed to the store")
	}
	for _, want := range []string{"Would rewrap 2 of 4", "1 already bound", "unreadable: " + bad.String()} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q missing %q", out.String(), want)
		}
	}

	st = &rewrapVault{report: store.SecretRewrapReport{Total: 2, Rewrapped: 2}}
	out.Reset()
	if err := rewrapSecretVault(context.Background(), st, false, &out); err != nil || !strings.Contains(out.String(), "Rewrapped 2 of 2") {
		t.Fatalf("clean run = %q, %v", out.String(), err)
	}
}
