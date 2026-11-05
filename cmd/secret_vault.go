package cmd

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// secretVaultCmd wires `goclaw secret-vault ...`: direct-DB admin of the secret
// vault. Values are write-only: they are read from stdin (hidden on a
// terminal) and no subcommand ever prints one.
func secretVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret-vault",
		Short: "Manage vault secrets for agents (direct DB access; postgres only)",
		Long: `Manage the local secret vault: API keys and logins that agents use through
{{secret:name}} placeholders without the value ever reaching the model.

Values are write-only. "set" reads the value from stdin (hidden when stdin is a
terminal); nothing prints it back. Requires GOCLAW_ENCRYPTION_KEY.`,
	}
	cmd.AddCommand(secretVaultListCmd(), secretVaultSetCmd(), secretVaultDeleteCmd())
	return cmd
}

// openSecretVault opens the PG vault store for a tenant-scoped context.
func openSecretVault(cmd *cobra.Command, tenantID string) (store.SecretVaultStore, context.Context, func(), error) {
	tid, err := uuid.Parse(strings.TrimSpace(tenantID))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("--tenant-id must be a tenant UUID")
	}
	encKey := os.Getenv("GOCLAW_ENCRYPTION_KEY")
	if encKey == "" {
		return nil, nil, nil, fmt.Errorf("GOCLAW_ENCRYPTION_KEY is not set: the vault refuses to store plaintext")
	}
	dsn, err := resolveDSN()
	if err != nil {
		return nil, nil, nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.PingContext(cmd.Context()); err != nil {
		db.Close()
		return nil, nil, nil, fmt.Errorf("ping db: %w", err)
	}
	st := pg.NewPGSecretVaultStore(db, encKey)
	if st == nil {
		db.Close()
		return nil, nil, nil, store.ErrSecretVaultNoKey
	}
	ctx := store.WithUserID(store.WithTenantID(cmd.Context(), tid), "cli")
	return st, ctx, func() { db.Close() }, nil
}

