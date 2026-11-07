// Package keyrotation re-encrypts stored secrets under the active key of the
// process keyring and reports which key each stored value is sealed with.
//
// It never logs, returns or prints a stored value or key material: results are
// counts per column and key ids, which are not secret.
package keyrotation

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
)

// Kind is how a column stores an encrypted value.
type Kind int

const (
	// Text is a TEXT column holding the envelope.
	Text Kind = iota
	// Bytea is a BYTEA column holding the envelope bytes (or, for values written
	// through the MCP JSONB helper, a JSON string that wraps the envelope).
	Bytea
	// JSONB is a JSONB column holding a JSON string that wraps the envelope.
	JSONB
)

// Target is one encrypted column.
type Target struct {
	Table  string
	Column string
	Kind   Kind
}

// Name is "table.column".
func (t Target) Name() string { return t.Table + "." + t.Column }

// PGTargets lists every PostgreSQL column that holds a value sealed by
// crypto.Encrypt. The secret vault is handled separately because its envelopes
// are bound to the row (see store.RewrapSecretCiphertext).
var PGTargets = []Target{
	{"llm_providers", "api_key", Text},
	{"mcp_servers", "api_key", Text},
	{"mcp_servers", "headers", JSONB},
	{"mcp_servers", "env", JSONB},
	{"mcp_user_credentials", "api_key", Text},
	{"mcp_user_credentials", "headers", Bytea},
	{"mcp_user_credentials", "env", Bytea},
	{"mcp_context_credentials", "api_key", Text},
	{"mcp_context_credentials", "headers", Bytea},
	{"mcp_context_credentials", "env", Bytea},
	{"mcp_oauth_tokens", "access_token", Text},
	{"mcp_oauth_tokens", "refresh_token", Text},
	{"mcp_oauth_tokens", "dcr_client_secret", Text},
	{"channel_instances", "credentials", Bytea},
	{"secure_cli_binaries", "encrypted_env", Bytea},
	{"secure_cli_user_credentials", "encrypted_env", Bytea},
	{"secure_cli_agent_grants", "encrypted_env", Bytea},
	{"secure_cli_agent_credentials", "encrypted_env", Bytea},
	{"secure_cli_context_grants", "encrypted_env", Bytea},
	{"secure_cli_context_credentials", "encrypted_env", Bytea},
	{"bitrix_portals", "credentials", Bytea},
	{"bitrix_portals", "state", Bytea},
	{"browser_cookies", "encrypted_value", Text},
	{"config_secrets", "value", Bytea},
	{"workstations", "metadata", Bytea},
	{"workstations", "default_env", Bytea},
	{"webhooks", "encrypted_secret", Text},
}

// Dialect selects the SQL flavour. SQLite exists so the core can be tested
// without a PostgreSQL server.
type Dialect int

const (
	PG Dialect = iota
	SQLite
)

// Class is how a stored value is sealed.
type Class int

const (
	ClassEmpty    Class = iota // NULL or empty
	ClassPlain                 // not an envelope
	ClassLegacy                // v1 "aes-gcm:" (no key id)
	ClassBound                 // v2 "v2:aes-gcm:" (bound to its row; not rotated here)
	ClassKeyed                 // v3, sealed under a named key
	classCount
)

// Row is the reading of one stored value.
type Row struct {
	Class   Class
	KID     string // ClassKeyed only
	Inner   string // the envelope, unwrapped from any JSON string
	Wrapped bool   // the envelope was inside a JSON string
}

// Inspect classifies a stored value without opening it.
func Inspect(stored string) Row {
	if stored == "" {
		return Row{Class: ClassEmpty}
	}
	inner, wrapped := stored, false
	if strings.HasPrefix(stored, `"`) {
		var s string
		if json.Unmarshal([]byte(stored), &s) == nil {
			inner, wrapped = s, true
		}
	}
	switch {
	case crypto.IsEncryptedV3(inner):
		kid, _ := crypto.EnvelopeKeyID(inner)
		return Row{Class: ClassKeyed, KID: kid, Inner: inner, Wrapped: wrapped}
	case crypto.IsEncryptedAAD(inner):
		return Row{Class: ClassBound, Inner: inner, Wrapped: wrapped}
	case crypto.IsEncrypted(inner):
		return Row{Class: ClassLegacy, Inner: inner, Wrapped: wrapped}
	default:
		return Row{Class: ClassPlain}
	}
}

