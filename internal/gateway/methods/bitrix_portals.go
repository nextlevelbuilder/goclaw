package methods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// BitrixPortalsMethods exposes self-service portal management over WS RPC.
// All methods are tenant-scoped (resolved from client.TenantID(), never from
// caller-supplied params); list/get_install_url are open to any authenticated
// tenant member, create/delete require RoleAdmin.
//
// gatewayPublicURL is a late-bound provider returning the gateway's externally
// reachable base URL (e.g. "https://goclaw.tamgiac.com"). Used to build the
// install URL we hand back to the UI. The HTTP middleware updates this on
// every authenticated request so the value tracks ingress changes without
// requiring static config — see SetGatewayPublicURLSnapshot.
type BitrixPortalsMethods struct {
	portalStore      store.BitrixPortalStore
	channelStore     store.ChannelInstanceStore // for "portal_in_use" check on delete
	gatewayPublicURL func() string
}

// NewBitrixPortalsMethods constructs the handler. gatewayPublicURL may return
// empty string when the gateway hasn't observed any public-URL request yet;
// callers see an INVALID_REQUEST error with a hint to open the UI via the
// public URL first.
func NewBitrixPortalsMethods(
	portalStore store.BitrixPortalStore,
	channelStore store.ChannelInstanceStore,
	gatewayPublicURL func() string,
) *BitrixPortalsMethods {
	return &BitrixPortalsMethods{
		portalStore:      portalStore,
		channelStore:     channelStore,
		gatewayPublicURL: gatewayPublicURL,
	}
}

func (m *BitrixPortalsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodBitrixPortalsList, m.handleList)
	router.Register(protocol.MethodBitrixPortalsCreate, m.handleCreate)
	router.Register(protocol.MethodBitrixPortalsGetInstallURL, m.handleGetInstallURL)
	router.Register(protocol.MethodBitrixPortalsDelete, m.handleDelete)
}

// bitrixPortalView is the wire shape returned to UI. Credentials are NEVER
// included — that's the entire point of having a dedicated view struct.
type bitrixPortalView struct {
	Name      string `json:"name"`
	Domain    string `json:"domain"`
	Installed bool   `json:"installed"`
	PublicURL string `json:"public_url,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// Validation regexes. Kept package-level so they compile once and tests can
// reference them directly.
var (
	// Bitrix24 cloud portal hosts. Matches *.bitrix24.{com,eu,ru,de,fr,jp,in,kz,ua,by,vn,tr,es,com.br,com.ar}
	// plus self-hosted *.bitrix.info. Subdomain regex matches DNS label rules.
	bitrixCloudDomainRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.(bitrix24\.(com|eu|ru|de|fr|jp|in|kz|ua|by|vn|tr|es|com\.br|com\.ar)|bitrix\.info)$`)

	// Valid hostname regex for self-hosted Bitrix24 instances (custom domains).
	// Accepts any valid FQDN or hostname with optional port (e.g. bx.example.com, portal.internal:8443).
	selfHostedDomainRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*(\:\d+)?$`)

	// Portal name: lowercase slug used in install state token + channel config
	// reference. Underscore allowed for legacy CLI-created portals.
	portalNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}[a-z0-9]$`)
)

// handleList returns all portals owned by the caller's tenant. Open to any
// authenticated tenant member — channel-form needs to populate a dropdown
// even for operator-role users; credentials are masked.
func (m *BitrixPortalsMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	tid := client.TenantID()
	if tid == uuid.Nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgUnauthorized)))
		return
	}

	rows, err := m.portalStore.ListByTenant(ctx, tid)
	if err != nil {
		slog.Error("bitrix.portals.list failed", "tenant", tid, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "bitrix portals")))
		return
	}

	views := make([]bitrixPortalView, 0, len(rows))
	for _, row := range rows {
		views = append(views, portalRowToView(row))
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"portals": views,
	}))
}

