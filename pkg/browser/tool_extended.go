package browser

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// MasterTenantUUID is the canonical master tenant ID for DB operations.
var masterTenantUUID = store.MasterTenantID.String()

// tenantIDForDB returns a valid UUID string for database operations.
// Falls back to MasterTenantID when no tenant context is present.
func tenantIDForDB(ctx context.Context) string {
	// First try browser context (propagated from store context in Execute)
	if tid := tenantIDFromCtx(ctx); tid != "" {
		return tid
	}
	// Then try store context directly
	if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil {
		return tid.String()
	}
	return masterTenantUUID
}

// handleAttach connects to an existing browser via CDP URL.
func (t *BrowserTool) handleAttach(ctx context.Context, args map[string]any) *tools.Result {
	cdpURL, _ := args["cdpUrl"].(string)
	if cdpURL == "" {
		return tools.ErrorResult("cdpUrl is required for attach action")
	}
	// SSRF protection: block private/loopback addresses
	if err := ValidateCDPURL(cdpURL); err != nil {
		return tools.ErrorResult(fmt.Sprintf("attach blocked: %v", err))
	}
	if err := t.manager.StartWithAttach(ctx, cdpURL); err != nil {
		return tools.ErrorResult(fmt.Sprintf("attach failed: %v", err))
	}
	return tools.NewResult("Attached to browser successfully.")
}

// handleGetCookies returns cookies for the current page.
func (t *BrowserTool) handleGetCookies(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	cookies, err := t.manager.GetCookies(ctx, targetID)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("getCookies failed: %v", err))
	}
	return jsonResult(cookies)
}

// handleSetCookie sets a cookie on the current page.
func (t *BrowserTool) handleSetCookie(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	cookieMap, ok := args["cookie"].(map[string]any)
	if !ok {
		return tools.ErrorResult("cookie object is required for setCookie action")
	}

	c := &Cookie{}
	if v, ok := cookieMap["name"].(string); ok {
		c.Name = v
	}
	if v, ok := cookieMap["value"].(string); ok {
		c.Value = v
	}
	if v, ok := cookieMap["domain"].(string); ok {
		c.Domain = v
	}
	if v, ok := cookieMap["path"].(string); ok {
		c.Path = v
	}
	if v, ok := cookieMap["secure"].(bool); ok {
		c.Secure = v
	}
	if v, ok := cookieMap["httpOnly"].(bool); ok {
		c.HTTPOnly = v
	}
	if v, ok := cookieMap["sameSite"].(string); ok {
		c.SameSite = v
	}
	if v, ok := cookieMap["expires"].(float64); ok {
		c.Expires = v
	}
	if v, ok := cookieMap["url"].(string); ok {
		c.URL = v
	}

	if c.Name == "" {
		return tools.ErrorResult("cookie.name is required")
	}

	if err := t.manager.SetCookie(ctx, targetID, c); err != nil {
		return tools.ErrorResult(fmt.Sprintf("setCookie failed: %v", err))
	}
	return tools.NewResult("Cookie set successfully.")
}

// handleClearCookies clears all cookies for the current page.
func (t *BrowserTool) handleClearCookies(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	if err := t.manager.ClearCookies(ctx, targetID); err != nil {
		return tools.ErrorResult(fmt.Sprintf("clearCookies failed: %v", err))
	}
	return tools.NewResult("Cookies cleared.")
}

// handleGetStorage returns localStorage or sessionStorage items.
func (t *BrowserTool) handleGetStorage(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	isLocal := true
	if kind, ok := args["storageKind"].(string); ok && kind == "session" {
		isLocal = false
	}
	items, err := t.manager.GetStorage(ctx, targetID, isLocal)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("getStorage failed: %v", err))
	}
	return jsonResult(items)
}

// handleSetStorage sets an item in localStorage or sessionStorage.
func (t *BrowserTool) handleSetStorage(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	isLocal := true
	if kind, ok := args["storageKind"].(string); ok && kind == "session" {
		isLocal = false
	}
	key, _ := args["storageKey"].(string)
	value, _ := args["storageValue"].(string)
	if key == "" {
		return tools.ErrorResult("storageKey is required for setStorage action")
	}
	if err := t.manager.SetStorage(ctx, targetID, isLocal, key, value); err != nil {
		return tools.ErrorResult(fmt.Sprintf("setStorage failed: %v", err))
	}
	return tools.NewResult("Storage item set successfully.")
}

