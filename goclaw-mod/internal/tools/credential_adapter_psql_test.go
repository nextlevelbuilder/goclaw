package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"runtime"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func psqlAdapterInstance(t *testing.T) CredentialAdapter {
	t.Helper()
	a := AdapterFor("psql")
	if a.Name() != "psql" {
		t.Fatalf("psql adapter not registered; AdapterFor(psql)=%q", a.Name())
	}
	return a
}

func TestPsqlAdapter_RegisteredAndShouldInject(t *testing.T) {
	a := psqlAdapterInstance(t)
	if !a.ShouldInject(nil) || !a.ShouldInject([]string{"-c", "select 1"}) {
		t.Fatalf("psql ShouldInject must always return true")
	}
}

func TestPsqlAdapter_LegacyEnvPassthrough(t *testing.T) {
	a := psqlAdapterInstance(t)

	// No credential → passthrough Injection, no error.
	inj, err := a.Prepare(context.Background(), &store.SecureCLIBinary{}, nil, nil)
	if err != nil || inj == nil || len(inj.Env) != 0 {
		t.Fatalf("nil cred should be no-op: inj=%+v err=%v", inj, err)
	}

	// Cred with credential_type=NULL (legacy env-vars) → passthrough.
	inj, err = a.Prepare(context.Background(), &store.SecureCLIBinary{}, &store.SecureCLIUserCredential{
		EncryptedEnv: []byte(`{"PGPASSWORD":"x"}`),
	}, nil)
	if err != nil {
		t.Fatalf("legacy env cred raised err: %v", err)
	}
	if inj == nil || len(inj.Env) != 0 || inj.Cleanup != nil {
		t.Fatalf("legacy env cred must produce empty Injection, got %+v", inj)
	}
}

func TestPsqlAdapter_PreparePgPasswordFile(t *testing.T) {
	a := psqlAdapterInstance(t)
	credType := "pg_password_file"
	blob, _ := json.Marshal(pgPasswordFile{
		Host:     "db.internal",
		Port:     "5432",
		Database: "prod",
		User:     "app",
		Password: "s3cret!",
	})
	inj, err := a.Prepare(context.Background(), &store.SecureCLIBinary{}, &store.SecureCLIUserCredential{
		CredentialType: &credType,
		EncryptedEnv:   blob,
	}, nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if inj == nil {
		t.Fatalf("Prepare returned nil Injection")
	}
	t.Cleanup(func() {
		if inj.Cleanup != nil {
			_ = inj.Cleanup()
		}
	})

	path, ok := inj.Env["PGPASSFILE"]
	if !ok || path == "" {
		t.Fatalf("PGPASSFILE missing from Injection.Env: %v", inj.Env)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	want := "db.internal:5432:prod:app:s3cret!\n"
	if string(got) != want {
		t.Fatalf(".pgpass line=%q, want %q", got, want)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("file mode=%o, want 0600", perm)
		}
	}
	// ScrubValues must include both the password AND the on-disk tmpfile path
	// (psql echoes `could not open password file "<path>"` on IO errors).
	if len(inj.ScrubValues) != 2 || inj.ScrubValues[0] != "s3cret!" || inj.ScrubValues[1] != path {
		t.Fatalf("ScrubValues=%v, want [s3cret! %s]", inj.ScrubValues, path)
	}
	if inj.Cleanup == nil {
		t.Fatalf("Cleanup missing")
	}
	if err := inj.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file still exists after cleanup")
	}
	// argv prefix must be empty — psql uses env only.
	if len(inj.ArgvPrefix) != 0 {
		t.Fatalf("psql adapter must not set ArgvPrefix, got %v", inj.ArgvPrefix)
	}
}

func TestPsqlAdapter_EscapesColonsAndBackslashes(t *testing.T) {
	a := psqlAdapterInstance(t)
	credType := "pg_password_file"
	// Adversarial password: contains both : and \.
	// .pgpass spec: escape backslash first, then colon.
	//   p: ab:cd → ab\:cd
	//   p: x\y   → x\\y
	//   combined: x\:y → x\\\:y
	blob, _ := json.Marshal(pgPasswordFile{
		Host:     "h",
		Port:     "5432",
		Database: "d",
		User:     "u",
		Password: `x\:y`,
	})
	inj, err := a.Prepare(context.Background(), &store.SecureCLIBinary{}, &store.SecureCLIUserCredential{
		CredentialType: &credType,
		EncryptedEnv:   blob,
	}, nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	t.Cleanup(func() {
		if inj.Cleanup != nil {
			_ = inj.Cleanup()
		}
	})

	got, err := os.ReadFile(inj.Env["PGPASSFILE"])
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := `h:5432:d:u:x\\\:y` + "\n"
	if string(got) != want {
		t.Fatalf("escaped line=%q, want %q", got, want)
	}
}

func TestPsqlAdapter_RejectsEmptyPassword(t *testing.T) {
	a := psqlAdapterInstance(t)
	credType := "pg_password_file"
	blob, _ := json.Marshal(pgPasswordFile{Host: "h", Port: "5432", Database: "d", User: "u"})
	if _, err := a.Prepare(context.Background(), &store.SecureCLIBinary{}, &store.SecureCLIUserCredential{
		CredentialType: &credType,
		EncryptedEnv:   blob,
	}, nil); err == nil {
		t.Fatalf("expected error for empty password")
	}
}

func TestPsqlAdapter_RejectsInvalidJSON(t *testing.T) {
	a := psqlAdapterInstance(t)
	credType := "pg_password_file"
	if _, err := a.Prepare(context.Background(), &store.SecureCLIBinary{}, &store.SecureCLIUserCredential{
		CredentialType: &credType,
		EncryptedEnv:   []byte(`not json`),
	}, nil); err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}

// Interface validation gate (Phase 2b §"Interface validation gate"):
// Confirms psql consumed the Phase 2 Injection shape without modification.
// If this test fails to compile after a Phase 2 interface change, the change
// likely needs to be re-evaluated for non-git generality.
func TestPsqlAdapter_InterfaceValidationGate(t *testing.T) {
	var _ CredentialAdapter = psqlAdapter{}
	// All four Injection fields are exercised by Prepare:
	//   Env         → PGPASSFILE
	//   Cleanup     → tmpfile removal
	//   ScrubValues → password redaction
	//   ArgvPrefix  → intentionally empty (not all adapters need argv mutation)
}
