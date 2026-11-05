//go:build windows

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRewriteConnectionPointsAtContainer(t *testing.T) {
	in := []string{
		"--host", "127.0.0.1", "--port", "5433", "--username", "goclaw", "--dbname", "goclaw",
		"--format=plain", "--clean", "--if-exists", "--no-owner", "--no-privileges",
	}
	want := []string{
		"--host", "127.0.0.1", "--port", "5432", "--username", "goclaw", "--dbname", "goclaw",
		"--format=plain", "--clean", "--if-exists", "--no-owner", "--no-privileges",
	}
	if got := rewriteConnection(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := rewriteConnection([]string{"--host=db.example", "-p", "6000", "--version"}); !reflect.DeepEqual(
		got, []string{"--host=127.0.0.1", "-p", "5432", "--version"}) {
		t.Fatalf("short and = forms: %v", got)
	}
}

func TestDockerArgsNeverCarryThePassword(t *testing.T) {
	got := dockerArgs("goclaw-postgres", []string{"--port", "5433", "--dbname", "goclaw"})
	want := []string{"exec", "-e", "PGPASSWORD", "goclaw-postgres", "pg_dump", "--port", "5432", "--dbname", "goclaw"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPasswordFromPgpassHandlesEscapes(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".pgpass")
	body := "# comment\nother:5432:otherdb:other:nope\n127.0.0.1:5433:goclaw:goclaw:pa\\:ss\\\\word\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := passwordFromPgpass(path, "goclaw", "goclaw"); got != `pa:ss\word` {
		t.Fatalf("password = %q", got)
	}
	if got := passwordFromPgpass(path, "nobody", "goclaw"); got != "" {
		t.Fatalf("unmatched user must not get a password, got %q", got)
	}
	if got := passwordFromPgpass(filepath.Join(t.TempDir(), "missing"), "a", "b"); got != "" {
		t.Fatalf("missing file: %q", got)
	}
}

func TestOptionValue(t *testing.T) {
	args := []string{"--username", "u1", "-d", "db1", "--other=x"}
	if optionValue(args, "--username", "-U") != "u1" || optionValue(args, "--dbname", "-d") != "db1" {
		t.Fatal("option lookup failed")
	}
	if optionValue([]string{"--username=u2"}, "--username", "-U") != "u2" {
		t.Fatal("= form failed")
	}
}