func secretVaultListCmd() *cobra.Command {
	var tenantID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List secrets (metadata only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, ctx, closeFn, err := openSecretVault(cmd, tenantID)
			if err != nil {
				return err
			}
			defer closeFn()
			return listSecretVault(ctx, st, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "Tenant UUID (required)")
	return cmd
}

func listSecretVault(ctx context.Context, st store.SecretVaultStore, out io.Writer) error {
	entries, err := st.List(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tKIND\tAPPROVAL\tDOMAINS")
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%v\t%s\n", e.Name, e.Kind, e.RequireApproval, strings.Join(e.AllowedDomains, ","))
	}
	return w.Flush()
}

type secretVaultSetOpts struct {
	Name, Kind, Username string
	Domains              []string
	Approval             *bool
	Hints                string
}

func secretVaultSetCmd() *cobra.Command {
	var (
		tenantID string
		o        secretVaultSetOpts
		approval string
	)
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Create a secret, or replace the value of an existing one (value read from stdin)",
		Example: `  printf '%s' "$TOKEN" | goclaw secret-vault set --tenant-id <uuid> --name gh-token --kind api_key --domains api.github.com
  goclaw secret-vault set --tenant-id <uuid> --name gh-login --kind login --username me --domains github.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch approval {
			case "":
			case "true", "false":
				v := approval == "true"
				o.Approval = &v
			default:
				return fmt.Errorf("--require-approval must be true or false")
			}
			st, ctx, closeFn, err := openSecretVault(cmd, tenantID)
			if err != nil {
				return err
			}
			defer closeFn()
			value, err := readSecretValue(os.Stdin, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			msg, err := setSecretVault(ctx, st, o, value)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), msg)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&tenantID, "tenant-id", "", "Tenant UUID (required)")
	f.StringVar(&o.Name, "name", "", "Secret name used in {{secret:name}} (required)")
	f.StringVar(&o.Kind, "kind", "api_key", "api_key or login")
	f.StringVar(&o.Username, "username", "", "Username (kind login)")
	f.StringSliceVar(&o.Domains, "domains", nil, "Allowed domains, comma separated (required for a new secret)")
	f.StringVar(&approval, "require-approval", "", "true or false (default true for login, false for api_key)")
	f.StringVar(&o.Hints, "hints", "", "Free-text note shown to admins")
	_ = cmd.MarkFlagRequired("tenant-id")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// setSecretVault creates the secret, or replaces its value when the name exists.
func setSecretVault(ctx context.Context, st store.SecretVaultStore, o secretVaultSetOpts, value string) (string, error) {
	payload := store.SecretVaultPayload{Username: o.Username, Secret: value}
	approval := o.Kind == store.SecretKindLogin
	if o.Approval != nil {
		approval = *o.Approval
	}
	e := &store.SecretVaultEntry{
		Name: o.Name, Kind: o.Kind, AllowedDomains: o.Domains, RequireApproval: approval,
		Hints: o.Hints, CreatedBy: store.UserIDFromContext(ctx),
	}
	err := st.Create(ctx, e, payload)
	action, msg := store.SecretAuditCreated, fmt.Sprintf("Created secret %q.", o.Name)
	if errors.Is(err, store.ErrSecretVaultNameTaken) {
		entries, lerr := st.List(ctx)
		if lerr != nil {
			return "", lerr
		}
		var existing *store.SecretVaultEntry
		for i := range entries {
			if entries[i].Name == o.Name {
				existing = &entries[i]
			}
		}
		if existing == nil {
			return "", err
		}
		if err = st.ReplaceValue(ctx, existing.ID, payload); err != nil {
			return "", err
		}
		e, action, msg = existing, store.SecretAuditUpdated, fmt.Sprintf("Replaced the value of secret %q.", o.Name)
	}
	if err != nil {
		return "", err
	}
	_ = st.AppendAudit(ctx, &store.SecretVaultAuditEntry{
		EntryID: e.ID, EntryName: e.Name, UserID: store.UserIDFromContext(ctx), Action: action, Outcome: "ok",
	})
	return msg, nil
}

func secretVaultDeleteCmd() *cobra.Command {
	var tenantID, name string
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a secret by name",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, ctx, closeFn, err := openSecretVault(cmd, tenantID)
			if err != nil {
				return err
			}
			defer closeFn()
			entries, err := st.List(ctx)
			if err != nil {
				return err
			}
			for _, e := range entries {
				if e.Name != name {
					continue
				}
				if err := st.Delete(ctx, e.ID); err != nil {
					return err
				}
				_ = st.AppendAudit(ctx, &store.SecretVaultAuditEntry{
					EntryID: e.ID, EntryName: e.Name, UserID: store.UserIDFromContext(ctx),
					Action: store.SecretAuditDeleted, Outcome: "ok",
				})
				fmt.Fprintf(cmd.OutOrStdout(), "Deleted secret %q.\n", name)
				return nil
			}
			return fmt.Errorf("secret %q not found", name)
		},
	}
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "Tenant UUID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Secret name (required)")
	_ = cmd.MarkFlagRequired("tenant-id")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// readSecretValue reads the secret from stdin without echoing it: hidden input
// on a terminal, or the first line of piped input.
func readSecretValue(in *os.File, prompts io.Writer) (string, error) {
	if term.IsTerminal(int(in.Fd())) {
		fmt.Fprint(prompts, "Secret value (hidden): ")
		b, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(prompts)
		if err != nil {
			return "", fmt.Errorf("read secret: %w", err)
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	return readSecretLine(in)
}

func readSecretLine(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read secret: %w", err)
	}
	value := strings.TrimRight(line, "\r\n")
	if value == "" {
		return "", fmt.Errorf("no secret value on stdin")
	}
	return value, nil
}