// handleCreate provisions a new portal row. Requires RoleAdmin to prevent
// operators from spending tenant credentials. Tokens are minted later by the
// install handler; this RPC only persists client_id/client_secret and returns
// the install URL the admin must visit.
func (m *BitrixPortalsMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgUnauthorized)))
		return
	}
	tid := client.TenantID()
	if tid == uuid.Nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgUnauthorized)))
		return
	}

	var params struct {
		Name         string `json:"name"`
		Domain       string `json:"domain"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}
	name := strings.ToLower(strings.TrimSpace(params.Name))
	domain := strings.ToLower(strings.TrimSpace(params.Domain))
	clientID := strings.TrimSpace(params.ClientID)
	clientSecret := strings.TrimSpace(params.ClientSecret)

	if !portalNameRegex.MatchString(name) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "name: lowercase letters, digits, hyphen, underscore (2-64 chars)")))
		return
	}
	if !bitrixCloudDomainRegex.MatchString(domain) && !selfHostedDomainRegex.MatchString(domain) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "domain: must be a valid hostname (e.g. *.bitrix24.com, *.bitrix.info, or your self-hosted domain)")))
		return
	}
	// SSRF + port validation for self-hosted domains (cloud domains are
	// Bitrix-operated and implicitly trusted).
	if !bitrixCloudDomainRegex.MatchString(domain) {
		if err := validateSelfHostedDomain(domain); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "domain: "+err.Error())))
			return
		}
	}
	if clientID == "" || clientSecret == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "client_id and client_secret")))
		return
	}

	// Build the install URL BEFORE persisting the row. If the gateway hasn't
	// observed a public URL yet (snapshot empty), reject the request now —
	// persisting a row we can't authorize would create an orphan that the
	// admin can't recover without a delete UI we don't yet have. Better to
	// fail fast and have the admin reopen the UI through their public URL.
	installURL, urlErr := m.buildInstallURL(tid, name)
	if urlErr != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrFailedPrecondition, urlErr.Error()))
		return
	}

	credsJSON, err := json.Marshal(store.BitrixPortalCredentials{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	})
	if err != nil {
		slog.Error("bitrix.portals.create: marshal creds", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "create portal")))
		return
	}

	row := &store.BitrixPortalData{
		TenantID:    tid,
		Name:        name,
		Domain:      domain,
		Credentials: credsJSON,
	}
	if err := m.portalStore.Create(ctx, row); err != nil {
		// Duplicate (tenant_id, name) UNIQUE constraint surfaces as a store
		// error string; map to ALREADY_EXISTS so UI can show an inline error.
		if isDuplicateKeyErr(err) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrAlreadyExists, i18n.T(locale, i18n.MsgAlreadyExists, "portal", name)))
			return
		}
		slog.Error("bitrix.portals.create failed", "tenant", tid, "name", name, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "portal", err.Error())))
		return
	}

	slog.Info("bitrix.portals.create", "tenant", tid, "name", name, "domain", domain)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"name":        row.Name,
		"domain":      row.Domain,
		"install_url": installURL,
	}))
}

// handleGetInstallURL re-builds the install URL for an existing portal row.
// Used by UI to resume an interrupted authorize flow (user closed modal
// before authorizing). Open to any tenant member — URL is not a secret.
func (m *BitrixPortalsMethods) handleGetInstallURL(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	tid := client.TenantID()
	if tid == uuid.Nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgUnauthorized)))
		return
	}
	var params struct {
		Name string `json:"name"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	name := strings.ToLower(strings.TrimSpace(params.Name))
	if name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
		return
	}

	// Verify the portal exists in this tenant — prevent fishing for portal
	// names across tenants by trying every URL combination.
	if _, err := m.portalStore.GetByName(ctx, tid, name); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "portal", name)))
		return
	}

	installURL, err := m.buildInstallURL(tid, name)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrFailedPrecondition, err.Error()))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"install_url": installURL,
	}))
}

