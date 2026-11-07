package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/keyrotation"
)

const (
	keysTestOld = "0123456789abcdef0123456789abcdef"
	keysTestNew = "fedcba9876543210fedcba9876543210"
)

func TestKeysAddPrintsKeyIDNotKey(t *testing.T) {
	crypto.SetKeyring(nil)
	c := keysAddCmd()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetArgs([]string{"--key", keysTestNew})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	kid, _ := crypto.KeyID(keysTestNew)
	if !strings.Contains(out.String(), kid+"=<the key>") || !strings.Contains(out.String(), "rotate --to "+kid) {
		t.Fatalf("output = %q", out.String())
	}
	if strings.Contains(out.String(), keysTestNew) {
		t.Fatal("add must not echo the key it was given")
	}
}

func TestKeysAddRejectsBadAndDuplicateKeys(t *testing.T) {
	kidOld, _ := crypto.KeyID(keysTestOld)
	kr, err := crypto.NewKeyring([]string{keysTestOld}, kidOld)
	if err != nil {
		t.Fatal(err)
	}
	crypto.SetKeyring(kr)
	t.Cleanup(func() { crypto.SetKeyring(nil) })

	for name, args := range map[string][]string{
		"short key":     {"--key", "short"},
		"already known": {"--key", keysTestOld},
		"nothing given": {},
	} {
		c := keysAddCmd()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs(args)
		if err := c.Execute(); err == nil {
			t.Errorf("%s: want an error", name)
		} else if strings.Contains(err.Error(), keysTestOld) {
			t.Errorf("%s: error leaks key material", name)
		}
	}
}

func TestKeysAddGenerateShowsKeyOnce(t *testing.T) {
	crypto.SetKeyring(nil)
	c := keysAddCmd()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetArgs([]string{"--generate"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Generated key") || !strings.Contains(out.String(), "Key id:") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRenderKeysStatusListsKidsAndCountsOnly(t *testing.T) {
	kidOld, _ := crypto.KeyID(keysTestOld)
	kidNew, _ := crypto.KeyID(keysTestNew)
	kr, err := crypto.NewKeyring([]string{keysTestOld, keysTestNew}, kidNew)
	if err != nil {
		t.Fatal(err)
	}
	statuses := []keyrotation.ColumnStatus{
		{Target: keyrotation.Target{Table: "llm_providers", Column: "api_key"}, Legacy: 2, ByKeyID: map[string]int{kidNew: 3}},
		{Target: keyrotation.Target{Table: "empty", Column: "x"}, Empty: 4, ByKeyID: map[string]int{}},
	}
	var out bytes.Buffer
	renderKeysStatus(&out, kr, statuses, vaultCounts{ByKeyID: map[string]int{kidOld: 1}, V2: 5})
	got := out.String()
	for _, want := range []string{kidOld, kidNew + " (active)", "llm_providers.api_key", kidNew + "=3", "secret_vault_entries"} {
		if !strings.Contains(got, want) {
			t.Errorf("status output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "empty.x") {
		t.Error("columns with no values should be omitted")
	}
	if strings.Contains(got, keysTestOld) || strings.Contains(got, keysTestNew) {
		t.Fatal("status leaks key material")
	}
}
