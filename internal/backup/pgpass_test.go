package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDSN_ValidFull(t *testing.T) {
	creds, err := ParseDSN("postgres://myuser:s3cret@db.example.com:5433/mydb?sslmode=require")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEqual(t, "host", "db.example.com", creds.Host)
	assertEqual(t, "port", "5433", creds.Port)
	assertEqual(t, "user", "myuser", creds.User)
	assertEqual(t, "password", "s3cret", creds.Password)
	assertEqual(t, "dbname", "mydb", creds.DBName)
	assertEqual(t, "sslmode", "require", creds.SSLMode)
}

func TestParseDSN_Defaults(t *testing.T) {
	creds, err := ParseDSN("postgres://admin@/testdb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEqual(t, "host", "localhost", creds.Host)
	assertEqual(t, "port", "5432", creds.Port)
	assertEqual(t, "user", "admin", creds.User)
	assertEqual(t, "password", "", creds.Password)
	assertEqual(t, "sslmode", "prefer", creds.SSLMode)
}

func TestParseDSN_InvalidScheme(t *testing.T) {
	_, err := ParseDSN("mysql://user:pass@localhost/db")
	if err == nil {
		t.Fatal("expected error for mysql scheme")
	}
	if !strings.Contains(err.Error(), "unsupported scheme") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseDSN_MissingDBName(t *testing.T) {
	_, err := ParseDSN("postgres://user:pass@localhost:5432/")
	if err == nil {
		t.Fatal("expected error for missing dbname")
	}
}

func TestParseDSN_SpecialCharsInPassword(t *testing.T) {
	// URL-encoded password: p@ss:w0rd! → p%40ss%3Aw0rd%21
	creds, err := ParseDSN("postgres://user:p%40ss%3Aw0rd%21@localhost/db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEqual(t, "password", "p@ss:w0rd!", creds.Password)
}

func TestWritePgpass_FilePermissions(t *testing.T) {
	creds := &PGCredentials{
		Host: "localhost", Port: "5432",
		User: "testuser", Password: "testpass",
		DBName: "testdb",
	}

	tempDir, pgpassPath, err := WritePgpass(creds)
	if err != nil {
		t.Fatalf("WritePgpass failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Verify file exists
	info, err := os.Stat(pgpassPath)
	if err != nil {
		t.Fatalf("pgpass file not found: %v", err)
	}

	// Verify 0600 permissions (owner read+write only)
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Fatalf("expected 0600 permissions, got %04o", perm)
	}
}

func TestWritePgpass_Content(t *testing.T) {
	creds := &PGCredentials{
		Host: "db.example.com", Port: "5433",
		User: "myuser", Password: "my:pass\\word",
		DBName: "mydb",
	}

	tempDir, pgpassPath, err := WritePgpass(creds)
	if err != nil {
		t.Fatalf("WritePgpass failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	data, err := os.ReadFile(pgpassPath)
	if err != nil {
		t.Fatalf("read pgpass: %v", err)
	}

	content := string(data)
	// Colons and backslashes must be escaped
	expected := `db.example.com:5433:mydb:myuser:my\:pass\\word` + "\n"
	if content != expected {
		t.Fatalf("pgpass content mismatch:\n  got:  %q\n  want: %q", content, expected)
	}
}

func TestWritePgpass_TempDirCleanup(t *testing.T) {
	creds := &PGCredentials{
		Host: "localhost", Port: "5432",
		User: "u", Password: "p", DBName: "db",
	}

	tempDir, _, err := WritePgpass(creds)
	if err != nil {
		t.Fatalf("WritePgpass failed: %v", err)
	}

	// Simulate cleanup
	os.RemoveAll(tempDir)

	// Verify directory is gone
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Fatalf("temp dir still exists after RemoveAll: %s", tempDir)
	}
}

func TestCleanEnv_NoSecretLeak(t *testing.T) {
	// Set a fake secret in current env to verify it doesn't leak
	t.Setenv("GOCLAW_POSTGRES_DSN", "postgres://user:SECRET@localhost/db")
	t.Setenv("GOCLAW_ENCRYPTION_KEY", "super-secret-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")

	env := CleanEnv("/tmp/test/.pgpass")

	envStr := strings.Join(env, "\n")

	// Must contain PGPASSFILE
	if !strings.Contains(envStr, "PGPASSFILE=/tmp/test/.pgpass") {
		t.Error("PGPASSFILE not set")
	}

	// Must NOT contain any secrets
	for _, forbidden := range []string{"SECRET", "super-secret", "aws-secret", "GOCLAW_POSTGRES_DSN", "GOCLAW_ENCRYPTION_KEY", "AWS_SECRET"} {
		if strings.Contains(envStr, forbidden) {
			t.Errorf("secret leaked in clean env: found %q", forbidden)
		}
	}

	// Must only have 4 entries
	if len(env) != 4 {
		t.Errorf("expected exactly 4 env vars, got %d: %v", len(env), env)
	}
}

func TestCleanEnv_HasRequiredVars(t *testing.T) {
	env := CleanEnv("/tmp/.pgpass")

	required := map[string]bool{"PGPASSFILE": false, "PATH": false, "HOME": false, "LC_ALL": false}
	for _, e := range env {
		key := strings.SplitN(e, "=", 2)[0]
		if _, ok := required[key]; ok {
			required[key] = true
		}
	}

	for k, found := range required {
		if !found {
			t.Errorf("missing required env var: %s", k)
		}
	}
}

func TestSanitizeDSN_StripsPassword(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			"postgres://user:s3cret@localhost:5432/db?sslmode=disable",
			"postgres://user@localhost:5432/db?sslmode=disable",
		},
		{
			"postgres://admin:p%40ss%3Aword@db.example.com/mydb",
			"postgres://admin@db.example.com/mydb",
		},
		{
			"postgres://nopass@localhost/db",
			"postgres://nopass@localhost/db",
		},
		{
			"not-a-url",
			"***",
		},
	}

	for _, tc := range cases {
		got := SanitizeDSN(tc.input)
		if got != tc.want {
			t.Errorf("SanitizeDSN(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestWritePgpass_PathIsolation(t *testing.T) {
	// Create two concurrent pgpass files — verify they don't collide
	creds1 := &PGCredentials{Host: "h1", Port: "5432", User: "u1", Password: "p1", DBName: "db1"}
	creds2 := &PGCredentials{Host: "h2", Port: "5433", User: "u2", Password: "p2", DBName: "db2"}

	dir1, path1, err := WritePgpass(creds1)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir1)

	dir2, path2, err := WritePgpass(creds2)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir2)

	// Different temp dirs
	if dir1 == dir2 {
		t.Error("concurrent WritePgpass created same temp dir")
	}

	// Different file paths
	if path1 == path2 {
		t.Error("concurrent WritePgpass created same pgpass path")
	}

	// Verify parent dirs are different
	if filepath.Dir(path1) == filepath.Dir(path2) {
		t.Error("pgpass files share parent directory")
	}
}

func assertEqual(t *testing.T, field, want, got string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %q, want %q", field, got, want)
	}
}
