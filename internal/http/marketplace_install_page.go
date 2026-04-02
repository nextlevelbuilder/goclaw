package http

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MarketplaceInstallHandler serves the install confirmation page for Hub marketplace.
type MarketplaceInstallHandler struct {
	marketplaceAPIKey string // GOCLAW_MARKETPLACE_API_KEY — used to verify signatures
	gatewayToken      string // for authenticating local /v1/teams/import calls
	localAPIPort      int
	db                *sql.DB
	installTokens     map[string]time.Time // one-time tokens for POST install
	tokenMu           sync.Mutex
}

// NewMarketplaceInstallHandler creates a new marketplace install page handler.
func NewMarketplaceInstallHandler(marketplaceAPIKey, gatewayToken string, localAPIPort int, db *sql.DB) *MarketplaceInstallHandler {
	return &MarketplaceInstallHandler{
		marketplaceAPIKey: marketplaceAPIKey,
		gatewayToken:      gatewayToken,
		localAPIPort:      localAPIPort,
		db:                db,
		installTokens:     make(map[string]time.Time),
	}
}

// RegisterRoutes registers marketplace install routes on the mux.
func (h *MarketplaceInstallHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /marketplace/install", h.handleConfirmPage)   // Public — just shows confirmation
	mux.HandleFunc("POST /marketplace/install", h.handleDoInstall) // Auth checked via gateway token in form
}

// handleConfirmPage shows the install confirmation page.
func (h *MarketplaceInstallHandler) handleConfirmPage(w http.ResponseWriter, r *http.Request) {
	params := h.extractParams(r)

	if err := h.verifySignature(params); err != nil {
		h.renderError(w, "Invalid Install Link", "This install link is invalid or has been tampered with.", http.StatusForbidden)
		return
	}

	agents := strings.Split(params["agents"], ",")

	// Generate one-time install token (prevents unauthenticated POST)
	installToken := h.generateInstallToken()

	// Check if already installed
	existingTeam := h.findInstalledTeam(params["slug"])
	params["install_token"] = installToken
	h.renderConfirm(w, params, agents, existingTeam)
}

