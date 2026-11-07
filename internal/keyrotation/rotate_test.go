package keyrotation

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
)

const (
	keyOld = "0123456789abcdef0123456789abcdef"
	keyNew = "fedcba9876543210fedcba9876543210"
)

var targets = []Target{{"secrets_text", "val", Text}, {"secrets_blob", "val", Bytea}}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{
		`CREATE TABLE secrets_text (id INTEGER PRIMARY KEY, val TEXT)`,
		`CREATE TABLE secrets_blob (id INTEGER PRIMARY KEY, val BLOB)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func ring(t *testing.T, active string, keys ...string) {
	t.Helper()
	kid, _ := crypto.KeyID(active)
	kr, err := crypto.NewKeyring(keys, kid)
	if err != nil {
		t.Fatal(err)
	}
	crypto.SetKeyring(kr)
	t.Cleanup(func() { crypto.SetKeyring(nil) })
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

// seed stores values sealed the way the stores did before key ids existed.
func seed(t *testing.T, db *sql.DB) {
	t.Helper()
	crypto.SetKeyring(nil)
	v1, _ := crypto.Encrypt("alpha", keyOld)
	v1b, _ := crypto.Encrypt(`{"K":"beta"}`, keyOld)
	wrapped, _ := crypto.Encrypt(`{"H":"gamma"}`, keyOld)
	w, _ := json.Marshal(wrapped)
	mustExec(t, db, `INSERT INTO secrets_text(val) VALUES (?)`, v1)
	mustExec(t, db, `INSERT INTO secrets_text(val) VALUES (?)`, "not-encrypted")
	mustExec(t, db, `INSERT INTO secrets_text(val) VALUES (NULL)`)
	mustExec(t, db, `INSERT INTO secrets_blob(val) VALUES (?)`, []byte(v1b))
	mustExec(t, db, `INSERT INTO secrets_blob(val) VALUES (?)`, w)
}

func values(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT COALESCE(val,'') FROM ` + table + ` ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			t.Fatal(err)
		}
		out = append(out, string(b))
	}
	return out
}

func TestRotateMovesEverythingAndKeepsValues(t *testing.T) {
	db := openDB(t)
	seed(t, db)
	ring(t, keyNew, keyOld, keyNew)
	ctx := context.Background()
	kidNew, _ := crypto.KeyID(keyNew)

	before, err := Status(ctx, db, SQLite, targets, 2)
	if err != nil || before[0].Legacy != 1 || before[0].Plain != 1 || before[1].Legacy != 2 {
		t.Fatalf("status before = %+v, %v", before, err)
	}

	res, err := Rotate(ctx, db, targets, keyNew, Options{Dialect: SQLite, BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Rotated != 1 || res[1].Rotated != 2 || res[0].Skipped != 1 || res[0].Unreadable+res[1].Unreadable != 0 {
		t.Fatalf("results = %+v", res)
	}

	after, _ := Status(ctx, db, SQLite, targets, 2)
	for _, cs := range after {
		if cs.Legacy != 0 || cs.ByKeyID[kidNew] == 0 {
			t.Fatalf("status after = %+v", cs)
		}
	}

	// Drop the old key: every rotated value must still open, unchanged.
	ring(t, keyNew, keyNew)
	text := values(t, db, "secrets_text")
	if got, err := crypto.Decrypt(text[0], keyNew); err != nil || got != "alpha" {
		t.Fatalf("text value = %q, %v", got, err)
	}
	blob := values(t, db, "secrets_blob")
	if got, err := crypto.Decrypt(blob[0], keyNew); err != nil || got != `{"K":"beta"}` {
		t.Fatalf("blob value = %q, %v", got, err)
	}
	var inner string
	if err := json.Unmarshal([]byte(blob[1]), &inner); err != nil {
		t.Fatalf("wrapped value lost its JSON string form: %q", blob[1])
	}
	if got, err := crypto.Decrypt(inner, keyNew); err != nil || got != `{"H":"gamma"}` {
		t.Fatalf("wrapped value = %q, %v", got, err)
	}
	if text[1] != "not-encrypted" {
		t.Fatalf("plain value was touched: %q", text[1])
	}
}

func TestRotateIsIdempotentAndResumable(t *testing.T) {
	db := openDB(t)
	seed(t, db)
	ring(t, keyNew, keyOld, keyNew)
	ctx := context.Background()

	snapshot := strings.Join(values(t, db, "secrets_text"), "|")
	res, err := Rotate(ctx, db, targets, keyNew, Options{Dialect: SQLite, DryRun: true})
	if err != nil || res[0].Rotated != 1 {
		t.Fatalf("dry run = %+v, %v", res, err)
	}
	if strings.Join(values(t, db, "secrets_text"), "|") != snapshot {
		t.Fatal("dry run changed data")
	}

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Rotate(cctx, db, targets, keyNew, Options{Dialect: SQLite}); err == nil {
		t.Fatal("a cancelled run must report the error")
	}
	if _, err := Rotate(ctx, db, targets, keyNew, Options{Dialect: SQLite, BatchSize: 1}); err != nil {
		t.Fatal(err)
	}
	again, err := Rotate(ctx, db, targets, keyNew, Options{Dialect: SQLite})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range again {
		if r.Rotated != 0 {
			t.Fatalf("second run rotated again: %+v", r)
		}
	}
}

func TestRotateReportsMissingKeyWithoutFallback(t *testing.T) {
	db := openDB(t)
	ring(t, keyOld, keyOld)
	v3, _ := crypto.Encrypt("x", keyOld)
	mustExec(t, db, `INSERT INTO secrets_text(val) VALUES (?)`, v3)

	other := "abcdefghijklmnopqrstuvwxyz012345"
	ring(t, other, other) // the old key is no longer configured
	res, err := Rotate(context.Background(), db, targets[:1], other, Options{Dialect: SQLite})
	if err != nil {
		t.Fatal(err)
	}
	oldKID, _ := crypto.KeyID(keyOld)
	if res[0].Unreadable != 1 || res[0].MissingKID[oldKID] != 1 || res[0].Rotated != 0 {
		t.Fatalf("result = %+v", res[0])
	}
	if got := values(t, db, "secrets_text")[0]; got != v3 {
		t.Fatal("unreadable value must stay untouched")
	}
}

func TestRotateRequiresTargetKeyInRing(t *testing.T) {
	db := openDB(t)
	ring(t, keyOld, keyOld)
	if _, err := Rotate(context.Background(), db, targets, keyNew, Options{Dialect: SQLite}); err == nil {
		t.Fatal("a key outside the keyring must be refused")
	}
}

func TestResultsNeverContainValues(t *testing.T) {
	db := openDB(t)
	seed(t, db)
	ring(t, keyNew, keyOld, keyNew)
	res, _ := Rotate(context.Background(), db, targets, keyNew, Options{Dialect: SQLite})
	b, _ := json.Marshal(res)
	for _, secret := range []string{"alpha", "beta", "gamma", keyOld, keyNew} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("result leaks %q", secret)
		}
	}
}
