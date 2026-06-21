package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const gatewayOperatorBinaryName = "goclaw"

var (
	errGatewayOperatorSecureCLIUnavailable = errors.New("gateway operator securecli unavailable")
	errGatewayOperatorTokenMissing         = errors.New("gateway operator token missing")
	errGatewayOperatorBinaryMissing        = errors.New("gateway operator goclaw binary missing")
	errGatewayOperatorExistingReview       = errors.New("gateway operator existing credential requires review")
	errGatewayOperatorRegisterFailed       = errors.New("gateway operator register failed")
	errGatewayOperatorCredentialFailed     = errors.New("gateway operator credential failed")
)

type gatewayOperatorBootstrapResult struct {
	Status   string    `json:"status"`
	BinaryID uuid.UUID `json:"binary_id,omitempty"`
	GrantID  uuid.UUID `json:"grant_id,omitempty"`
	Warning  string    `json:"warning,omitempty"`
}

func (h *AgentsHandler) SetGatewayOperatorBootstrap(secureCLI store.SecureCLIStore, grants store.SecureCLIAgentGrantStore, agentCreds store.SecureCLIAgentCredentialStore, gatewayAddr string) {
	h.secureCLI = secureCLI
	h.secureCLIGrants = grants
	h.secureCLIAgentCreds = agentCreds
	h.gatewayAddr = gatewayAddr
	if h.findGatewayOperatorBinary == nil {
		h.findGatewayOperatorBinary = defaultFindGatewayOperatorBinary
	}
}

func (h *AgentsHandler) bootstrapGatewayOperatorForCreatedAgent(ctx context.Context, agentID uuid.UUID, locale string) *gatewayOperatorBootstrapResult {
	if h.secureCLI == nil || h.secureCLIGrants == nil || h.secureCLIAgentCreds == nil {
		return &gatewayOperatorBootstrapResult{Status: "warning", Warning: i18n.T(locale, i18n.MsgGatewayOperatorSecureCLIUnavailable)}
	}
	first, err := h.isDeterministicFirstAgent(ctx, agentID)
	if err != nil {
		slog.Warn("gateway_operator.bootstrap.first_agent_check_failed", "agent_id", agentID, "error", err)
		return &gatewayOperatorBootstrapResult{Status: "warning", Warning: i18n.T(locale, i18n.MsgGatewayOperatorEligibilityFailed)}
	}
	if !first {
		return &gatewayOperatorBootstrapResult{Status: "skipped", Warning: i18n.T(locale, i18n.MsgGatewayOperatorNotFirstAgent)}
	}

	result, err := h.bootstrapGatewayOperatorAccess(ctx, agentID)
	if err != nil {
		slog.Warn("gateway_operator.bootstrap.failed", "agent_id", agentID, "error", err)
		return &gatewayOperatorBootstrapResult{Status: "warning", Warning: gatewayOperatorWarning(locale, err)}
	}
	result.Status = "granted"
	return result
}

func (h *AgentsHandler) isDeterministicFirstAgent(ctx context.Context, agentID uuid.UUID) (bool, error) {
	if h.agents == nil || agentID == uuid.Nil {
		return false, nil
	}
	agents, err := h.agents.List(ctx, "")
	if err != nil {
		return false, err
	}
	if len(agents) == 0 {
		return false, nil
	}
	firstID := deterministicFirstAgentID(agents)
	return firstID == agentID, nil
}

func deterministicFirstAgentID(agents []store.AgentData) uuid.UUID {
	sort.SliceStable(agents, func(i, j int) bool {
		if !agents[i].CreatedAt.Equal(agents[j].CreatedAt) {
			return agents[i].CreatedAt.Before(agents[j].CreatedAt)
		}
		return agents[i].ID.String() < agents[j].ID.String()
	})
	if len(agents) == 0 {
		return uuid.Nil
	}
	return agents[0].ID
}