// handleDoInstall downloads the bundle and imports it.
func (h *MarketplaceInstallHandler) handleDoInstall(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderError(w, "Invalid Request", err.Error(), http.StatusBadRequest)
		return
	}

	// Verify one-time install token
	installToken := r.FormValue("install_token")
	if !h.consumeInstallToken(installToken) {
		h.renderError(w, "Session Expired", "This install session has expired. Please start the install again from Hub.", http.StatusForbidden)
		return
	}

	// Verify admin authorization via gateway token
	adminToken := r.FormValue("admin_token")
	if h.gatewayToken != "" && adminToken != h.gatewayToken {
		h.renderError(w, "Unauthorized", "Invalid admin token. Enter your GoClaw gateway token to authorize the install.", http.StatusForbidden)
		return
	}

	params := make(map[string]string)
	// registry_key is NOT accepted from form — injected from server config in verifySignature
	for _, key := range []string{"slug", "title", "agents", "bundle_url", "sig", "ts"} {
		v := r.FormValue(key)
		if decoded, err := url.QueryUnescape(v); err == nil {
			v = decoded
		}
		params[key] = v
	}

	if err := h.verifySignature(params); err != nil {
		h.renderError(w, "Invalid Install Link", "This install link is invalid or has been tampered with.", http.StatusForbidden)
		return
	}

	bundleURL := params["bundle_url"]

	if bundleURL == "" {
		h.renderError(w, "Missing Bundle URL", "No bundle URL provided.", http.StatusBadRequest)
		return
	}

	// S3: Validate bundle URL before requesting
	if err := validateExternalURL(bundleURL); err != nil {
		h.renderError(w, "Invalid Bundle URL", err.Error(), http.StatusBadRequest)
		return
	}

	// Download the bundle from Hub using server-side API key (S2: never from URL/form)
	dlReq, err := http.NewRequest("GET", bundleURL, nil) // B3: check error
	if err != nil {
		h.renderError(w, "Download Failed", fmt.Sprintf("Could not create download request: %v", err), http.StatusInternalServerError)
		return
	}
	dlReq.Header.Set("X-Registry-Key", h.marketplaceAPIKey)

	client := &http.Client{Timeout: 30 * time.Second}
	dlResp, err := client.Do(dlReq)
	if err != nil {
		h.renderError(w, "Download Failed", fmt.Sprintf("Could not download bundle: %v", err), http.StatusBadGateway)
		return
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(dlResp.Body)
		h.renderError(w, "Download Failed", fmt.Sprintf("Hub returned %d: %s", dlResp.StatusCode, string(body)), http.StatusBadGateway)
		return
	}

	// Save to temp file
	tmpFile, err := os.CreateTemp("", "marketplace-*.tar.gz")
	if err != nil {
		h.renderError(w, "Install Failed", "Could not create temp file.", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := io.Copy(tmpFile, io.LimitReader(dlResp.Body, 100*1024*1024)); err != nil {
		tmpFile.Close()
		h.renderError(w, "Install Failed", "Could not save bundle.", http.StatusInternalServerError)
		return
	}
	tmpFile.Close()

	// POST the bundle to local /v1/teams/import
	result, err := h.importLocally(tmpFile.Name())
	if err != nil {
		h.renderError(w, "Import Failed", err.Error(), http.StatusInternalServerError)
		return
	}

	h.renderSuccess(w, params["title"], result)
}

// importLocally POSTs the bundle file to the local teams import endpoint.
func (h *MarketplaceInstallHandler) importLocally(bundlePath string) (string, error) {
	f, err := os.Open(bundlePath)
	if err != nil {
		return "", fmt.Errorf("open bundle: %v", err)
	}
	defer f.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "team.tar.gz")
	if _, err := io.Copy(part, f); err != nil {
		return "", fmt.Errorf("copy bundle to form: %v", err)
	}
	writer.Close()

	importURL := fmt.Sprintf("http://localhost:%d/v1/teams/import", h.localAPIPort)
	req, err := http.NewRequest("POST", importURL, &body) // B3: check error
	if err != nil {
		return "", fmt.Errorf("create import request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if h.gatewayToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.gatewayToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("local import failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("import error (%d): %s", resp.StatusCode, string(respBody))
	}

	return string(respBody), nil
}

// findInstalledTeam checks if a team with this hub_slug is already installed.
func (h *MarketplaceInstallHandler) findInstalledTeam(slug string) string {
	if h.db == nil || slug == "" {
		return ""
	}
	var name string
	if err := h.db.QueryRow(`SELECT name FROM agent_teams WHERE settings->>'hub_slug' = $1 LIMIT 1`, slug).Scan(&name); err != nil && err != sql.ErrNoRows {
		slog.Warn("marketplace: findInstalledTeam query failed", "slug", slug, "error", err)
	}
	return name
}

// generateInstallToken creates a one-time token valid for 10 minutes.
func (h *MarketplaceInstallHandler) generateInstallToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	token := hex.EncodeToString(b)

	h.tokenMu.Lock()
	h.installTokens[token] = time.Now().Add(10 * time.Minute)
	// Cleanup expired tokens
	for k, exp := range h.installTokens {
		if time.Now().After(exp) {
			delete(h.installTokens, k)
		}
	}
	h.tokenMu.Unlock()

	return token
}

// consumeInstallToken validates and consumes a one-time install token.
func (h *MarketplaceInstallHandler) consumeInstallToken(token string) bool {
	if token == "" {
		return false
	}
	h.tokenMu.Lock()
	defer h.tokenMu.Unlock()
	exp, ok := h.installTokens[token]
	if !ok || time.Now().After(exp) {
		return false
	}
	delete(h.installTokens, token) // one-time use
	return true
}

func (h *MarketplaceInstallHandler) extractParams(r *http.Request) map[string]string {
	params := make(map[string]string)
	// registry_key is intentionally NOT extracted from URL — injected in verifySignature (S2)
	for _, key := range []string{"slug", "title", "agents", "bundle_url", "sig", "ts"} {
		v := r.URL.Query().Get(key)
		// URL-decode in case of double encoding (browser preserves encoded chars)
		if decoded, err := url.QueryUnescape(v); err == nil {
			v = decoded
		}
		params[key] = v
	}
	return params
}

