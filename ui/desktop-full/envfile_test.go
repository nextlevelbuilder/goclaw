//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvAcceptsShellExportSyntax(t *testing.T) {
	src := "# comment\n" +
		"\xef\xbb\xbfexport GOCLAW_PORT=18800\n" +
		"GOCLAW_GATEWAY_TOKEN=\"abc def\"\n" +
		"  export   SINGLE='x y'  \n" +
		"PATH=$PATH:/usr/bin\n" +
		"WITH_DOLLAR=a$HOME\n" +
		"EMPTY=\n" +
		"not a line\n" +
		"1BAD=x\n" +
		"\n"
	got, err := parseEnv(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	want := []envEntry{
		{"GOCLAW_PORT", "18800"},
		{"GOCLAW_GATEWAY_TOKEN", "abc def"},
		{"SINGLE", "x y"},
		{"WITH_DOLLAR", "a$HOME"},
		{"EMPTY", ""},
	}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestMergeEnvLaterLayerWins(t *testing.T) {
	a := []envEntry{{"A", "1"}, {"B", "2"}}
	b := []envEntry{{"B", "3"}, {"C", "4"}}
	got := mergeEnv(a, b)
	want := []envEntry{{"A", "1"}, {"B", "3"}, {"C", "4"}}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("merge = %#v, want %#v", got, want)
		}
	}
}

func TestFormatEnvRoundTrips(t *testing.T) {
	in := []envEntry{{"K1", `a"b`}, {"K2", `"quoted"`}, {"K3", "has space"}, {"K4", ""}}
	out, err := parseEnv(strings.NewReader(formatEnv(in)))
	if err != nil {
		t.Fatal(err)
	}
	for i := range in {
		if out[i] != in[i] {
			t.Fatalf("round trip %d = %#v, want %#v", i, out[i], in[i])
		}
	}
}

func TestImportFromDirMergesCopiesConfigAndRestrictsAccess(t *testing.T) {
	src := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".env", "POSTGRES_PASSWORD=from-env\nGOCLAW_PORT=1\n")
	write(".env.local", "export GOCLAW_POSTGRES_DSN=postgres://u:p@127.0.0.1:5432/db\nGOCLAW_PORT=18800\n")
	write("config.json", `{"gateway":{}}`)

	dst := t.TempDir()
	envPath, cfgPath := filepath.Join(dst, "goclaw.env"), filepath.Join(dst, "config.json")
	res, err := importFromDir(src, envPath, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if res.Keys != 3 || !res.ConfigCopied {
		t.Fatalf("result = %#v", res)
	}
	entries, err := loadEnvFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := envLookup(entries, "GOCLAW_PORT"); v != "18800" {
		t.Fatalf("GOCLAW_PORT = %q, .env.local must override .env", v)
	}
	if b, _ := os.ReadFile(filepath.Join(src, ".env")); !strings.Contains(string(b), "GOCLAW_PORT=1") {
		t.Fatal("the original .env was modified")
	}
	for _, p := range []string{envPath, cfgPath} {
		out, err := exec.Command("icacls", p).CombinedOutput()
		if err != nil {
			t.Fatal(err)
		}
		acl := string(out)
		for _, broad := range []string{"BUILTIN\\Users", "Everyone", "Authenticated Users"} {
			if strings.Contains(acl, broad) {
				t.Fatalf("%s is readable by %s:\n%s", filepath.Base(p), broad, acl)
			}
		}
	}

	// Importing again keeps the previous file as a timestamped backup.
	if _, err := importFromDir(src, envPath, cfgPath); err != nil {
		t.Fatal(err)
	}
	baks, _ := filepath.Glob(envPath + ".bak-*")
	if len(baks) != 1 {
		t.Fatalf("expected one backup of the previous env file, got %v", baks)
	}
}

func TestImportFromDirRejectsFolderWithoutDSN(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, ".env"), []byte("FOO=bar\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := importFromDir(src, filepath.Join(t.TempDir(), "e"), filepath.Join(t.TempDir(), "c")); err == nil {
		t.Fatal("expected an error when GOCLAW_POSTGRES_DSN is missing")
	}
	if _, err := importFromDir(t.TempDir(), "e", "c"); err == nil {
		t.Fatal("expected an error for an empty folder")
	}
}

func TestPostgresAddr(t *testing.T) {
	cases := []struct{ dsn, host, port string }{
		{"postgres://u:p@127.0.0.1:5432/db?sslmode=disable", "127.0.0.1", "5432"},
		{"postgresql://u:p@db.local/db", "db.local", "5432"},
		{"postgres://u:p@[::1]:5433/db", "::1", "5433"},
		{"host=pg port=6543 user=u dbname=d", "pg", "6543"},
		{"user=u dbname=d", "localhost", "5432"},
	}
	for _, c := range cases {
		h, p, err := postgresAddr(c.dsn)
		if err != nil || h != c.host || p != c.port {
			t.Errorf("%q -> %q %q %v, want %q %q", c.dsn, h, p, err, c.host, c.port)
		}
	}
	if _, _, err := postgresAddr(" "); err == nil {
		t.Error("empty DSN must be an error")
	}
}