// handleDelete removes a portal row. Requires RoleAdmin. Blocked when any
// channel_instance in this tenant references the portal — UI must delete
// the channel first (or the operator can disable the channel before
// reassigning to a different portal).
func (m *BitrixPortalsMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgUnauthorized)))
		return
	}
	tid := client.TenantID()
	if tid == uuid.Nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgUnauthorized)))
		return
	}
	var params struct {
		Name string `json:"name"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	name := strings.ToLower(strings.TrimSpace(params.Name))
	if name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
		return
	}

	// In-use guard: scan tenant channels and reject delete if any of them
	// reference this portal via config.portal. List is small per-tenant —
	// no need for a dedicated indexed query.
	users, err := m.findChannelsUsingPortal(ctx, name)
	if err != nil {
		slog.Error("bitrix.portals.delete: in-use check failed", "tenant", tid, "name", name, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "delete portal")))
		return
	}
	if len(users) > 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrFailedPrecondition,
			i18n.T(locale, i18n.MsgInvalidRequest, "portal is used by channel(s): "+strings.Join(users, ", "))))
		return
	}

	if err := m.portalStore.Delete(ctx, tid, name); err != nil {
		slog.Error("bitrix.portals.delete failed", "tenant", tid, "name", name, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "portal", err.Error())))
		return
	}
	slog.Info("bitrix.portals.delete", "tenant", tid, "name", name)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"status": "deleted"}))
}

// findChannelsUsingPortal returns the names of channel_instances in the
// caller's tenant scope (resolved from ctx by the store) that reference
// the given portal name via config.portal.
func (m *BitrixPortalsMethods) findChannelsUsingPortal(ctx context.Context, portalName string) ([]string, error) {
	if m.channelStore == nil {
		return nil, nil
	}
	rows, err := m.channelStore.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	var users []string
	for _, inst := range rows {
		if inst.ChannelType != "bitrix24" || len(inst.Config) == 0 {
			continue
		}
		var cfg struct {
			Portal string `json:"portal"`
		}
		if err := json.Unmarshal(inst.Config, &cfg); err != nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(cfg.Portal), portalName) {
			users = append(users, inst.Name)
		}
	}
	return users, nil
}

// buildInstallURL composes the URL the portal admin must visit to authorize
// the app. State token <tenant_id>:<portal_name> is verified by the install
// handler (router.go) and ties the OAuth callback to the correct row.
func (m *BitrixPortalsMethods) buildInstallURL(tid uuid.UUID, name string) (string, error) {
	if m.gatewayPublicURL == nil {
		return "", errors.New("gateway public URL provider not wired")
	}
	base := strings.TrimRight(strings.TrimSpace(m.gatewayPublicURL()), "/")
	if base == "" {
		return "", errors.New("gateway public URL unknown — open the UI via your public goclaw URL first, then retry")
	}
	return base + "/bitrix24/install?state=" + tid.String() + ":" + name, nil
}

// portalRowToView converts a store row into the masked wire view. Decodes
// `state` JSON to surface `installed` + `public_url` without exposing the
// underlying token blob.
func portalRowToView(row store.BitrixPortalData) bitrixPortalView {
	v := bitrixPortalView{
		Name:   row.Name,
		Domain: row.Domain,
	}
	if !row.CreatedAt.IsZero() {
		v.CreatedAt = row.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if len(row.State) > 0 {
		var st store.BitrixPortalState
		if err := json.Unmarshal(row.State, &st); err == nil {
			v.Installed = st.RefreshToken != ""
			v.PublicURL = st.PublicURL
		}
	}
	return v
}

// lookupHost is the DNS resolver used by validateSelfHostedDomain.
// Replaced in tests to avoid real network calls and to exercise multi-IP
// SSRF bypass scenarios.
var lookupHost = net.LookupHost

// validateSelfHostedDomain checks a self-hosted Bitrix24 domain for SSRF
// risks and invalid port ranges. Cloud domains (*.bitrix24.*, *.bitrix.info)
// are Bitrix-operated and implicitly trusted — this function is only called
// for custom/self-hosted domains.
//
// Policy:
//   - Rejects literal private/loopback/metadata IPs (127.x, 10.x, 192.168.x,
//     169.254.x, ::1, fc00::, etc.)
//   - Rejects hostnames where ANY resolved IP is blocked (not just the first)
//   - Rejects .localhost and .local TLDs (commonly used for local dev)
//   - Validates port range 1-65535 when a port is specified
func validateSelfHostedDomain(domain string) error {
	// Extract host and optional port.
	host := domain
	portStr := ""
	if idx := strings.LastIndex(domain, ":"); idx != -1 {
		host = domain[:idx]
		portStr = domain[idx+1:]
	}

	// Validate port range if present.
	if portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("port must be 1-65535")
		}
	}

	// Reject .localhost and .local TLDs (commonly used for local development).
	lowerHost := strings.ToLower(host)
	if strings.HasSuffix(lowerHost, ".localhost") || strings.HasSuffix(lowerHost, ".local") || lowerHost == "localhost" {
		return fmt.Errorf("private/internal hostnames (localhost, .local, .localhost) are not allowed")
	}

	// If the host is a literal IP, check it against blocked CIDRs.
	if ip := net.ParseIP(host); ip != nil {
		if security.IsBlocked(ip) {
			return fmt.Errorf("IP %s is in a blocked range (loopback/private/metadata)", ip)
		}
		return nil
	}

	// For hostnames, resolve and check ALL returned IPs. DNS result ordering
	// is not a security boundary — a resolver may return a public IP first
	// followed by a private one (e.g. split-horizon, fallback addresses).
	addrs, err := lookupHost(host)
	if err != nil {
		return fmt.Errorf("cannot resolve hostname %q", host)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("hostname %q resolved to no addresses", host)
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			return fmt.Errorf("resolved address %q is not a valid IP", addr)
		}
		if security.IsBlocked(ip) {
			return fmt.Errorf("hostname %q resolved to blocked IP %s (loopback/private/metadata)", host, ip)
		}
	}
	return nil
}

// isDuplicateKeyErr probes a store error for a UNIQUE violation. Kept as a
// string substring match because the store interface doesn't expose typed
// duplicate errors and we want consistent behaviour between pg + sqlite
// backends. Covers Postgres (`duplicate key`, SQLSTATE 23505) + SQLite
// (`UNIQUE constraint failed`).
func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "23505")
}
