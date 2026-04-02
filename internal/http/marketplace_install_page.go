package http

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

)

// MarketplaceInstallHandler serves the install confirmation page for Hub marketplace.
type MarketplaceInstallHandler struct {
	marketplaceAPIKey string // GOCLAW_MARKETPLACE_API_KEY — used to verify signatures
	gatewayToken      string // for authenticating local /v1/teams/import calls
	localAPIPort      int
}

// NewMarketplaceInstallHandler creates a new marketplace install page handler.
func NewMarketplaceInstallHandler(marketplaceAPIKey, gatewayToken string, localAPIPort int) *MarketplaceInstallHandler {
	return &MarketplaceInstallHandler{
		marketplaceAPIKey: marketplaceAPIKey,
		gatewayToken:      gatewayToken,
		localAPIPort:      localAPIPort,
	}
}

// RegisterRoutes registers marketplace install routes on the mux.
func (h *MarketplaceInstallHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /marketplace/install", h.handleConfirmPage)  // Public — just shows confirmation
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
	h.renderConfirm(w, params, agents)
}

// handleDoInstall downloads the bundle and imports it.
func (h *MarketplaceInstallHandler) handleDoInstall(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderError(w, "Invalid Request", err.Error(), http.StatusBadRequest)
		return
	}

	params := make(map[string]string)
	for _, key := range []string{"slug", "title", "agents", "bundle_url", "registry_key", "sig"} {
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
	registryKey := params["registry_key"]

	if bundleURL == "" {
		h.renderError(w, "Missing Bundle URL", "No bundle URL provided.", http.StatusBadRequest)
		return
	}

	// Download the bundle from Hub
	dlReq, _ := http.NewRequest("GET", bundleURL, nil)
	dlReq.Header.Set("X-Registry-Key", registryKey)

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

	if _, err := io.Copy(tmpFile, dlResp.Body); err != nil {
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
	io.Copy(part, f)
	writer.Close()

	importURL := fmt.Sprintf("http://localhost:%d/v1/teams/import", h.localAPIPort)
	req, _ := http.NewRequest("POST", importURL, &body)
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

func (h *MarketplaceInstallHandler) extractParams(r *http.Request) map[string]string {
	params := make(map[string]string)
	for _, key := range []string{"slug", "title", "agents", "bundle_url", "registry_key", "sig"} {
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

	fmt.Printf("[marketplace-install] key=%s..., signing=[%s], expected=%s, got=%s, match=%v\n",
		h.marketplaceAPIKey[:8], signingString[:80], expected[:16], sig[:16], hmac.Equal([]byte(sig), []byte(expected)))

	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

func (h *MarketplaceInstallHandler) renderConfirm(w http.ResponseWriter, params map[string]string, agents []string) {
	title := params["title"]
	slug := params["slug"]
	var agentList strings.Builder
	for _, a := range agents {
		a = strings.TrimSpace(a)
		if a != "" {
			agentList.WriteString(fmt.Sprintf(`<li style="padding:4px 0">🤖 %s</li>`, a))
		}
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Install %s — GoClaw</title>
  <style>
    *{margin:0;padding:0;box-sizing:border-box}
    body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;background:#0f1117;color:#e1e1e6;display:flex;justify-content:center;align-items:center;min-height:100vh;padding:20px}
    .card{background:#1a1b23;border:1px solid #2a2b35;border-radius:16px;padding:32px;max-width:480px;width:100%%}
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
    <h1>Install "%s"?</h1>
    <p class="subtitle">This will add %d agent(s) to your GoClaw instance.</p>
    <ul class="agents">%s</ul>
    <div class="actions">
      <form method="POST" action="/marketplace/install" style="flex:1;display:flex">
        <input type="hidden" name="slug" value="%s">
        <input type="hidden" name="title" value="%s">
        <input type="hidden" name="agents" value="%s">
        <input type="hidden" name="bundle_url" value="%s">
        <input type="hidden" name="registry_key" value="%s">
        <input type="hidden" name="sig" value="%s">
        <button type="submit" class="btn btn-primary" style="flex:1">Install Team</button>
      </form>
      <a href="javascript:window.close()" class="btn btn-secondary">Cancel</a>
    </div>
    <p class="from">Verify this is from a trusted Hub before installing.</p>
  </div>
</body>
</html>`,
		title, title, len(agents), agentList.String(),
		slug, title, strings.Join(agents, ","),
		params["bundle_url"],
		params["registry_key"],
		params["sig"])

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}

func (h *MarketplaceInstallHandler) renderSuccess(w http.ResponseWriter, title, result string) {
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Installed! — GoClaw</title>
  <style>
    *{margin:0;padding:0;box-sizing:border-box}
    body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;background:#0f1117;color:#e1e1e6;display:flex;justify-content:center;align-items:center;min-height:100vh;padding:20px}
    .card{background:#1a1b23;border:1px solid #2a2b35;border-radius:16px;padding:32px;max-width:480px;width:100%%;text-align:center}
    .check{font-size:48px;margin-bottom:16px}
    h1{font-size:20px;color:#4ade80;margin-bottom:8px}
    .detail{color:#8b8c96;font-size:13px;margin:16px 0;padding:12px;background:#12131a;border-radius:8px;text-align:left;word-break:break-all;font-family:monospace}
    .btn{display:inline-block;padding:10px 24px;border-radius:8px;background:#c98342;color:#fff;text-decoration:none;font-size:14px;font-weight:600;margin-top:16px}
  </style>
</head>
<body>
  <div class="card">
    <div class="check">✓</div>
    <h1>%s Installed!</h1>
    <div class="detail">%s</div>
    <a href="/" class="btn">Go to Dashboard</a>
  </div>
</body>
</html>`, title, result)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}

func (h *MarketplaceInstallHandler) renderError(w http.ResponseWriter, title, message string, status int) {
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Error — GoClaw</title>
  <style>
    *{margin:0;padding:0;box-sizing:border-box}
    body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;background:#0f1117;color:#e1e1e6;display:flex;justify-content:center;align-items:center;min-height:100vh;padding:20px}
    .card{background:#1a1b23;border:1px solid #2a2b35;border-radius:16px;padding:32px;max-width:480px;width:100%%;text-align:center}
    h1{font-size:20px;color:#f87171;margin-bottom:12px}
    p{color:#8b8c96;font-size:14px}
    .btn{display:inline-block;padding:10px 24px;border-radius:8px;background:#2a2b35;color:#8b8c96;text-decoration:none;font-size:14px;margin-top:20px}
  </style>
</head>
<body>
  <div class="card">
    <h1>%s</h1>
    <p>%s</p>
    <a href="javascript:window.close()" class="btn">Close</a>
  </div>
</body>
</html>`, title, message)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(html))
}

