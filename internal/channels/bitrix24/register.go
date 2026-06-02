package bitrix24

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"time"
)

// maxAvatarBytes caps how much we'll pull from a BotAvatar URL before giving
// up. Bitrix24 rejects PERSONAL_PHOTO payloads past ~300 KB after base64
// encoding; 256 KB pre-encoding keeps us inside that envelope with a little
// slack for JPEG quirks.
const maxAvatarBytes = 256 * 1024

// registerBot ensures the bot identified by cfg.BotCode is registered on the
// portal and returns its bot_id. Three paths, in order of preference:
//
//  1. **State recovery** — if the portal already has a bot_id for this code
//     in `state.registered_bots`, verify it still exists on the portal (an
//     admin may have deleted it through the Bitrix UI). If present we're
//     done; if missing fall through to re-register under the same code.
//
//  2. **Fresh register** — call imbot.register. Success path yields the new
//     bot_id.
//
//  3. **Duplicate-code fallback** — Bitrix returns an error code when the
//     CODE is already used (e.g. another goclaw instance raced us, or
//     state was wiped). Recover by listing bots and picking the one whose
//     CODE matches.
//
// The function is intentionally idempotent so goclaw restarts don't spawn
// duplicate bots — critical because Bitrix charges per bot for larger plans.
func (c *Channel) registerBot(ctx context.Context) (int, error) {
	portal := c.Portal()
	client := c.Client()
	if portal == nil || client == nil {
		return 0, errors.New("bitrix24 register: portal/client not initialised")
	}

	// BITRIX24_FORCE_REREGISTER=1 bypasses the persisted-state cache so the
	// next Start() always pushes the current public_url + bot config back
	// through imbot.register. Use this when public_url changes (tunnel URL
	// rotated, deployed to new host, …) and Bitrix-side event handler URLs
	// must be refreshed without recreating the bot row.
	forceReregister := strings.TrimSpace(os.Getenv("BITRIX24_FORCE_REREGISTER")) == "1"

	// Path 1: recover from persisted state.
	if !forceReregister {
		if id, ok := portal.LookupRegisteredBot(c.cfg.BotCode); ok && id > 0 {
			exists, err := c.verifyBot(ctx, id)
			if err != nil {
				slog.Warn("bitrix24 register: verify cached bot failed — will attempt re-register",
					"portal", c.cfg.Portal, "bot_code", c.cfg.BotCode, "cached_bot_id", id, "err", err)
			} else if exists {
				return id, nil
			}
		}
	} else {
		slog.Info("bitrix24 register: BITRIX24_FORCE_REREGISTER=1 — bypassing cache, will call imbot.register",
			"portal", c.cfg.Portal, "bot_code", c.cfg.BotCode)
	}

	// Path 2: fresh register. Abort up-front when public_url is missing —
	// imbot.register needs absolute EVENT_* URLs and Bitrix will reject a
	// relative path with a confusing error. Better to surface the operator-
	// actionable config problem here.
	if c.eventHandlerURL() == "" {
		return 0, fmt.Errorf("bitrix24 register: public_url not set on channel_instance config (required for imbot.register)")
	}
	params := c.registerParams(ctx)

	resp, err := client.Call(ctx, "imbot.register", params)
	if err == nil {
		id := intFromResult(resp)
		if id <= 0 {
			return 0, fmt.Errorf("bitrix24 register: bot_id missing from response")
		}
		return id, nil
	}

	// Path 3: duplicate CODE fallback. Bitrix doesn't publish a stable code
	// here — the string surfaces in error.description — so we substring-match
	// on the known fragments.
	if isDuplicateCodeError(err) {
		id, lookupErr := c.findBotIDByCode(ctx, c.cfg.BotCode)
		if lookupErr != nil {
			return 0, fmt.Errorf("bot code duplicate but imbot.list lookup failed: %w", lookupErr)
		}
		if id <= 0 {
			return 0, fmt.Errorf("bot code duplicate but no bot with CODE=%q found on portal", c.cfg.BotCode)
		}
		return id, nil
	}

	return 0, fmt.Errorf("bitrix24 imbot.register: %w", err)
}