// ColumnStatus counts the values of one column by how they are sealed.
type ColumnStatus struct {
	Target  Target
	Empty   int
	Plain   int
	Legacy  int
	Bound   int
	ByKeyID map[string]int
}

// Total is the number of values counted.
func (c ColumnStatus) Total() int {
	n := c.Empty + c.Plain + c.Legacy + c.Bound
	for _, v := range c.ByKeyID {
		n += v
	}
	return n
}

// Options configures a run.
type Options struct {
	Dialect   Dialect
	BatchSize int  // rows per batch (default 200)
	DryRun    bool // count what would change, write nothing
}

func (o Options) batch() int {
	if o.BatchSize <= 0 {
		return 200
	}
	return o.BatchSize
}

// Result is the outcome of rotating one column.
type Result struct {
	Target     Target
	Examined   int
	Rotated    int
	Current    int
	Skipped    int            // plain, empty or bound values
	Unreadable int            // could not be opened with the keyring
	MissingKID map[string]int // key ids named by values but absent from the keyring
	Conflicts  int            // changed under us; picked up by the next pass
}

// selectExpr yields the column as text so it can be compared and bound.
func selectExpr(d Dialect, t Target) string {
	if d == PG {
		switch t.Kind {
		case JSONB:
			return t.Column + "::text"
		case Bytea:
			return "encode(" + t.Column + ", 'base64')"
		}
	}
	return t.Column
}

func writeExpr(d Dialect, t Target, ph string) string {
	if d == PG {
		switch t.Kind {
		case JSONB:
			return ph + "::jsonb"
		case Bytea:
			return "decode(" + ph + ", 'base64')"
		}
	}
	return ph
}

// load returns the stored value as the string the stores wrote.
func load(d Dialect, t Target, raw sql.RawBytes) (string, error) {
	if d == PG && t.Kind == Bytea {
		b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(raw), "\n", ""))
		return string(b), err
	}
	return string(raw), nil
}

// store converts the string to the form bound for a write or compare.
func store(d Dialect, t Target, v string) any {
	if t.Kind == Bytea {
		if d == PG {
			return base64.StdEncoding.EncodeToString([]byte(v))
		}
		return []byte(v)
	}
	return v
}

