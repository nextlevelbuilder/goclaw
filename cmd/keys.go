package cmd

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/keyrotation"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// keysCmd manages the encryption keyring. Nothing it prints is a secret: key
// ids are a hash prefix, and stored values are never read out.
func keysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Inspect and rotate the encryption keys",
		Long: `Encryption keys are configured with:

  GOCLAW_ENCRYPTION_KEYS    "kid=key,kid=key"  every key that may open stored data
  GOCLAW_ENCRYPTION_KEY_ID  kid                the key used for new writes
  GOCLAW_ENCRYPTION_KEY     key                single-key setup, still supported

To rotate: keys add --key <new>, add the printed entry to GOCLAW_ENCRYPTION_KEYS
(keep the old key there), then keys rotate --to <kid>. Once keys status shows
nothing left under the old key, remove it and set GOCLAW_ENCRYPTION_KEY_ID to
the new kid.`,
	}
	cmd.AddCommand(keysStatusCmd(), keysAddCmd(), keysRotateCmd())
	return cmd
}

func keysAddCmd() *cobra.Command {
	var key string
	var generate bool
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Validate a new key and print its id and the config entry to add",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if generate {
				if key != "" {
					return errors.New("use either --key or --generate")
				}
				raw := make([]byte, 32)
				if _, err := rand.Read(raw); err != nil {
					return err
				}
				key = hex.EncodeToString(raw)
				fmt.Fprintf(out, "Generated key (shown once, store it in your secret manager):\n%s\n\n", key)
			}
			if key == "" {
				return errors.New("--key or --generate is required")
			}
			kid, err := crypto.KeyID(key)
			if err != nil {
				return err
			}
			if kr := crypto.CurrentKeyring(); kr != nil {
				for _, k := range kr.KIDs() {
					if k == kid {
						return fmt.Errorf("key %s is already configured", kid)
					}
				}
			}
			fmt.Fprintf(out, "Key id: %s\n", kid)
			fmt.Fprintf(out, "Add to GOCLAW_ENCRYPTION_KEYS (comma separated, keep the existing keys):\n  %s=<the key>\n", kid)
			fmt.Fprintf(out, "Then run: goclaw keys rotate --to %s\n", kid)
			return nil
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "New 32-byte key (raw, hex or base64)")
	cmd.Flags().BoolVar(&generate, "generate", false, "Generate a random key")
	return cmd
}

func keysStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "List configured key ids and how stored values are sealed",
		RunE: func(cmd *cobra.Command, args []string) error {
			kr := crypto.CurrentKeyring()
			if kr == nil {
				return errors.New("no encryption key is configured (GOCLAW_ENCRYPTION_KEY or GOCLAW_ENCRYPTION_KEYS)")
			}
			db, closeFn, err := openKeysDB(cmd.Context())
			if err != nil {
				return err
			}
			defer closeFn()
			statuses, err := keyrotation.Status(cmd.Context(), db, keyrotation.PG, keyrotation.PGTargets, 500)
			if err != nil {
				return err
			}
			vault, err := vaultStatus(cmd.Context(), db)
			if err != nil {
				return err
			}
			renderKeysStatus(cmd.OutOrStdout(), kr, statuses, vault)
			return nil
		},
	}
}