// unregisterBot calls imbot.unregister to remove the bot from the Bitrix24
// portal. Returns nil when the bot was successfully unregistered OR when it
// no longer exists on the portal — admin may have manually deleted via
// Bitrix UI between channel Start and Destroy, in which case there's nothing
// to do and we treat the absence as success (idempotent).
//
// Caller is responsible for clearing local state (Portal.ForgetRegisteredBot,
// Router.UnregisterBot) — this function only owns the network call.
func (c *Channel) unregisterBot(ctx context.Context, botID int) error {
	if botID <= 0 {
		return nil
	}
	client := c.Client()
	if client == nil {
		return errors.New("bitrix24 unregister: client not initialised")
	}
	_, err := client.Call(ctx, "imbot.unregister", map[string]any{"BOT_ID": botID})
	if err == nil {
		return nil
	}
	if isBotNotFoundError(err) {
		slog.Info("bitrix24 unregister: bot already absent on portal — treating as success",
			"portal", c.cfg.Portal, "bot_id", botID)
		return nil
	}
	return fmt.Errorf("bitrix24 imbot.unregister: %w", err)
}

// isBotNotFoundError pattern-matches the Bitrix24 rejection when BOT_ID does
// not exist on the portal. Bitrix returns different codes across portal
// versions, so we check both the structured code field and a few common
// description substrings.
func isBotNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.Code == "ERROR_BOT_NOT_FOUND" || apiErr.Code == "BOT_NOT_FOUND" {
			return true
		}
		if containsFold(apiErr.Description, "bot not found") ||
			containsFold(apiErr.Description, "not registered") ||
			containsFold(apiErr.Description, "no bot with") {
			return true
		}
	}
	return false
}

// registerParams builds the imbot.register body. Avatar fetching is
// best-effort — a slow or broken source shouldn't block startup.
func (c *Channel) registerParams(ctx context.Context) map[string]any {
	props := map[string]any{
		"NAME":          c.cfg.BotName,
		"COLOR":         "AZURE",
		"WORK_POSITION": "AI Assistant",
	}
	if c.cfg.BotAvatar != "" {
		if b64, err := c.fetchAvatarBase64(ctx, c.cfg.BotAvatar); err == nil && b64 != "" {
			props["PERSONAL_PHOTO"] = b64
		} else if err != nil {
			slog.Warn("bitrix24 avatar fetch failed — registering without PERSONAL_PHOTO",
				"portal", c.cfg.Portal, "url", c.cfg.BotAvatar, "err", err)
		}
	}

	handlerURL := c.eventHandlerURL()
	// BotType is validated at factory load to be "B" or "O"; applyConfigDefaults
	// fills "" → "B". Pass through verbatim — Bitrix rejects unknown values.
	return map[string]any{
		"CODE":                  c.cfg.BotCode,
		"TYPE":                  c.cfg.BotType,
		"EVENT_MESSAGE_ADD":     handlerURL,
		"EVENT_WELCOME_MESSAGE": handlerURL,
		"EVENT_BOT_DELETE":      handlerURL,
		"PROPERTIES":            props,
	}
}

// eventHandlerURL returns the absolute URL Bitrix24 should call for events,
// or an empty string when no source has a public URL. Priority:
//
//  1. portal.PublicURL() — captured by the install handler from the request
//     Bitrix24 itself sent; self-verifying because the URL has been proven
//     reachable. This is the preferred source.
//  2. c.cfg.PublicURL — legacy per-instance config (deprecated). Used only
//     when (1) is empty, e.g. portal was installed on a goclaw release that
//     predated the capture feature. A deprecation warning is logged so an
//     operator can plan a reinstall.
//
// The empty case is caller-visible so registerBot can fail with a Config
// error instead of burning an API call that Bitrix is guaranteed to reject
// ("URL invalid").
func (c *Channel) eventHandlerURL() string {
	if c.portal != nil {
		if v := strings.TrimRight(strings.TrimSpace(c.portal.PublicURL()), "/"); v != "" {
			return v + eventsPath
		}
	}
	base := strings.TrimRight(strings.TrimSpace(c.cfg.PublicURL), "/")
	if base == "" {
		return ""
	}
	slog.Warn("bitrix24: using legacy config.public_url — reinstall the portal to capture the URL automatically",
		"portal", c.cfg.Portal, "bot_code", c.cfg.BotCode)
	return base + eventsPath
}