func (h *MarketplaceInstallHandler) verifySignature(params map[string]string) error {
	if h.marketplaceAPIKey == "" {
		return fmt.Errorf("marketplace API key not configured")
	}

	sig := params["sig"]
	if sig == "" {
		return fmt.Errorf("missing signature")
	}

	// Check signature expiry
	tsStr := params["ts"]
	if tsStr == "" {
		return fmt.Errorf("missing timestamp")
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp format")
	}
	if time.Since(time.Unix(ts, 0)) > 15*time.Minute {
		return fmt.Errorf("install link expired")
	}

	// S2: Inject registry_key from server config (not from URL/form) before verifying
	params["registry_key"] = h.marketplaceAPIKey

	// Build the signing string: sorted key=value pairs (excluding sig)
	var keys []string
	for k := range params {
		if k != "sig" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	signingString := strings.Join(parts, "&")

	mac := hmac.New(sha256.New, []byte(h.marketplaceAPIKey))
	mac.Write([]byte(signingString))
	expected := hex.EncodeToString(mac.Sum(nil))

	// S5: No key material in logs
	slog.Debug("marketplace-install: signature check", "match", hmac.Equal([]byte(sig), []byte(expected)))

	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

// validateExternalURL validates a bundle URL for SSRF protection (S3).
func validateExternalURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	isLocal := u.Host == "localhost" || strings.HasPrefix(u.Host, "127.0.0.1")
	if u.Scheme != "https" && !isLocal {
		return fmt.Errorf("URL must use HTTPS")
	}
	// Check for private IPs
	if !isLocal {
		host := u.Hostname()
		// Check literal IP
		ip := net.ParseIP(host)
		if ip != nil {
			if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				return fmt.Errorf("URL resolves to private/internal address")
			}
		} else {
			// Resolve hostname and check all IPs
			ips, err := net.LookupHost(host)
			if err == nil {
				for _, ipStr := range ips {
					resolved := net.ParseIP(ipStr)
					if resolved != nil && (resolved.IsPrivate() || resolved.IsLoopback() || resolved.IsLinkLocalUnicast()) {
						return fmt.Errorf("URL hostname resolves to private/internal address")
					}
				}
			}
		}
	}
	return nil
}

// confirmData holds template data for the install confirmation page.
type confirmData struct {
	Title        string
	AgentCount   int
	WarningHTML  template.HTML
	AgentListHTML template.HTML
	Slug         string
	Agents       string
	BundleURL    string
	Sig          string
	Ts           string
	InstallToken string
	ButtonLabel  string
}