func ph(d Dialect, n int) string {
	if d == PG {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

// scan walks a column in row order, calling fn for each non-NULL value. It
// stops when fn returns an error.
func scan(ctx context.Context, db *sql.DB, d Dialect, t Target, batch int, fn func(rowRef, stored string) error) error {
	ref, refSel, after := "rowid", "rowid", "rowid > ?"
	if d == PG {
		ref, refSel, after = "ctid", "ctid::text", "ctid > $1::tid"
	}
	last := "(0,0)"
	if d == SQLite {
		last = "0"
	}
	q := fmt.Sprintf(`SELECT %s, %s FROM %s WHERE %s AND %s IS NOT NULL ORDER BY %s LIMIT %d`,
		refSel, selectExpr(d, t), t.Table, after, t.Column, ref, batch)
	for {
		rows, err := db.QueryContext(ctx, q, last)
		if err != nil {
			return fmt.Errorf("%s: %w", t.Name(), err)
		}
		type item struct{ ref, val string }
		var items []item
		for rows.Next() {
			var r string
			var raw sql.RawBytes
			if err := rows.Scan(&r, &raw); err != nil {
				rows.Close()
				return fmt.Errorf("%s: %w", t.Name(), err)
			}
			v, lerr := load(d, t, raw)
			if lerr != nil {
				v = "" // undecodable: counted as empty rather than aborting the scan
			}
			items = append(items, item{r, v})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", t.Name(), err)
		}
		if len(items) == 0 {
			return nil
		}
		for _, it := range items {
			if err := fn(it.ref, it.val); err != nil {
				return err
			}
		}
		last = items[len(items)-1].ref
	}
}

// Status counts how each target's values are sealed. It reads, never writes.
func Status(ctx context.Context, db *sql.DB, d Dialect, targets []Target, batch int) ([]ColumnStatus, error) {
	if batch <= 0 {
		batch = 200
	}
	out := make([]ColumnStatus, 0, len(targets))
	for _, t := range targets {
		cs := ColumnStatus{Target: t, ByKeyID: map[string]int{}}
		err := scan(ctx, db, d, t, batch, func(_, stored string) error {
			r := Inspect(stored)
			switch r.Class {
			case ClassEmpty:
				cs.Empty++
			case ClassPlain:
				cs.Plain++
			case ClassLegacy:
				cs.Legacy++
			case ClassBound:
				cs.Bound++
			case ClassKeyed:
				cs.ByKeyID[r.KID]++
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		out = append(out, cs)
	}
	return out, nil
}

// Rotate seals every value of targets under activeKey, which must be in the
// installed keyring. It is idempotent and resumable: a value already sealed
// under activeKey is left alone, a failed or interrupted run is finished by
// running it again, and each write is a compare-and-set on the old value so a
// concurrent change is never overwritten.
func Rotate(ctx context.Context, db *sql.DB, targets []Target, activeKey string, opt Options) ([]Result, error) {
	activeKID, ok := crypto.ActiveKeyID(activeKey)
	if !ok {
		return nil, errors.New("keyrotation: the target key is not in the keyring")
	}
	var results []Result
	for _, t := range targets {
		res := Result{Target: t, MissingKID: map[string]int{}}
		type pending struct{ ref, old, enc string }
		var batch []pending
		flush := func() error {
			for _, p := range batch {
				ok, err := compareAndSet(ctx, db, opt.Dialect, t, p.ref, p.old, p.enc)
				if err != nil {
					return err
				}
				if ok {
					res.Rotated++
				} else {
					res.Conflicts++
				}
			}
			batch = batch[:0]
			return nil
		}
		err := scan(ctx, db, opt.Dialect, t, opt.batch(), func(ref, stored string) error {
			res.Examined++
			r := Inspect(stored)
			switch r.Class {
			case ClassEmpty, ClassPlain, ClassBound:
				res.Skipped++
				return nil
			case ClassKeyed:
				if r.KID == activeKID {
					res.Current++
					return nil
				}
			}
			plain, err := crypto.Decrypt(r.Inner, activeKey)
			if err != nil {
				var unknown *crypto.UnknownKeyIDError
				if errors.As(err, &unknown) {
					res.MissingKID[unknown.KID]++
				}
				res.Unreadable++
				return nil
			}
			enc, err := crypto.Encrypt(plain, activeKey)
			if err != nil {
				res.Unreadable++
				return nil
			}
			// Prove the new value opens before it can replace the old one.
			if back, err := crypto.Decrypt(enc, activeKey); err != nil || back != plain {
				res.Unreadable++
				return nil
			}
			if r.Wrapped {
				w, _ := json.Marshal(enc)
				enc = string(w)
			}
			if opt.DryRun {
				res.Rotated++
				return nil
			}
			batch = append(batch, pending{ref, stored, enc})
			if len(batch) >= opt.batch() {
				return flush()
			}
			return nil
		})
		if err == nil {
			err = flush()
		}
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}

func compareAndSet(ctx context.Context, db *sql.DB, d Dialect, t Target, ref, old, enc string) (bool, error) {
	ref1, cmp, refPh := "rowid", selectExpr(d, t), ph(d, 3)
	if d == PG {
		ref1, refPh = "ctid", ph(d, 3)+"::tid"
	}
	q := fmt.Sprintf(`UPDATE %s SET %s = %s WHERE %s = %s AND %s = %s`,
		t.Table, t.Column, writeExpr(d, t, ph(d, 1)), ref1, refPh, cmp, ph(d, 2))
	if d == SQLite {
		// "?" placeholders are positional: value, row, old.
		res, err := db.ExecContext(ctx, q, store(d, t, enc), ref, store(d, t, old))
		return affected(res, err)
	}
	res, err := db.ExecContext(ctx, q, store(d, t, enc), store(d, t, old), ref)
	return affected(res, err)
}

func affected(res sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