// verifyBot confirms a bot_id still exists on the portal. Used during
// startup recovery so a bot_id cached in state but manually deleted on the
// Bitrix side doesn't leave us silently broken.
//
// imbot.list returns an array of bot rows; we just scan for the id.
// Any transport error propagates — the caller decides whether to re-register
// or bail out.
func (c *Channel) verifyBot(ctx context.Context, botID int) (bool, error) {
	client := c.Client()
	if client == nil {
		return false, errors.New("bitrix24 verify: client not initialised")
	}

	resp, err := client.Call(ctx, "imbot.bot.list", nil)
	if err != nil {
		// Older portals expose a different endpoint name; try the alternate.
		alt, altErr := client.Call(ctx, "imbot.list", nil)
		if altErr != nil {
			// Surface BOTH errors so operators can see whether this is a
			// portal-side outage (both fail the same way) vs. an endpoint
			// naming issue (only one side fails).
			return false, fmt.Errorf("bitrix24 verify: %w",
				errors.Join(err, altErr))
		}
		resp = alt
	}

	return responseContainsBotID(resp, botID), nil
}

// findBotIDByCode scans the portal for a bot whose CODE equals the given
// code. Used in the duplicate-code fallback path of registerBot. Returns
// 0 (not an error) when the code is genuinely absent.
func (c *Channel) findBotIDByCode(ctx context.Context, code string) (int, error) {
	if code == "" {
		return 0, errors.New("findBotIDByCode: empty code")
	}
	client := c.Client()
	if client == nil {
		return 0, errors.New("bitrix24 find: client not initialised")
	}

	resp, err := client.Call(ctx, "imbot.bot.list", nil)
	if err != nil {
		alt, altErr := client.Call(ctx, "imbot.list", nil)
		if altErr != nil {
			return 0, fmt.Errorf("bitrix24 find-by-code: %w",
				errors.Join(err, altErr))
		}
		resp = alt
	}

	return findBotIDByCodeInResponse(resp, code), nil
}

// fetchAvatarBase64 downloads an image and returns it base64-encoded.
// Bounded by maxAvatarBytes; content-type sniffing is left to Bitrix.
//
// Bitrix expects raw base64 (no data: prefix) for PERSONAL_PHOTO.
//
// The scheme is restricted to http/https. Although cfg.BotAvatar comes from
// operator config (not from a request body), defense-in-depth rejects
// file:// and custom schemes so a mistyped config can't read a local file
// or hit an unexpected handler.
func (c *Channel) fetchAvatarBase64(ctx context.Context, rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", nil
	}
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("avatar fetch: parse url: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		// allowed
	default:
		return "", fmt.Errorf("avatar fetch: unsupported URL scheme %q (only http/https allowed)", parsed.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	// Default http.Client follows up to 10 redirects without re-validating the
	// scheme of the next hop. A CDN-style redirect could land on file:// or
	// (more realistically) on an http:// URL that we explicitly forbade above.
	// Re-check every hop so the initial-URL restriction can't be bypassed.
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			switch strings.ToLower(r.URL.Scheme) {
			case "http", "https":
				return nil
			default:
				return fmt.Errorf("avatar fetch: redirect to unsupported scheme %q", r.URL.Scheme)
			}
		},
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("avatar fetch: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAvatarBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) == 0 {
		return "", errors.New("avatar fetch: empty body")
	}
	if len(body) > maxAvatarBytes {
		return "", fmt.Errorf("avatar fetch: size exceeds %d bytes", maxAvatarBytes)
	}
	return base64.StdEncoding.EncodeToString(body), nil
}