// handleClearStorage clears localStorage or sessionStorage.
func (t *BrowserTool) handleClearStorage(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	isLocal := true
	if kind, ok := args["storageKind"].(string); ok && kind == "session" {
		isLocal = false
	}
	if err := t.manager.ClearStorage(ctx, targetID, isLocal); err != nil {
		return tools.ErrorResult(fmt.Sprintf("clearStorage failed: %v", err))
	}
	return tools.NewResult("Storage cleared.")
}

// handleProfiles lists browser profiles.
func (t *BrowserTool) handleProfiles(ctx context.Context, args map[string]any) *tools.Result {
	if t.storage == nil {
		return tools.ErrorResult("profile storage not configured")
	}
	tenantID := tenantIDFromCtx(ctx)
	if tenantID == "" {
		tenantID = "default"
	}
	profiles, err := t.storage.ListProfiles(tenantID)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("profiles failed: %v", err))
	}
	return jsonResult(profiles)
}

// handleDeleteProfile deletes a browser profile.
func (t *BrowserTool) handleDeleteProfile(ctx context.Context, args map[string]any) *tools.Result {
	if t.storage == nil {
		return tools.ErrorResult("profile storage not configured")
	}
	profile, _ := args["profile"].(string)
	if profile == "" {
		return tools.ErrorResult("profile name is required for deleteProfile action")
	}
	tenantID := tenantIDFromCtx(ctx)
	if tenantID == "" {
		tenantID = "default"
	}
	if err := t.storage.DeleteProfile(tenantID, profile); err != nil {
		return tools.ErrorResult(fmt.Sprintf("deleteProfile failed: %v", err))
	}
	return tools.NewResult(fmt.Sprintf("Profile %q deleted.", profile))
}

// handleFocusTab activates a tab.
func (t *BrowserTool) handleFocusTab(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	if targetID == "" {
		return tools.ErrorResult("targetId is required for focusTab action")
	}
	if err := t.manager.FocusTab(ctx, targetID); err != nil {
		return tools.ErrorResult(fmt.Sprintf("focusTab failed: %v", err))
	}
	return tools.NewResult("Tab focused.")
}

// handleErrors returns captured JavaScript exceptions.
func (t *BrowserTool) handleErrors(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	errors, err := t.manager.GetJSErrors(ctx, targetID)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("errors failed: %v", err))
	}
	return jsonResult(errors)
}

// handleEmulate sets device/viewport emulation on a page.
func (t *BrowserTool) handleEmulate(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	opts := EmulateOpts{}
	if v, ok := args["userAgent"].(string); ok {
		opts.UserAgent = v
	}
	if v, ok := args["width"].(float64); ok {
		opts.Width = int(v)
	}
	if v, ok := args["height"].(float64); ok {
		opts.Height = int(v)
	}
	if v, ok := args["scale"].(float64); ok {
		opts.Scale = v
	}
	if v, ok := args["isMobile"].(bool); ok {
		opts.IsMobile = v
	}
	if v, ok := args["hasTouch"].(bool); ok {
		opts.HasTouch = v
	}
	if v, ok := args["landscape"].(bool); ok {
		opts.Landscape = v
	}
	if err := t.manager.Emulate(ctx, targetID, opts); err != nil {
		return tools.ErrorResult(fmt.Sprintf("emulate failed: %v", err))
	}
	return tools.NewResult("Emulation set successfully.")
}