var confirmTmpl = template.Must(template.New("confirm").Parse(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Install {{.Title}} — GoClaw</title>
  <style>
    *{margin:0;padding:0;box-sizing:border-box}
    body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;background:#0f1117;color:#e1e1e6;display:flex;justify-content:center;align-items:center;min-height:100vh;padding:20px}
    .card{background:#1a1b23;border:1px solid #2a2b35;border-radius:16px;padding:32px;max-width:480px;width:100%}
    h1{font-size:20px;margin-bottom:8px}
    .subtitle{color:#8b8c96;font-size:14px;margin-bottom:24px}
    .agents{list-style:none;margin-bottom:24px;padding:16px;background:#12131a;border-radius:8px;font-size:14px}
    .badge{display:inline-block;padding:2px 8px;background:#c9834220;color:#c98342;border-radius:4px;font-size:12px;margin-bottom:16px}
    .actions{display:flex;gap:12px}
    .btn{padding:10px 20px;border-radius:8px;border:none;font-size:14px;font-weight:600;cursor:pointer;flex:1;text-align:center;text-decoration:none}
    .btn-primary{background:#c98342;color:#fff}
    .btn-primary:hover{background:#b5753a}
    .btn-secondary{background:#2a2b35;color:#8b8c96}
    .btn-secondary:hover{background:#35363f}
    .from{color:#8b8c96;font-size:12px;margin-top:16px;text-align:center}
  </style>
</head>
<body>
  <div class="card">
    <span class="badge">From GoClaw Hub Marketplace</span>
    <h1>Install &#34;{{.Title}}&#34;?</h1>
    <p class="subtitle">This will add {{.AgentCount}} agent(s) to your GoClaw instance.</p>
    {{.WarningHTML}}
    <ul class="agents">{{.AgentListHTML}}</ul>
    <div class="actions">
      <form method="POST" action="/marketplace/install" style="flex:1;display:flex">
        <input type="hidden" name="slug" value="{{.Slug}}">
        <input type="hidden" name="title" value="{{.Title}}">
        <input type="hidden" name="agents" value="{{.Agents}}">
        <input type="hidden" name="bundle_url" value="{{.BundleURL}}">
        <input type="hidden" name="sig" value="{{.Sig}}">
        <input type="hidden" name="ts" value="{{.Ts}}">
        <input type="hidden" name="install_token" value="{{.InstallToken}}">
        <div style="margin-bottom:12px">
          <label style="display:block;color:#8b8c96;font-size:12px;margin-bottom:4px">Gateway Token (admin authorization)</label>
          <input type="password" name="admin_token" required placeholder="Enter your GoClaw gateway token" style="width:100%;padding:8px 12px;background:#12131a;border:1px solid #2a2b35;border-radius:6px;color:#e1e1e6;font-size:13px;outline:none">
        </div>
        <button type="submit" class="btn btn-primary" style="flex:1">{{.ButtonLabel}}</button>
      </form>
      <a href="javascript:window.close()" class="btn btn-secondary">Cancel</a>
    </div>
    <p class="from">Verify this is from a trusted Hub before installing.</p>
  </div>
</body>
</html>`))

// successData holds template data for the success page.
type successData struct {
	Title  string
	Result string
}

var successTmpl = template.Must(template.New("success").Parse(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Installed! — GoClaw</title>
  <style>
    *{margin:0;padding:0;box-sizing:border-box}
    body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;background:#0f1117;color:#e1e1e6;display:flex;justify-content:center;align-items:center;min-height:100vh;padding:20px}
    .card{background:#1a1b23;border:1px solid #2a2b35;border-radius:16px;padding:32px;max-width:480px;width:100%;text-align:center}
    .check{font-size:48px;margin-bottom:16px}
    h1{font-size:20px;color:#4ade80;margin-bottom:8px}
    .detail{color:#8b8c96;font-size:13px;margin:16px 0;padding:12px;background:#12131a;border-radius:8px;text-align:left;word-break:break-all;font-family:monospace}
    .btn{display:inline-block;padding:10px 24px;border-radius:8px;background:#c98342;color:#fff;text-decoration:none;font-size:14px;font-weight:600;margin-top:16px}
  </style>
</head>
<body>
  <div class="card">
    <div class="check">&#10003;</div>
    <h1>{{.Title}} Installed!</h1>
    <div class="detail">{{.Result}}</div>
    <a href="/" class="btn">Go to Dashboard</a>
  </div>
</body>
</html>`))

// errorData holds template data for the error page.
type errorData struct {
	Title   string
	Message string
}

var errorTmpl = template.Must(template.New("error").Parse(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Error — GoClaw</title>
  <style>
    *{margin:0;padding:0;box-sizing:border-box}
    body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;background:#0f1117;color:#e1e1e6;display:flex;justify-content:center;align-items:center;min-height:100vh;padding:20px}
    .card{background:#1a1b23;border:1px solid #2a2b35;border-radius:16px;padding:32px;max-width:480px;width:100%;text-align:center}
    h1{font-size:20px;color:#f87171;margin-bottom:12px}
    p{color:#8b8c96;font-size:14px}
    .btn{display:inline-block;padding:10px 24px;border-radius:8px;background:#2a2b35;color:#8b8c96;text-decoration:none;font-size:14px;margin-top:20px}
  </style>
</head>
<body>
  <div class="card">
    <h1>{{.Title}}</h1>
    <p>{{.Message}}</p>
    <a href="javascript:window.close()" class="btn">Close</a>
  </div>
</body>
</html>`))

func (h *MarketplaceInstallHandler) renderConfirm(w http.ResponseWriter, params map[string]string, agents []string, existingTeam string) {
	var agentListHTML strings.Builder
	for _, a := range agents {
		a = strings.TrimSpace(a)
		if a != "" {
			agentListHTML.WriteString(`<li style="padding:4px 0">&#8226; `)
			template.HTMLEscape(&agentListHTML, []byte(a))
			agentListHTML.WriteString(`</li>`)
		}
	}

	var warningHTML template.HTML
	buttonLabel := "Install Team"
	if existingTeam != "" {
		warningHTML = template.HTML(`<div style="padding:12px 16px;background:#c9834220;border:1px solid #c9834240;border-radius:8px;margin-bottom:16px;font-size:13px;color:#c98342"><strong>`) +
			template.HTML(template.HTMLEscapeString(existingTeam)) +
			template.HTML(`</strong> is already installed. This will re-import and update it.</div>`)
		buttonLabel = "Update Team"
	}

	data := confirmData{
		Title:         params["title"],
		AgentCount:    len(agents),
		WarningHTML:   warningHTML,
		AgentListHTML: template.HTML(agentListHTML.String()),
		Slug:          params["slug"],
		Agents:        strings.Join(agents, ","),
		BundleURL:     params["bundle_url"],
		Sig:           params["sig"],
		Ts:            params["ts"],
		InstallToken:  params["install_token"],
		ButtonLabel:   buttonLabel,
	}

	var buf bytes.Buffer
	if err := confirmTmpl.Execute(&buf, data); err != nil {
		slog.Error("template render failed", "error", err)
		http.Error(w, "Internal error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(buf.Bytes())
}

func (h *MarketplaceInstallHandler) renderSuccess(w http.ResponseWriter, title, result string) {
	var buf bytes.Buffer
	if err := successTmpl.Execute(&buf, successData{Title: title, Result: result}); err != nil {
		slog.Error("template render failed", "error", err)
		http.Error(w, "Internal error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(buf.Bytes())
}

func (h *MarketplaceInstallHandler) renderError(w http.ResponseWriter, title, message string, status int) {
	var buf bytes.Buffer
	if err := errorTmpl.Execute(&buf, errorData{Title: title, Message: message}); err != nil {
		slog.Error("template render failed", "error", err)
		http.Error(w, "Internal error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}