// intFromResult pulls an int out of a RawResult envelope. imbot.register
// returns either `{"result": 42}` or (more rarely) `{"result": {"BOT_ID": 42}}`
// depending on portal version — handle both without importing a JSON schema.
func intFromResult(r *RawResult) int {
	if r == nil || len(r.Result) == 0 {
		return 0
	}

	// Try plain integer first.
	var n json.Number
	if err := json.Unmarshal(r.Result, &n); err == nil {
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	}

	// Fallback: object with BOT_ID.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(r.Result, &obj); err == nil {
		for _, k := range []string{"BOT_ID", "bot_id", "ID", "id"} {
			if raw, ok := obj[k]; ok {
				var v json.Number
				if err := json.Unmarshal(raw, &v); err == nil {
					if i, err := v.Int64(); err == nil {
						return int(i)
					}
				}
			}
		}
	}
	return 0
}

// responseContainsBotID scans imbot.list output for the given bot_id.
// The shape is either an array (legacy) or an object keyed by bot id
// (newer portals) — handle both transparently.
func responseContainsBotID(r *RawResult, botID int) bool {
	if r == nil || len(r.Result) == 0 || botID <= 0 {
		return false
	}

	// Array form.
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(r.Result, &arr); err == nil {
		for _, row := range arr {
			if rowHasBotID(row, botID) {
				return true
			}
		}
		return false
	}

	// Map form — keys may be numeric strings or {BOT_ID: ..., CODE: ...}.
	var obj map[string]map[string]json.RawMessage
	if err := json.Unmarshal(r.Result, &obj); err == nil {
		for key, row := range obj {
			if key == fmt.Sprintf("%d", botID) {
				return true
			}
			if rowHasBotID(row, botID) {
				return true
			}
		}
	}
	return false
}

// findBotIDByCodeInResponse scans imbot.list output for a CODE match and
// returns the associated bot_id. Returns 0 if the code isn't found.
func findBotIDByCodeInResponse(r *RawResult, code string) int {
	if r == nil || len(r.Result) == 0 || code == "" {
		return 0
	}

	// Array form.
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(r.Result, &arr); err == nil {
		for _, row := range arr {
			if rowCodeMatches(row, code) {
				if id := extractBotID(row); id > 0 {
					return id
				}
			}
		}
		return 0
	}

	// Map form.
	var obj map[string]map[string]json.RawMessage
	if err := json.Unmarshal(r.Result, &obj); err == nil {
		for key, row := range obj {
			if rowCodeMatches(row, code) {
				if id := extractBotID(row); id > 0 {
					return id
				}
				// Fall back to the object key if it's numeric (older portals).
				if id := atoiSafe(key); id > 0 {
					return id
				}
			}
		}
	}
	return 0
}

func rowHasBotID(row map[string]json.RawMessage, botID int) bool {
	return extractBotID(row) == botID
}

func rowCodeMatches(row map[string]json.RawMessage, code string) bool {
	for _, k := range []string{"CODE", "code"} {
		if raw, ok := row[k]; ok {
			var s string
			if err := json.Unmarshal(raw, &s); err == nil && s == code {
				return true
			}
		}
	}
	return false
}

func extractBotID(row map[string]json.RawMessage) int {
	for _, k := range []string{"BOT_ID", "bot_id", "ID", "id"} {
		if raw, ok := row[k]; ok {
			var n json.Number
			if err := json.Unmarshal(raw, &n); err == nil {
				if i, err := n.Int64(); err == nil {
					return int(i)
				}
			}
			// Some portals quote ids.
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				if i := atoiSafe(s); i > 0 {
					return i
				}
			}
		}
	}
	return 0
}

func atoiSafe(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
		if n < 0 { // overflow guard
			return 0
		}
	}
	return n
}

// isDuplicateCodeError pattern-matches the Bitrix24 rejection payload for a
// CODE that already exists on the portal. The error surface isn't fully
// documented — these substrings cover the cases observed on 2024–2025 portals.
func isDuplicateCodeError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.Code == "ERROR_ARGUMENT" || apiErr.Code == "ERROR_REGISTER_BOT" {
			return containsFold(apiErr.Description, "code") &&
				(containsFold(apiErr.Description, "exist") || containsFold(apiErr.Description, "duplicate"))
		}
		if containsFold(apiErr.Description, "already exist") ||
			containsFold(apiErr.Description, "duplicate") {
			return true
		}
	}
	msg := err.Error()
	return containsFold(msg, "already exist") || containsFold(msg, "code exist") || containsFold(msg, "duplicate code")
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