// handlePDF generates a PDF from the page.
func (t *BrowserTool) handlePDF(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	landscape, _ := args["landscape"].(bool)
	data, err := t.manager.PDF(ctx, targetID, landscape)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("pdf failed: %v", err))
	}

	// Save to workspace/pdfs/ directory
	pdfDir := filepath.Join(os.TempDir(), "goclaw_pdfs")
	if ws := tools.ToolWorkspaceFromCtx(ctx); ws != "" {
		pdfDir = filepath.Join(ws, "pdfs")
	}
	if err := os.MkdirAll(pdfDir, 0755); err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to create pdfs directory: %v", err))
	}
	pdfPath := filepath.Join(pdfDir, fmt.Sprintf("page_%d.pdf", time.Now().UnixNano()))
	if err := os.WriteFile(pdfPath, data, 0644); err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to save PDF: %v", err))
	}
	return tools.NewResult(fmt.Sprintf("PDF saved to %s (%d bytes)", pdfPath, len(data)))
}

// handleSetHeaders sets extra HTTP headers for all requests on a page.
func (t *BrowserTool) handleSetHeaders(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	headersRaw, ok := args["headers"].(map[string]any)
	if !ok {
		return tools.ErrorResult("headers object is required for setHeaders action")
	}
	headers := make(map[string]string, len(headersRaw))
	for k, v := range headersRaw {
		if s, ok := v.(string); ok {
			headers[k] = s
		}
	}
	if err := t.manager.SetExtraHeaders(ctx, targetID, headers); err != nil {
		return tools.ErrorResult(fmt.Sprintf("setHeaders failed: %v", err))
	}
	return tools.NewResult("Extra headers set successfully.")
}

// handleSetOffline enables or disables offline mode for a page.
func (t *BrowserTool) handleSetOffline(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	offline, _ := args["offline"].(bool)
	if err := t.manager.SetOffline(ctx, targetID, offline); err != nil {
		return tools.ErrorResult(fmt.Sprintf("setOffline failed: %v", err))
	}
	if offline {
		return tools.NewResult("Page is now offline.")
	}
	return tools.NewResult("Page is back online.")
}

// handleStartScreencast starts streaming JPEG frames from a page.
func (t *BrowserTool) handleStartScreencast(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	fps := 10
	quality := 80
	if v, ok := args["fps"].(float64); ok && v > 0 {
		fps = int(v)
	}
	if v, ok := args["quality"].(float64); ok && v > 0 {
		quality = int(v)
	}
	_, err := t.manager.StartScreencast(ctx, targetID, fps, quality)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("startScreencast failed: %v", err))
	}
	return tools.NewResult(fmt.Sprintf("Screencast started (fps=%d, quality=%d). Frames are streaming.", fps, quality))
}

// handleStopScreencast stops the screencast for a page.
func (t *BrowserTool) handleStopScreencast(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	if err := t.manager.StopScreencast(ctx, targetID); err != nil {
		return tools.ErrorResult(fmt.Sprintf("stopScreencast failed: %v", err))
	}
	return tools.NewResult("Screencast stopped.")
}

// --- Proxy handlers ---

// safeProxy strips sensitive fields before returning proxy info to LLM.
type safeProxy struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Geo       string `json:"geo,omitempty"`
	IsEnabled bool   `json:"isEnabled"`
	IsHealthy bool   `json:"isHealthy"`
}

func (t *BrowserTool) handleProxyList(ctx context.Context) *tools.Result {
	if t.proxy == nil {
		return tools.ErrorResult("proxy management not configured")
	}
	proxies, err := t.proxy.List(ctx, tenantIDForDB(ctx))
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("proxy.list failed: %v", err))
	}
	safe := make([]safeProxy, len(proxies))
	for i, p := range proxies {
		safe[i] = safeProxy{ID: p.ID, Name: p.Name, URL: p.URL, Geo: p.Geo, IsEnabled: p.IsEnabled, IsHealthy: p.IsHealthy}
	}
	return jsonResult(safe)
}