func (h *AgentsHandler) bootstrapGatewayOperatorAccess(ctx context.Context, agentID uuid.UUID) (*gatewayOperatorBootstrapResult, error) {
	if strings.TrimSpace(pkgGatewayToken) == "" {
		return nil, errGatewayOperatorTokenMissing
	}
	if h.secureCLI == nil || h.secureCLIGrants == nil || h.secureCLIAgentCreds == nil {
		return nil, errGatewayOperatorSecureCLIUnavailable
	}
	findBinary := h.findGatewayOperatorBinary
	if findBinary == nil {
		findBinary = defaultFindGatewayOperatorBinary
	}
	binaryPath, err := findBinary()
	if err != nil {
		return nil, errGatewayOperatorBinaryMissing
	}

	binary, err := h.ensureGatewayOperatorBinary(ctx, binaryPath)
	if err != nil {
		return nil, err
	}
	grant, err := h.ensureGatewayOperatorGrant(ctx, binary.ID, agentID)
	if err != nil {
		return nil, err
	}
	if err := h.setGatewayOperatorAgentCredential(ctx, binary.ID, agentID); err != nil {
		return nil, err
	}

	h.emitCacheInvalidate("secure_cli", binary.ID.String())
	return &gatewayOperatorBootstrapResult{BinaryID: binary.ID, GrantID: grant.ID}, nil
}

func (h *AgentsHandler) ensureGatewayOperatorBinary(ctx context.Context, binaryPath string) (*store.SecureCLIBinary, error) {
	existing, err := h.findGatewayOperatorBinaryConfig(ctx)
	if err != nil {
		return nil, errGatewayOperatorCredentialFailed
	}
	if existing != nil {
		return h.applyGatewayOperatorBinaryPolicy(ctx, existing, binaryPath)
	}

	binary := &store.SecureCLIBinary{
		BinaryName:     gatewayOperatorBinaryName,
		BinaryPath:     &binaryPath,
		Description:    gatewayOperatorDescription(),
		DenyArgs:       gatewayOperatorDenyArgs(),
		DenyVerbose:    gatewayOperatorDenyVerbose(),
		TimeoutSeconds: 30,
		Tips:           gatewayOperatorTips(),
		IsGlobal:       false,
		Enabled:        true,
		CreatedBy:      gatewayOperatorActor(ctx),
	}
	if err := h.secureCLI.Create(ctx, binary); err != nil {
		if found, findErr := h.findGatewayOperatorBinaryConfig(ctx); findErr == nil && found != nil {
			if updated, updateErr := h.applyGatewayOperatorBinaryPolicy(ctx, found, binaryPath); updateErr == nil {
				return updated, nil
			}
		}
		return nil, errGatewayOperatorRegisterFailed
	}
	return binary, nil
}

func (h *AgentsHandler) applyGatewayOperatorBinaryPolicy(ctx context.Context, binary *store.SecureCLIBinary, binaryPath string) (*store.SecureCLIBinary, error) {
	if err := h.secureCLI.Update(ctx, binary.ID, gatewayOperatorBinaryPolicyUpdates(binaryPath)); err != nil {
		return nil, errGatewayOperatorCredentialFailed
	}
	binary.IsGlobal = false
	binary.Enabled = true
	binary.BinaryPath = &binaryPath
	binary.Description = gatewayOperatorDescription()
	binary.DenyArgs = gatewayOperatorDenyArgs()
	binary.DenyVerbose = gatewayOperatorDenyVerbose()
	binary.TimeoutSeconds = 30
	binary.Tips = gatewayOperatorTips()
	binary.AdapterName = nil
	return binary, nil
}

func (h *AgentsHandler) findGatewayOperatorBinaryConfig(ctx context.Context) (*store.SecureCLIBinary, error) {
	binaries, err := h.secureCLI.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range binaries {
		if strings.EqualFold(strings.TrimSpace(binaries[i].BinaryName), gatewayOperatorBinaryName) {
			b := binaries[i]
			return &b, nil
		}
	}
	return nil, nil
}

func gatewayOperatorBinaryPolicyUpdates(binaryPath string) map[string]any {
	return map[string]any{
		"binary_path":     binaryPath,
		"description":     gatewayOperatorDescription(),
		"deny_args":       gatewayOperatorDenyArgs(),
		"deny_verbose":    gatewayOperatorDenyVerbose(),
		"timeout_seconds": 30,
		"tips":            gatewayOperatorTips(),
		"is_global":       false,
		"enabled":         true,
		"adapter_name":    nil,
	}
}

func gatewayOperatorDescription() string {
	return "Local GoClaw gateway operator CLI"
}

func gatewayOperatorTips() string {
	return "Use for local gateway operations such as `goclaw agent list`. Auth/setup/migration/backup/restore and verbose/debug commands are blocked."
}

func gatewayOperatorDenyArgs() json.RawMessage {
	raw, _ := json.Marshal([]string{
		`^auth\b`,
		`^onboard\b`,
		`^setup\b`,
		`^migrate\b`,
		`^upgrade\b`,
		`^backup\b`,
		`^restore\b`,
		`^tenant-backup\b`,
		`^tenant-restore\b`,
		`^config\s+set\b`,
	})
	return raw
}

