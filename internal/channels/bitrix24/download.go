package bitrix24

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/security"
)

const (
	// maxInboundFiles caps how many attachments we download per message so a
	// single event can't trigger an unbounded fan-out of REST + HTTP calls.
	maxInboundFiles = 10
	// inboundDownloadTimeout bounds a single file download. The REST client's
	// 15s timeout is tuned for JSON calls; media needs a longer, dedicated one.
	inboundDownloadTimeout = 5 * time.Minute
	// maxInboundRedirects caps redirect hops on a download (each re-validated by
	// CheckRedirect). Bitrix download links rarely redirect; a low cap limits a
	// redirect-loop / SSRF-probe before the per-hop host check even runs.
	maxInboundRedirects = 5
)

// fileDownloadResult mirrors the imbot.v2.File.download result envelope.
// The method returns a one-time, pre-authorized download link.
type fileDownloadResult struct {
	DownloadURL string `json:"downloadUrl"`
}

// downloadEventFiles resolves and downloads every attachment on a Bitrix24
// message event, returning bus.MediaFile values ready to publish to the bus.
//
// Each file is fetched via imbot.v2.File.download (returns a one-time download
// URL that already embeds an auth token) then streamed to a temp file under the
// configured size cap. The MIME type is preserved so the agent pipeline routes
// the file to the right reader (image / document / audio / video).
//
// Best-effort: a single file that fails to resolve or download is logged and
// skipped — the other files (and the message text) still reach the agent.
func (c *Channel) downloadEventFiles(ctx context.Context, botID int, files []EventFile) []bus.MediaFile {
	if len(files) == 0 || botID <= 0 {
		return nil
	}
	client := c.Client()
	if client == nil {
		slog.Warn("bitrix24 media: no REST client, skipping inbound files", "portal", c.cfg.Portal)
		return nil
	}

	maxBytes := int64(c.cfg.MediaMaxMB) * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = 20 * 1024 * 1024 // applyConfigDefaults should set this; belt-and-braces.
	}

	if len(files) > maxInboundFiles {
		slog.Warn("bitrix24 media: too many attachments, capping",
			"total", len(files), "cap", maxInboundFiles, "portal", c.cfg.Portal)
		files = files[:maxInboundFiles]
	}

	// SSRF guard. fetchOneFile validates the *initial* downloadUrl host against
	// the portal domain, but that URL may legitimately 3xx to a public CDN, and a
	// hostile/garbled 3xx could point at an internal service (cloud metadata
	// 169.254.169.254, internal Redis, etc.). NewRedirectFollowingSafeClient
	// re-validates the RESOLVED destination IP of every hop at dial time, so a
	// redirect whose host resolves into a private/loopback/link-local range is
	// refused — even via DNS rebinding — while legitimate public redirects still
	// succeed. Checking the dial IP (not the hostname string) is the fix for the
	// same class of bug as portal-domain validation.
	hc := security.NewRedirectFollowingSafeClient(inboundDownloadTimeout, maxInboundRedirects)
	var out []bus.MediaFile
	for _, f := range files {
		// Pre-flight size check — skip oversized files without a download attempt.
		if f.Size > 0 && f.Size > maxBytes {
			slog.Warn("bitrix24 media: file exceeds size cap, skipping",
				"file_id", f.ID, "size", f.Size, "max", maxBytes)
			continue
		}
		mf, err := c.fetchOneFile(ctx, hc, client, botID, f, maxBytes)
		if err != nil {
			slog.Warn("bitrix24 media: download failed, skipping file",
				"file_id", f.ID, "name", f.Name, "err", err)
			continue
		}
		out = append(out, mf)
	}
	return out
}

// fetchOneFile resolves one attachment's download URL via imbot.v2.File.download
// and streams it to a temp file, returning a populated bus.MediaFile.
func (c *Channel) fetchOneFile(ctx context.Context, hc *http.Client, client *Client, botID int, f EventFile, maxBytes int64) (bus.MediaFile, error) {
	// fileId is documented as integer but Bitrix accepts the numeric string;
	// EventFile.ID is already a string, so pass it through verbatim.
	rr, err := client.Call(ctx, "imbot.v2.File.download", map[string]any{
		"botId":  botID,
		"fileId": f.ID,
	})
	if err != nil {
		return bus.MediaFile{}, fmt.Errorf("imbot.v2.File.download: %w", err)
	}
	var res fileDownloadResult
	if err := json.Unmarshal(rr.Result, &res); err != nil {
		return bus.MediaFile{}, fmt.Errorf("decode download result: %w", err)
	}
	if res.DownloadURL == "" {
		return bus.MediaFile{}, fmt.Errorf("empty downloadUrl")
	}

	// Defense in depth: the link must point at the portal domain. The REST API
	// issued it, so this normally holds — the check guards against a malformed
	// or hostile response redirecting our fetch elsewhere (SSRF). Fail CLOSED:
	// reject when either side is empty or they differ, so a blank/unparseable
	// host (exactly the malformed case this exists to catch) is denied.
	dom := client.Domain()
	if host := bxURLHost(res.DownloadURL); dom == "" || host == "" || host != dom {
		return bus.MediaFile{}, fmt.Errorf("downloadUrl host %q != portal %q", host, dom)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, res.DownloadURL, nil)
	if err != nil {
		return bus.MediaFile{}, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return bus.MediaFile{}, fmt.Errorf("GET downloadUrl: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return bus.MediaFile{}, fmt.Errorf("download status %d", resp.StatusCode)
	}

	mime := resolveMime(f, resp.Header.Get("Content-Type"))

	tmp, err := os.CreateTemp("", "goclaw_bitrix_*"+filepath.Ext(f.Name))
	if err != nil {
		return bus.MediaFile{}, fmt.Errorf("create temp: %w", err)
	}
	// +1 so we can detect "wrote exactly the cap then more was available".
	written, err := io.Copy(tmp, io.LimitReader(resp.Body, maxBytes+1))
	tmp.Close()
	if err != nil {
		os.Remove(tmp.Name())
		return bus.MediaFile{}, fmt.Errorf("save file: %w", err)
	}
	if written > maxBytes {
		os.Remove(tmp.Name())
		return bus.MediaFile{}, fmt.Errorf("file exceeds %d bytes", maxBytes)
	}

	slog.Info("bitrix24 media: downloaded inbound file",
		"file_id", f.ID, "name", f.Name, "bytes", written, "mime", mime)
	return bus.MediaFile{Path: tmp.Name(), MimeType: mime, Filename: f.Name}, nil
}

// resolveMime picks the best MIME type for an attachment, preferring the value
// Bitrix sent in the event, then the download response's Content-Type, then a
// filename-based guess. Falls back to octet-stream so persistMedia still stores
// the file (the agent treats unknown types as generic documents).
func resolveMime(f EventFile, respCT string) string {
	if f.Mime != "" {
		return f.Mime
	}
	if ct := strings.TrimSpace(strings.SplitN(respCT, ";", 2)[0]); ct != "" && ct != "application/octet-stream" {
		return ct
	}
	if detected := media.DetectMIMEType(f.Name); detected != "" {
		return detected
	}
	return "application/octet-stream"
}

// bxURLHost extracts the bare hostname (no port) from a URL, returning "" on
// parse failure. Hostname() — not Host — so a legitimate ":443" doesn't cause a
// false mismatch against the portal domain.
func bxURLHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