func (t *BrowserTool) handleProxyAdd(ctx context.Context, args map[string]any) *tools.Result {
	if t.proxy == nil {
		return tools.ErrorResult("proxy management not configured")
	}
	proxyURL, _ := args["proxyUrl"].(string)
	proxyName, _ := args["proxyName"].(string)
	if proxyURL == "" || proxyName == "" {
		return tools.ErrorResult("proxyUrl and proxyName are required for proxy.add")
	}
	if err := ValidateProxyURL(proxyURL); err != nil {
		return tools.ErrorResult(fmt.Sprintf("proxy.add blocked: %v", err))
	}
	p := &store.BrowserProxy{
		Name: proxyName,
		URL:  proxyURL,
	}
	if v, ok := args["proxyGeo"].(string); ok {
		p.Geo = v
	}
	if v, ok := args["proxyUsername"].(string); ok {
		p.Username = v
	}
	if v, ok := args["proxyPassword"].(string); ok {
		p.Password = v
	}
	if err := t.proxy.Add(ctx, p); err != nil {
		return tools.ErrorResult(fmt.Sprintf("proxy.add failed: %v", err))
	}
	return tools.NewResult(fmt.Sprintf("Proxy %q added.", proxyName))
}

func (t *BrowserTool) handleProxyRemove(ctx context.Context, args map[string]any) *tools.Result {
	if t.proxy == nil {
		return tools.ErrorResult("proxy management not configured")
	}
	proxyID, _ := args["proxyId"].(string)
	if proxyID == "" {
		return tools.ErrorResult("proxyId is required for proxy.remove")
	}
	if err := t.proxy.Remove(ctx, proxyID, tenantIDForDB(ctx)); err != nil {
		return tools.ErrorResult(fmt.Sprintf("proxy.remove failed: %v", err))
	}
	return tools.NewResult("Proxy removed.")
}

func (t *BrowserTool) handleProxyHealth(ctx context.Context) *tools.Result {
	if t.proxy == nil {
		return tools.ErrorResult("proxy management not configured")
	}
	if err := t.proxy.RunHealthCheck(ctx, tenantIDForDB(ctx)); err != nil {
		return tools.ErrorResult(fmt.Sprintf("proxy.health failed: %v", err))
	}
	return tools.NewResult("Proxy health check complete.")
}

// --- Extension handlers ---

func (t *BrowserTool) handleExtensionList(ctx context.Context) *tools.Result {
	if t.extension == nil {
		return tools.ErrorResult("extension management not configured")
	}
	exts, err := t.extension.List(ctx, tenantIDForDB(ctx))
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("extension.list failed: %v", err))
	}
	return jsonResult(exts)
}

func (t *BrowserTool) handleExtensionAdd(ctx context.Context, args map[string]any) *tools.Result {
	if t.extension == nil {
		return tools.ErrorResult("extension management not configured")
	}
	name, _ := args["extensionName"].(string)
	path, _ := args["extensionPath"].(string)
	if name == "" || path == "" {
		return tools.ErrorResult("extensionName and extensionPath are required for extension.add")
	}
	// Path traversal protection: validate path within workspace
	ws := tools.ToolWorkspaceFromCtx(ctx)
	if err := ValidateExtensionPath(path, ws); err != nil {
		return tools.ErrorResult(fmt.Sprintf("extension.add blocked: %v", err))
	}
	e := &store.BrowserExtension{
		Name:    name,
		Path:    path,
		Enabled: true,
	}
	if err := t.extension.Add(ctx, e); err != nil {
		return tools.ErrorResult(fmt.Sprintf("extension.add failed: %v", err))
	}
	return tools.NewResult(fmt.Sprintf("Extension %q added.", name))
}

func (t *BrowserTool) handleExtensionRemove(ctx context.Context, args map[string]any) *tools.Result {
	if t.extension == nil {
		return tools.ErrorResult("extension management not configured")
	}
	extID, _ := args["extensionId"].(string)
	if extID == "" {
		return tools.ErrorResult("extensionId is required for extension.remove")
	}
	if err := t.extension.Remove(ctx, extID); err != nil {
		return tools.ErrorResult(fmt.Sprintf("extension.remove failed: %v", err))
	}
	return tools.NewResult("Extension removed.")
}

// --- Audit handler ---

func (t *BrowserTool) handleAuditList(ctx context.Context, args map[string]any) *tools.Result {
	if t.audit == nil {
		return tools.ErrorResult("audit logging not configured")
	}
	opts := store.AuditListOpts{Limit: 50}
	if v, ok := args["auditAction"].(string); ok {
		opts.Action = v
	}
	if v, ok := args["auditLimit"].(float64); ok && v > 0 {
		opts.Limit = int(v)
	}
	entries, total, err := t.audit.List(ctx, tenantIDForDB(ctx), opts)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("audit.list failed: %v", err))
	}
	return jsonResult(map[string]any{
		"entries": entries,
		"total":   total,
	})
}