func gatewayOperatorDenyVerbose() json.RawMessage {
	raw, _ := json.Marshal([]string{`-v`, `--verbose`, `--debug`})
	return raw
}

func (h *AgentsHandler) ensureGatewayOperatorGrant(ctx context.Context, binaryID uuid.UUID, agentID uuid.UUID) (*store.SecureCLIAgentGrant, error) {
	grants, err := h.secureCLIGrants.ListByAgent(ctx, agentID)
	if err != nil {
		return nil, errGatewayOperatorCredentialFailed
	}
	for i := range grants {
		if grants[i].BinaryID != binaryID {
			continue
		}
		if !grants[i].Enabled {
			if err := h.secureCLIGrants.Update(ctx, grants[i].ID, map[string]any{"enabled": true}); err != nil {
				return nil, errGatewayOperatorCredentialFailed
			}
			grants[i].Enabled = true
		}
		return &grants[i], nil
	}

	grant := &store.SecureCLIAgentGrant{
		BinaryID: binaryID,
		AgentID:  agentID,
		Enabled:  true,
	}
	if err := h.secureCLIGrants.Create(ctx, grant); err != nil {
		return nil, errGatewayOperatorCredentialFailed
	}
	return grant, nil
}

func (h *AgentsHandler) setGatewayOperatorAgentCredential(ctx context.Context, binaryID uuid.UUID, agentID uuid.UUID) error {
	env, err := store.SerializeSecureCLIEnv(map[string]store.SecureCLIEnvEntry{
		"GOCLAW_GATEWAY_TOKEN": {
			Kind:  store.SecureCLIEnvKindSensitive,
			Value: pkgGatewayToken,
		},
		"GOCLAW_SERVER": {
			Kind:  store.SecureCLIEnvKindSensitive,
			Value: gatewayOperatorServerURL(h.gatewayAddr),
		},
	})
	if err != nil {
		return errGatewayOperatorCredentialFailed
	}
	if err := h.secureCLIAgentCreds.SetAgentCredentials(ctx, binaryID, agentID, env, gatewayOperatorActor(ctx)); err != nil {
		return errGatewayOperatorCredentialFailed
	}
	return nil
}

func gatewayOperatorWarning(locale string, err error) string {
	switch {
	case errors.Is(err, errGatewayOperatorSecureCLIUnavailable):
		return i18n.T(locale, i18n.MsgGatewayOperatorSecureCLIUnavailable)
	case errors.Is(err, errGatewayOperatorTokenMissing):
		return i18n.T(locale, i18n.MsgGatewayOperatorTokenMissing)
	case errors.Is(err, errGatewayOperatorBinaryMissing):
		return i18n.T(locale, i18n.MsgGatewayOperatorBinaryMissing)
	case errors.Is(err, errGatewayOperatorExistingReview):
		return i18n.T(locale, i18n.MsgGatewayOperatorExistingReview)
	case errors.Is(err, errGatewayOperatorRegisterFailed):
		return i18n.T(locale, i18n.MsgGatewayOperatorRegisterFailed)
	default:
		return i18n.T(locale, i18n.MsgGatewayOperatorCredentialFailed)
	}
}

func gatewayOperatorActor(ctx context.Context) string {
	if userID := strings.TrimSpace(store.UserIDFromContext(ctx)); userID != "" {
		return userID
	}
	return "system"
}

func gatewayOperatorServerURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "http://127.0.0.1:18790"
	}
	if u, err := url.Parse(addr); err == nil && u.Scheme != "" {
		return strings.TrimRight(addr, "/")
	}
	return "http://" + strings.TrimRight(addr, "/")
}

func defaultFindGatewayOperatorBinary() (string, error) {
	if exe, err := os.Executable(); err == nil && strings.EqualFold(filepath.Base(exe), gatewayOperatorBinaryName) && skills.IsExecutableFile(exe) {
		return exe, nil
	}
	if path, err := exec.LookPath(gatewayOperatorBinaryName); err == nil && skills.IsExecutableFile(path) {
		return path, nil
	}
	if path, ok := skills.FindRuntimeExecutable(gatewayOperatorBinaryName); ok && skills.IsExecutableFile(path) {
		return path, nil
	}
	return "", fmt.Errorf("%s binary not found", gatewayOperatorBinaryName)
}