func keysRotateCmd() *cobra.Command {
	var (
		to     string
		dryRun bool
		batch  int
	)
	cmd := &cobra.Command{
		Use:   "rotate",
		Short: "Re-encrypt all stored secrets under one key (values are never printed)",
		Long: `Seals every encrypted column, and the secret vault of every tenant, under the
key --to. Safe to re-run: values already under that key are skipped, and an
interrupted run is finished by running it again. Each write only applies if
the row still holds the value that was read. Exits non-zero when any value
cannot be opened with the configured keys; those are counted per key id and
left untouched.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			kr := crypto.CurrentKeyring()
			if kr == nil {
				return errors.New("no encryption key is configured (GOCLAW_ENCRYPTION_KEY or GOCLAW_ENCRYPTION_KEYS)")
			}
			target, ok := kr.KeyFor(strings.TrimSpace(to))
			if !ok {
				return &crypto.UnknownKeyIDError{KID: strings.TrimSpace(to)}
			}
			db, closeFn, err := openKeysDB(cmd.Context())
			if err != nil {
				return err
			}
			defer closeFn()
			return rotateKeys(cmd.Context(), cmd.OutOrStdout(), db, target, dryRun, batch)
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "Key id to seal everything under (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Only count what would be rotated")
	cmd.Flags().IntVar(&batch, "batch-size", 200, "Rows per batch")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func openKeysDB(ctx context.Context) (*sql.DB, func(), error) {
	dsn, err := resolveDSN()
	if err != nil {
		return nil, nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("ping db: %w", err)
	}
	return db, func() { db.Close() }, nil
}

// vaultCounts is how the secret vault's rows are sealed, summed over tenants.
type vaultCounts struct {
	ByKeyID map[string]int
	V2      int
	Legacy  int
}

func vaultStatus(ctx context.Context, db *sql.DB) (vaultCounts, error) {
	vc := vaultCounts{ByKeyID: map[string]int{}}
	rows, err := db.QueryContext(ctx, `SELECT encrypted_payload FROM secret_vault_entries`)
	if err != nil {
		return vc, fmt.Errorf("secret_vault_entries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return vc, err
		}
		r := keyrotation.Inspect(v)
		switch r.Class {
		case keyrotation.ClassKeyed:
			vc.ByKeyID[r.KID]++
		case keyrotation.ClassBound:
			vc.V2++
		case keyrotation.ClassLegacy:
			vc.Legacy++
		}
	}
	return vc, rows.Err()
}

func renderKeysStatus(out io.Writer, kr *crypto.Keyring, statuses []keyrotation.ColumnStatus, vault vaultCounts) {
	fmt.Fprintln(out, "Keys:")
	for _, kid := range kr.KIDs() {
		mark := ""
		if kid == kr.ActiveKID() {
			mark = " (active)"
		}
		fmt.Fprintf(out, "  %s%s\n", kid, mark)
	}
	fmt.Fprintln(out)

	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "COLUMN\tLEGACY\tPLAIN\tBOUND\tBY KEY")
	for _, cs := range statuses {
		if cs.Total() == cs.Empty {
			continue
		}
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%s\n", cs.Target.Name(), cs.Legacy, cs.Plain, cs.Bound, formatKeyCounts(cs.ByKeyID))
	}
	fmt.Fprintf(w, "secret_vault_entries.encrypted_payload\t%d\t0\t%d\t%s\n", vault.Legacy, vault.V2, formatKeyCounts(vault.ByKeyID))
	_ = w.Flush()
	fmt.Fprintln(out, "\nLEGACY and BOUND values carry no key id; rotate moves them to the active key.")
}

func formatKeyCounts(m map[string]int) string {
	if len(m) == 0 {
		return "-"
	}
	kids := make([]string, 0, len(m))
	for k := range m {
		kids = append(kids, k)
	}
	sort.Strings(kids)
	parts := make([]string, 0, len(kids))
	for _, k := range kids {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

// rotateKeys seals the columns and every tenant's vault under target and
// prints counts only. It returns an error when anything stayed unreadable.
func rotateKeys(ctx context.Context, out io.Writer, db *sql.DB, target string, dryRun bool, batch int) error {
	verb := "Rotated"
	if dryRun {
		verb = "Would rotate"
	}
	results, err := keyrotation.Rotate(ctx, db, keyrotation.PGTargets, target, keyrotation.Options{
		Dialect: keyrotation.PG, BatchSize: batch, DryRun: dryRun,
	})
	if err != nil {
		return err
	}
	unreadable := 0
	missing := map[string]int{}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "COLUMN\tROTATED\tCURRENT\tSKIPPED\tUNREADABLE\tCONFLICTS")
	for _, r := range results {
		if r.Examined == 0 {
			continue
		}
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\n", r.Target.Name(), r.Rotated, r.Current, r.Skipped, r.Unreadable, r.Conflicts)
		unreadable += r.Unreadable
		for kid, n := range r.MissingKID {
			missing[kid] += n
		}
	}
	_ = w.Flush()

	vaultRotated, vaultCurrent, vaultUnreadable, err := rotateVaults(ctx, db, target, dryRun)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "secret vault: %s %d, already current %d, unreadable %d\n", strings.ToLower(verb), vaultRotated, vaultCurrent, vaultUnreadable)
	unreadable += vaultUnreadable

	if len(missing) > 0 {
		fmt.Fprintf(out, "values sealed under keys that are not configured: %s\n", formatKeyCounts(missing))
	}
	if unreadable > 0 {
		return fmt.Errorf("%d values could not be opened with the configured keys and were left untouched", unreadable)
	}
	if !dryRun {
		fmt.Fprintln(out, "Done. Run `goclaw keys status` to confirm nothing is left under the old key.")
	}
	return nil
}

func rotateVaults(ctx context.Context, db *sql.DB, target string, dryRun bool) (rotated, current, unreadable int, err error) {
	st := pg.NewPGSecretVaultStore(db, target)
	if st == nil {
		return 0, 0, 0, store.ErrSecretVaultNoKey
	}
	rows, err := db.QueryContext(ctx, `SELECT id FROM tenants`)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("list tenants: %w", err)
	}
	var tenants []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, 0, 0, err
		}
		tenants = append(tenants, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, 0, 0, err
	}
	for _, tid := range tenants {
		tctx := store.WithUserID(store.WithTenantID(ctx, tid), "cli")
		rep, rerr := st.RewrapSecrets(tctx, dryRun)
		if rerr != nil {
			return rotated, current, unreadable, fmt.Errorf("secret vault: %w", rerr)
		}
		rotated += rep.Rewrapped
		current += rep.Current
		unreadable += len(rep.Unreadable)
	}
	return rotated, current, unreadable, nil
}
