package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/channels/bitrix24"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// bitrixPortalCmd wires `goclaw bitrix-portal ...` — direct-DB management of
// `bitrix_portals` rows. It seeds the portal row required before an operator
// runs the OAuth install flow at `/bitrix24/install`.
//
// Writes go through PGBitrixPortalStore so GOCLAW_ENCRYPTION_KEY is applied
// to the credentials column the same way the runtime would. Reads via `list`
// deliberately don't print secrets — credentials stay encrypted at rest, and
// a debug tool dumping them would be a regression.
func bitrixPortalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bitrix-portal",
		Short: "Manage Bitrix24 portals (direct DB access; postgres only)",
		Long: `Manage Bitrix24 portal rows in the database.

GoClaw expects a ` + "`bitrix_portals`" + ` row to exist before an operator runs the
OAuth install flow at ` + "`/bitrix24/install`" + `. This command seeds that row without
requiring SQL access to the database.`,
	}
	cmd.AddCommand(bitrixPortalCreateCmd())
	cmd.AddCommand(bitrixPortalListCmd())
	cmd.AddCommand(bitrixPortalUpdateCredentialsCmd())
	cmd.AddCommand(bitrixPortalSetPublicURLCmd())
	return cmd
}

// bitrixPortalUpdateCredentialsCmd swaps client_id/client_secret on an
// existing portal row. Used when rotating client_secret OR migrating from
// local app to marketplace app on the same Bitrix24 portal — the row stays
// (so channel configs keep working by name) but OAuth identity changes.
//
// Side effect: the existing OAuth state token is invalidated by default
// because it was minted against the OLD client_id/secret and will fail
// to refresh against new credentials. Pass --keep-state to skip that.
// After update, the portal admin must visit the install URL to obtain
// new tokens against the new credentials.
func bitrixPortalUpdateCredentialsCmd() *cobra.Command {
	var (
		tenantID     string
		name         string
		clientID     string
		clientSecret string
		keepState    bool
	)
	cmd := &cobra.Command{
		Use:   "update-credentials",
		Short: "Replace client_id/client_secret on an existing portal row",
		Long: `Update OAuth credentials on an existing bitrix_portals row.

Use this when rotating client_secret or migrating from local app to
marketplace app. The OAuth state token is cleared by default (state from
old credentials cannot refresh under new client_id/secret); pass
--keep-state only if rotating the secret of the SAME application.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(name) == "" ||
				strings.TrimSpace(clientID) == "" || strings.TrimSpace(clientSecret) == "" {
				return fmt.Errorf("--tenant-id, --name, --client-id, --client-secret are all required")
			}
			tid, err := uuid.Parse(tenantID)
			if err != nil {
				return fmt.Errorf("invalid --tenant-id: %w", err)
			}

			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			db, err := sql.Open("pgx", dsn)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer db.Close()
			if err := db.PingContext(cmd.Context()); err != nil {
				return fmt.Errorf("ping db: %w", err)
			}

			encKey := os.Getenv("GOCLAW_ENCRYPTION_KEY")
			if encKey == "" {
				fmt.Fprintln(os.Stderr, "WARNING: GOCLAW_ENCRYPTION_KEY is not set — credentials will be stored UNENCRYPTED")
			}

			creds := store.BitrixPortalCredentials{
				ClientID:     clientID,
				ClientSecret: clientSecret,
			}
			credsJSON, err := json.Marshal(creds)
			if err != nil {
				return fmt.Errorf("marshal credentials: %w", err)
			}

			portalStore := pg.NewPGBitrixPortalStore(db, encKey)
			if err := portalStore.UpdateCredentials(cmd.Context(), tid, name, credsJSON); err != nil {
				return fmt.Errorf("update credentials: %w", err)
			}
			if !keepState {
				if err := portalStore.UpdateState(cmd.Context(), tid, name, nil); err != nil {
					return fmt.Errorf("clear state: %w", err)
				}
			}

			fmt.Printf("Updated bitrix_portals row:\n")
			fmt.Printf("  tenant_id: %s\n", tid)
			fmt.Printf("  name:      %s\n", name)
			if !keepState {
				fmt.Printf("  state:     cleared (admin must reinstall to mint new tokens)\n")
			} else {
				fmt.Printf("  state:     kept (only valid if rotating same-app secret)\n")
			}
			fmt.Printf("\nNext step — have the portal admin visit:\n")
			fmt.Printf("  https://<public_url>/bitrix24/install?state=%s:%s\n", tid, name)
			return nil
		},
	}
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "Tenant UUID this portal belongs to (required)")
	cmd.Flags().StringVar(&name, "name", "", "Portal name to update (required)")
	cmd.Flags().StringVar(&clientID, "client-id", "", "New Bitrix24 application client_id (required)")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "New Bitrix24 application client_secret (required)")
	cmd.Flags().BoolVar(&keepState, "keep-state", false, "Keep existing OAuth state token (only safe when rotating secret of SAME application)")
	return cmd
}

func bitrixPortalCreateCmd() *cobra.Command {
	var (
		tenantID     string
		name         string
		domain       string
		clientID     string
		clientSecret string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a bitrix_portals row with client_id/client_secret",
		Long: `Create a new Bitrix24 portal registration.

After the row exists, direct the portal admin to
` + "`https://<public_url>/bitrix24/install?state=<tenant_id>:<name>`" + `
to authorize the app — the install handler writes the OAuth token into the
` + "`state`" + ` column of this same row.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(name) == "" ||
				strings.TrimSpace(domain) == "" || strings.TrimSpace(clientID) == "" ||
				strings.TrimSpace(clientSecret) == "" {
				return fmt.Errorf("--tenant-id, --name, --domain, --client-id, --client-secret are all required")
			}
			tid, err := uuid.Parse(tenantID)
			if err != nil {
				return fmt.Errorf("invalid --tenant-id: %w", err)
			}
			// Strip protocol + trailing slash from domain; Bitrix24 identifies
			// the portal by bare host (e.g. `tamgiac.bitrix24.com`).
			dom := normalizeBitrixDomain(domain)

			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			db, err := sql.Open("pgx", dsn)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer db.Close()
			if err := db.PingContext(cmd.Context()); err != nil {
				return fmt.Errorf("ping db: %w", err)
			}

			encKey := os.Getenv("GOCLAW_ENCRYPTION_KEY")
			if encKey == "" {
				// Not fatal — pg store passes plaintext through when the key is
				// empty — but the runtime gateway would also run unencrypted,
				// which is almost never what a production deploy wants. Warn
				// loud so the operator notices instead of silently storing
				// client_secret as cleartext.
				fmt.Fprintln(os.Stderr, "WARNING: GOCLAW_ENCRYPTION_KEY is not set — credentials will be stored UNENCRYPTED")
			}

			creds := store.BitrixPortalCredentials{
				ClientID:     clientID,
				ClientSecret: clientSecret,
			}
			credsJSON, err := json.Marshal(creds)
			if err != nil {
				return fmt.Errorf("marshal credentials: %w", err)
			}

			portalStore := pg.NewPGBitrixPortalStore(db, encKey)
			data := &store.BitrixPortalData{
				TenantID:    tid,
				Name:        name,
				Domain:      dom,
				Credentials: credsJSON,
				// State stays empty — it's populated by /bitrix24/install
				// after the portal admin authorizes the app.
			}
			if err := portalStore.Create(cmd.Context(), data); err != nil {
				return fmt.Errorf("create portal: %w", err)
			}

			fmt.Printf("Created bitrix_portals row:\n")
			fmt.Printf("  id:        %s\n", data.ID)
			fmt.Printf("  tenant_id: %s\n", data.TenantID)
			fmt.Printf("  name:      %s\n", data.Name)
			fmt.Printf("  domain:    %s\n", data.Domain)
			fmt.Printf("\nNext step — have the portal admin visit:\n")
			fmt.Printf("  https://<public_url>/bitrix24/install?state=%s:%s\n", data.TenantID, data.Name)
			fmt.Printf("(public_url must match the `public_url` field on the channel_instance config.)\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "Tenant UUID this portal belongs to (required)")
	cmd.Flags().StringVar(&name, "name", "", "Short portal name, referenced by channel_instance.config.portal (required)")
	cmd.Flags().StringVar(&domain, "domain", "", "Bitrix24 portal host, e.g. tamgiac.bitrix24.com (required)")
	cmd.Flags().StringVar(&clientID, "client-id", "", "Bitrix24 application client_id / application_id (required)")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "Bitrix24 application client_secret / application key (required)")
	return cmd
}

func bitrixPortalListCmd() *cobra.Command {
	var tenantID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List bitrix_portals rows (optionally scoped to one tenant)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			db, err := sql.Open("pgx", dsn)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer db.Close()
			if err := db.PingContext(cmd.Context()); err != nil {
				return fmt.Errorf("ping db: %w", err)
			}

			portalStore := pg.NewPGBitrixPortalStore(db, os.Getenv("GOCLAW_ENCRYPTION_KEY"))

			var rows []store.BitrixPortalData
			if tenantID == "" {
				rows, err = portalStore.ListAllForLoader(cmd.Context())
			} else {
				tid, parseErr := uuid.Parse(tenantID)
				if parseErr != nil {
					return fmt.Errorf("invalid --tenant-id: %w", parseErr)
				}
				rows, err = portalStore.ListByTenant(cmd.Context(), tid)
			}
			if err != nil {
				return fmt.Errorf("list portals: %w", err)
			}

			if len(rows) == 0 {
				fmt.Println("(no portals)")
				return nil
			}
			fmt.Printf("%-36s  %-36s  %-24s  %s\n", "ID", "TENANT_ID", "NAME", "DOMAIN")
			for _, r := range rows {
				// Credentials deliberately not printed. If the runtime couldn't
				// decrypt them the scan already logged a warning; we tag that
				// case here so operators spot a corrupt row at a glance.
				nameCol := r.Name
				if len(r.Credentials) == 0 {
					nameCol += " (creds:empty)"
				}
				fmt.Printf("%-36s  %-36s  %-24s  %s\n", r.ID, r.TenantID, nameCol, r.Domain)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "Filter to one tenant UUID (optional)")
	return cmd
}

// bitrixPortalSetPublicURLCmd backfills bitrix_portals.state.public_url for
// portals installed before automatic public_url capture existed. One-shot op:
// after running once, channel registration succeeds for new bots and future
// reinstalls update the URL automatically.
//
// Usage:
//
//	goclaw bitrix-portal set-public-url \
//	  --tenant-id <uuid> --name <portal> --url https://goclaw.example.com
func bitrixPortalSetPublicURLCmd() *cobra.Command {
	var (
		tenantID string
		name     string
		url      string
	)
	cmd := &cobra.Command{
		Use:   "set-public-url",
		Short: "Backfill state.public_url for a portal installed pre Phase-01",
		Long: `Set the gateway-public URL used to register Bitrix24 imbot event handlers.

Required for portals that were installed before the goclaw release that
auto-captures the URL from the /bitrix24/install callback. Without it, the
factory cannot build a valid EVENT_MESSAGE_ADD URL for new channels.

After running once, the value is persisted in bitrix_portals.state.public_url
and reused on every restart. Subsequent reinstalls (when the public URL
rotates) overwrite the value automatically — this command is only for the
initial backfill.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(name) == "" ||
				strings.TrimSpace(url) == "" {
				return fmt.Errorf("--tenant-id, --name, --url are all required")
			}
			tid, err := uuid.Parse(tenantID)
			if err != nil {
				return fmt.Errorf("invalid --tenant-id: %w", err)
			}

			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			db, err := sql.Open("pgx", dsn)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer db.Close()
			if err := db.PingContext(cmd.Context()); err != nil {
				return fmt.Errorf("ping db: %w", err)
			}

			encKey := os.Getenv("GOCLAW_ENCRYPTION_KEY")
			if encKey == "" {
				fmt.Fprintln(os.Stderr, "WARNING: GOCLAW_ENCRYPTION_KEY is not set — state will be read/written UNENCRYPTED")
			}

			portalStore := pg.NewPGBitrixPortalStore(db, encKey)
			portal, err := bitrix24.NewPortal(cmd.Context(), tid, name, portalStore, encKey)
			if err != nil {
				return fmt.Errorf("load portal: %w", err)
			}
			if err := portal.UpdatePublicURL(cmd.Context(), strings.TrimRight(url, "/")); err != nil {
				return fmt.Errorf("update public_url: %w", err)
			}

			fmt.Printf("Updated bitrix_portals.state.public_url:\n")
			fmt.Printf("  tenant_id:  %s\n", tid)
			fmt.Printf("  name:       %s\n", name)
			fmt.Printf("  public_url: %s\n", portal.PublicURL())
			fmt.Printf("\nNew channels on this portal can now imbot.register successfully.\n")
			fmt.Printf("Set BITRIX24_FORCE_REREGISTER=1 + restart if existing channels were\n")
			fmt.Printf("registered against a stale URL and need to refresh Bitrix-side handlers.\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "Tenant UUID this portal belongs to (required)")
	cmd.Flags().StringVar(&name, "name", "", "Portal name (required)")
	cmd.Flags().StringVar(&url, "url", "", "Gateway public URL, e.g. https://goclaw.tamgiac.com (required)")
	return cmd
}

// normalizeBitrixDomain strips scheme and trailing slashes so callers can paste
// either `https://tamgiac.bitrix24.com/` or bare `tamgiac.bitrix24.com` and get
// a consistent value in the DB. Bitrix24's OAuth callback compares the bare
// host, so storing it with scheme would silently break the install flow.
func normalizeBitrixDomain(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimSuffix(s, "/")
	return s
}
