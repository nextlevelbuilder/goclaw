package http

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
)

const (
	defaultGatewayUpgradeScript = "/usr/local/bin/goclaw-upgrade-release"
	defaultGatewayUpgradeStatus = "/var/lib/goclaw/update-jobs/current.json"
	gatewayUpgradeTokenHeader   = "X-GoClaw-Upgrade-Token"
	gatewayUpgradeRunningMaxAge = 30 * time.Minute
)

var gatewayUpgradeTagRE = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-(beta|rc)\.[0-9]+)?$`)

type gatewayUpgradeRunner interface {
	Start(tag string) error
}

type gatewayUpgradeCommandRunner struct {
	scriptPath string
}

func (r gatewayUpgradeCommandRunner) Start(tag string) error {
	cmd := exec.Command("sudo", "-n", r.scriptPath, tag)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			slog.Warn("gateway upgrade command exited with error", "error", err)
		}
	}()
	return nil
}

// GatewayUpgradeHandler triggers the host-local GoClaw release upgrade script.
// It never accepts arbitrary commands or URLs.
type GatewayUpgradeHandler struct {
	ScriptPath   string
	StatusPath   string
	TriggerToken string
	Runner       gatewayUpgradeRunner
	mu           sync.Mutex
}

func NewGatewayUpgradeHandlerFromEnv() *GatewayUpgradeHandler {
	scriptPath := strings.TrimSpace(os.Getenv("GOCLAW_UPGRADE_SCRIPT"))
	if scriptPath == "" {
		scriptPath = defaultGatewayUpgradeScript
	}
	statusPath := strings.TrimSpace(os.Getenv("GOCLAW_UPGRADE_STATUS_PATH"))
	if statusPath == "" {
		statusPath = defaultGatewayUpgradeStatus
	}
	h := &GatewayUpgradeHandler{
		ScriptPath:   scriptPath,
		StatusPath:   statusPath,
		TriggerToken: os.Getenv("GOCLAW_UPGRADE_TRIGGER_TOKEN"),
	}
	h.Runner = gatewayUpgradeCommandRunner{scriptPath: h.ScriptPath}
	return h
}

func (h *GatewayUpgradeHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/system/gateway/upgrade/status", requireAuth(permissions.RoleAdmin, h.handleStatus))
	mux.HandleFunc("POST /v1/system/gateway/upgrade", requireAuth(permissions.RoleAdmin, h.handleStart))
}

func (h *GatewayUpgradeHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMasterScope(w, r) {
		return
	}
	if !h.requireTriggerToken(w, r) {
		return
	}

	status, err := h.readStatus()
	if err != nil {
		slog.Error("gateway upgrade status read failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read gateway upgrade status"})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *GatewayUpgradeHandler) handleStart(w http.ResponseWriter, r *http.Request) {
	if !requireMasterScope(w, r) {
		return
	}
	if !enforcePackagesWriteLimit(w, r, "/v1/system/gateway/upgrade") {
		return
	}
	if !h.requireTriggerToken(w, r) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var req struct {
		Tag string `json:"tag"`
	}
	if !bindJSON(w, r, extractLocale(r), &req) {
		return
	}
	tag := strings.TrimSpace(req.Tag)
	if !validGatewayUpgradeTag(tag) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tag must be latest or vMAJOR.MINOR.PATCH[-beta.N|-rc.N]"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	status, err := h.readStatus()
	if err != nil {
		slog.Error("gateway upgrade status read failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read gateway upgrade status"})
		return
	}
	if gatewayUpgradeStatusRunning(status, time.Now().UTC()) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "gateway upgrade already running"})
		return
	}

	runner := h.Runner
	if runner == nil {
		runner = gatewayUpgradeCommandRunner{scriptPath: h.ScriptPath}
	}
	if err := h.writeRunningStatus(tag); err != nil {
		slog.Error("gateway upgrade status write failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write gateway upgrade status"})
		return
	}
	if err := runner.Start(tag); err != nil {
		slog.Error("gateway upgrade start failed", "error", err)
		_ = h.writeFailedStatus(tag, "failed to start gateway upgrade")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to start gateway upgrade"})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":       true,
		"accepted": true,
		"tag":      tag,
	})
}

func (h *GatewayUpgradeHandler) requireTriggerToken(w http.ResponseWriter, r *http.Request) bool {
	if h.TriggerToken == "" {
		slog.Warn("security.gateway_upgrade_token_unconfigured", "path", r.URL.Path)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "gateway upgrade trigger token is not configured"})
		return false
	}
	provided := r.Header.Get(gatewayUpgradeTokenHeader)
	if subtle.ConstantTimeCompare([]byte(provided), []byte(h.TriggerToken)) == 1 {
		return true
	}
	slog.Warn("security.gateway_upgrade_token_denied", "path", r.URL.Path)
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "upgrade trigger token required"})
	return false
}

func (h *GatewayUpgradeHandler) readStatus() (map[string]any, error) {
	path := h.statusPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{"state": "idle"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read upgrade status: %w", err)
	}
	if len(data) > 64*1024 {
		return nil, fmt.Errorf("upgrade status too large")
	}
	var status map[string]any
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("decode upgrade status: %w", err)
	}
	if status == nil {
		return map[string]any{"state": "idle"}, nil
	}
	return status, nil
}

func gatewayUpgradeStatusRunning(status map[string]any, now time.Time) bool {
	if status["state"] != "running" {
		return false
	}
	startedRaw, ok := status["startedAt"].(string)
	if !ok || strings.TrimSpace(startedRaw) == "" {
		return true
	}
	startedAt, err := time.Parse(time.RFC3339, startedRaw)
	if err != nil {
		return true
	}
	age := now.Sub(startedAt)
	if age < 0 || age <= gatewayUpgradeRunningMaxAge {
		return true
	}
	slog.Warn("gateway upgrade stale running status ignored", "started_at", startedRaw, "age", age.String())
	return false
}

func (h *GatewayUpgradeHandler) writeRunningStatus(tag string) error {
	return h.writeStatus(map[string]any{
		"jobId":        time.Now().UTC().Format("20060102T150405Z") + "-" + tag,
		"state":        "running",
		"requestedTag": tag,
		"resolvedTag":  "",
		"startedAt":    time.Now().UTC().Format(time.RFC3339),
		"finishedAt":   nil,
		"error":        nil,
	})
}

func (h *GatewayUpgradeHandler) writeFailedStatus(tag, reason string) error {
	return h.writeStatus(map[string]any{
		"jobId":        time.Now().UTC().Format("20060102T150405Z") + "-" + tag,
		"state":        "failed",
		"requestedTag": tag,
		"resolvedTag":  "",
		"startedAt":    time.Now().UTC().Format(time.RFC3339),
		"finishedAt":   time.Now().UTC().Format(time.RFC3339),
		"error":        reason,
	})
}

func (h *GatewayUpgradeHandler) writeStatus(status map[string]any) error {
	path := h.statusPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create upgrade status dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".current-*.json")
	if err != nil {
		return fmt.Errorf("create upgrade status tmp: %w", err)
	}
	tmpName := tmp.Name()
	encErr := json.NewEncoder(tmp).Encode(status)
	closeErr := tmp.Close()
	if encErr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("encode upgrade status: %w", encErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close upgrade status tmp: %w", closeErr)
	}
	if err := os.Chmod(tmpName, 0o640); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod upgrade status tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace upgrade status: %w", err)
	}
	return nil
}

func (h *GatewayUpgradeHandler) statusPath() string {
	if h.StatusPath == "" {
		return defaultGatewayUpgradeStatus
	}
	return filepath.Clean(h.StatusPath)
}

func validGatewayUpgradeTag(tag string) bool {
	return tag == "latest" || gatewayUpgradeTagRE.MatchString(tag)
}
