package slack

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	slackapi "github.com/slack-go/slack"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// --- Message debounce/batching ---

type debounceEntry struct {
	timer     *time.Timer
	messages  []string
	mu        sync.Mutex
	senderID  string
	channelID string
	media     []string
	metadata  map[string]string
	peerKind  string
}

// debounceMessage batches rapid messages. Returns true if message was debounced.
func (c *Channel) debounceMessage(localKey, senderID, channelID, content string, media []string, metadata map[string]string, peerKind string) bool {
	c.debounceMu.Lock()
	entry, loaded := c.debounceTimers[localKey]
	if !loaded {
		entry = &debounceEntry{
			senderID:  senderID,
			channelID: channelID,
			media:     media,
			metadata:  metadata,
			peerKind:  peerKind,
		}
		c.debounceTimers[localKey] = entry
	}
	c.debounceMu.Unlock()

	entry.mu.Lock()
	defer entry.mu.Unlock()

	entry.messages = append(entry.messages, content)
	if loaded {
		// Only append media for subsequent messages; first message's media is set in constructor.
		entry.media = append(entry.media, media...)
	}
	entry.metadata = metadata // use latest message's metadata

	if !loaded {
		entry.timer = time.AfterFunc(c.debounceDelay, func() {
			c.flushDebounce(localKey)
		})
		return true
	}

	if entry.timer != nil {
		entry.timer.Reset(c.debounceDelay)
	}
	return true
}

func (c *Channel) flushDebounce(localKey string) {
	c.debounceMu.Lock()
	entry, ok := c.debounceTimers[localKey]
	if ok {
		delete(c.debounceTimers, localKey)
	}
	c.debounceMu.Unlock()

	if !ok {
		return
	}

	entry.mu.Lock()
	combined := strings.Join(entry.messages, "\n")
	entry.mu.Unlock()

	c.HandleMessage(entry.senderID, entry.channelID, combined, entry.media, entry.metadata, entry.peerKind)

	if entry.peerKind == "group" {
		c.GroupHistory().Clear(localKey)
	}
}

// --- File download (SSRF-protected) ---

var slackDownloadAllowlist = []string{
	".slack.com",
	".slack-edge.com",
	".slack-files.com",
}

func isAllowedDownloadHost(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, suffix := range slackDownloadAllowlist {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func (c *Channel) downloadFile(name, urlPrivate, urlPrivateDownload string, maxBytes int64) (string, error) {
	downloadURL := urlPrivateDownload
	if downloadURL == "" {
		downloadURL = urlPrivate
	}
	if downloadURL == "" {
		return "", fmt.Errorf("no download URL for file %s", name)
	}

	if !isAllowedDownloadHost(downloadURL) {
		return "", fmt.Errorf("security: download URL hostname not in Slack allowlist: %s", downloadURL)
	}

	ext := filepath.Ext(name)
	if ext == "" {
		ext = ".dat"
	}
	tmpFile, err := os.CreateTemp("", "slack-file-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	client := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			req.Header.Del("Authorization") // strip auth on redirect (CDN presigned URL)
			if req.URL.Scheme != "https" {
				return fmt.Errorf("security: redirect to non-HTTPS URL blocked: %s", req.URL)
			}
			// Only allow redirects to known Slack CDN domains to prevent SSRF.
			host := req.URL.Hostname()
			if !isAllowedSlackHost(host) {
				return fmt.Errorf("security: redirect to untrusted host blocked: %s", host)
			}
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.config.BotToken)

	resp, err := client.Do(req)
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxBytes)); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

// allowedSlackHosts contains trusted Slack CDN domains for redirect validation.
var allowedSlackHosts = []string{
	".slack-edge.com",
	".slack.com",
	"files.slack.com",
}

// isAllowedSlackHost checks if a hostname belongs to a known Slack CDN domain.
func isAllowedSlackHost(host string) bool {
	for _, suffix := range allowedSlackHosts {
		if host == strings.TrimPrefix(suffix, ".") || strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// --- File upload (v2 3-step API) ---

func (c *Channel) uploadFile(channelID, threadTS string, media bus.MediaAttachment) error {
	filePath := media.URL
	fileName := filepath.Base(filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", filePath, err)
	}

	params := slackapi.UploadFileParameters{
		Filename:        fileName,
		FileSize:        len(data),
		Reader:          bytes.NewReader(data),
		Title:           fileName,
		Channel:         channelID,
		ThreadTimestamp: threadTS,
	}

	_, err = c.api.UploadFile(params)
	if err != nil {
		return fmt.Errorf("upload file: %w", err)
	}

	return nil
}