// --- Storage extras ---

func (t *BrowserTool) handleStoragePurge(ctx context.Context, args map[string]any) *tools.Result {
	if t.storage == nil {
		return tools.ErrorResult("profile storage not configured")
	}
	profile, _ := args["profile"].(string)
	if profile == "" {
		return tools.ErrorResult("profile is required for storage.purge")
	}
	tenantID := tenantIDFromCtx(ctx)
	if tenantID == "" {
		tenantID = "default"
	}
	if err := t.storage.PurgeSession(tenantID, profile); err != nil {
		return tools.ErrorResult(fmt.Sprintf("storage.purge failed: %v", err))
	}
	return tools.NewResult(fmt.Sprintf("Profile %q purged.", profile))
}

func (t *BrowserTool) handleStorageCleanup(ctx context.Context, args map[string]any) *tools.Result {
	if t.storage == nil {
		return tools.ErrorResult("profile storage not configured")
	}
	maxAgeHours, _ := args["maxAge"].(float64)
	if maxAgeHours <= 0 {
		return tools.ErrorResult("maxAge (hours) is required for storage.cleanup")
	}
	tenantID := tenantIDFromCtx(ctx)
	if tenantID == "" {
		tenantID = "default"
	}
	removed, err := t.storage.Cleanup(tenantID, time.Duration(maxAgeHours)*time.Hour)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("storage.cleanup failed: %v", err))
	}
	return tools.NewResult(fmt.Sprintf("Removed %d old profiles.", removed))
}

// --- LiveView handler ---

func (t *BrowserTool) handleLiveViewCreate(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	if targetID == "" {
		return tools.ErrorResult("targetId is required for liveview.create")
	}

	if t.sessions == nil {
		return tools.ErrorResult("screencast sessions not configured")
	}

	// Determine expiry (default 60 min, max 1440 min = 24h).
	expiryMinutes := 60
	if v, ok := args["expiresMinutes"].(float64); ok && v > 0 {
		expiryMinutes = int(v)
		if expiryMinutes > 1440 {
			expiryMinutes = 1440
		}
	}

	mode, _ := args["mode"].(string)
	if mode == "" {
		mode = "view"
	}

	// Generate crypto-random token (20 bytes = 40 hex chars).
	tokenBytes := make([]byte, 20)
	if _, err := rand.Read(tokenBytes); err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to generate token: %v", err))
	}
	token := hex.EncodeToString(tokenBytes)

	expiresAt := time.Now().Add(time.Duration(expiryMinutes) * time.Minute)

	sess := &store.ScreencastSession{
		Token:     token,
		TargetID:  targetID,
		Mode:      mode,
		ExpiresAt: expiresAt,
	}

	if err := t.sessions.Create(ctx, sess); err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to create live view session: %v", err))
	}

	path := "/browser/live/" + token
	shareURL := path
	if u := t.resolvePublicURL(ctx); u != "" {
		shareURL = u + path
	}

	return jsonResult(map[string]string{
		"url":       shareURL,
		"token":     token,
		"targetId":  targetID,
		"expiresAt": expiresAt.Format(time.RFC3339),
		"mode":      mode,
	})
}

// browserSettings is the JSON schema for builtin_tools.settings["browser"].
type browserSettings struct {
	PublicURL string `json:"public_url,omitempty"`
}

// resolvePublicURL returns the public base URL for share links.
// Priority: DB settings (from context) > struct field (from config/env).
func (t *BrowserTool) resolvePublicURL(ctx context.Context) string {
	if settings := tools.BuiltinToolSettingsFromCtx(ctx); settings != nil {
		if raw, ok := settings["browser"]; ok && len(raw) > 0 {
			var s browserSettings
			if json.Unmarshal(raw, &s) == nil && s.PublicURL != "" {
				return strings.TrimRight(s.PublicURL, "/")
			}
		}
	}
	return strings.TrimRight(t.publicURL, "/")
}
