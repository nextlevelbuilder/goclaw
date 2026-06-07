package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// psqlAdapter is the framework-validation stub adapter (Phase 2b).
//
// Why it exists: Phase 2 introduces a generic CredentialAdapter interface that
// MUST hold for non-git credential families (kubectl, docker, psql, npm…) or
// it's a git hack pretending to be generic. The psql/PGPASSFILE pattern is the
// simplest non-git case — env-only, file-based, no argv mutation — so if the
// interface doesn't fit it cleanly, that's the signal to reshape NOW, before
// Phase 3/4 cement the shape around git.
//
// Production usage requires UI work (Phase 5 extends the cred-type picker
// beyond git). Until then, this preset is registered but only routes when an
// operator manually sets a binary row's adapter_name='psql' AND seeds a
// `pg_password_file` credential via DB / API.
type psqlAdapter struct{}

func (psqlAdapter) Name() string { return "psql" }

// ShouldInject returns true for any invocation — psql has no subcommand surface,
// every call is "the command", so the adapter is always consulted.
func (psqlAdapter) ShouldInject(_ []string) bool { return true }

// pgPasswordFile is the on-disk JSON shape stored in
// secure_cli_user_credentials.encrypted_env when credential_type='pg_password_file'.
type pgPasswordFile struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
}

func (psqlAdapter) Prepare(_ context.Context, _ *store.SecureCLIBinary, cred *store.SecureCLIUserCredential, _ []string) (*Injection, error) {
	if cred == nil {
		return &Injection{}, nil
	}
	credType := ""
	if cred.CredentialType != nil {
		credType = *cred.CredentialType
	}
	// Legacy env-vars credential — leave untouched, behave like passthrough.
	if credType != "pg_password_file" {
		return &Injection{}, nil
	}

	var pg pgPasswordFile
	if err := json.Unmarshal(cred.EncryptedEnv, &pg); err != nil {
		return nil, fmt.Errorf("decode pg credential: %w", err)
	}
	if pg.Password == "" {
		return nil, fmt.Errorf("pg credential missing password")
	}

	line := fmt.Sprintf("%s:%s:%s:%s:%s\n",
		escapePgpass(pg.Host),
		escapePgpass(pg.Port),
		escapePgpass(pg.Database),
		escapePgpass(pg.User),
		escapePgpass(pg.Password),
	)

	path, cleanup, err := materializeEphemeral(context.TODO(), []byte(line), "pgpass")
	if err != nil {
		return nil, err
	}
	return &Injection{
		Env:     map[string]string{"PGPASSFILE": path},
		Cleanup: cleanup,
		// Scrub both the secret AND the on-disk tmpfile path — psql echoes
		// `could not open password file "<path>"` on permission/IO errors.
		// Mirrors git SSH adapter pattern (credential_adapter_git.go).
		ScrubValues: []string{pg.Password, path},
	}, nil
}

// escapePgpass escapes backslash and colon per the libpq .pgpass spec so a
// malicious password containing `:` cannot break the line format and inject a
// second entry.
//
//	postgresql.org/docs/current/libpq-pgpass.html
func escapePgpass(s string) string {
	// Order matters: escape backslash FIRST, then colon, otherwise the colon
	// escape's own backslash gets double-escaped.
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `:`, `\:`)
	return s
}

func init() {
	RegisterAdapter(psqlAdapter{})
}
