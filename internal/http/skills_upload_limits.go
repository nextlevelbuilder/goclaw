package http

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	skillUploadMaxSizeConfigKey       = config.SkillMaxUploadSizeSystemConfigKey
	skillUploadMultipartOverheadBytes = int64(1 << 20)
)

func (h *SkillsHandler) SetUploadLimitConfig(cfg config.SkillsConfig) {
	h.uploadLimitCfg = cfg
}

func (h *SkillsHandler) SetSystemConfigStore(s store.SystemConfigStore) {
	h.systemConfigs = s
}

func (h *SkillsHandler) resolveSkillUploadLimitMB(ctx context.Context, frontmatter map[string]string) int {
	if mb, ok := h.resolveTenantSkillUploadLimitMB(ctx); ok {
		return mb
	}
	if mb, ok := parseSkillUploadLimitMB(frontmatter["max_upload_size_mb"]); ok {
		return config.ClampSkillMaxUploadSizeMB(mb)
	}
	return h.uploadLimitCfg.EffectiveMaxUploadSizeMB()
}

func (h *SkillsHandler) resolvePreParseUploadLimitMB(ctx context.Context) int {
	if mb, ok := h.resolveTenantSkillUploadLimitMB(ctx); ok {
		return mb
	}
	return config.MaxSkillMaxUploadSizeMB
}

func (h *SkillsHandler) resolveTenantSkillUploadLimitMB(ctx context.Context) (int, bool) {
	if h.systemConfigs == nil {
		return 0, false
	}
	raw, err := h.systemConfigs.Get(ctx, skillUploadMaxSizeConfigKey)
	if err != nil {
		return 0, false
	}
	mb, ok := parseSkillUploadLimitMB(raw)
	if !ok {
		return 0, false
	}
	return config.ClampSkillMaxUploadSizeMB(mb), true
}

func parseSkillUploadLimitMB(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	mb, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return mb, true
}

func skillUploadLimitBytes(limitMB int) int64 {
	return int64(config.ClampSkillMaxUploadSizeMB(limitMB)) << 20
}

func skillUploadBodyBytes(limitMB int) int64 {
	return skillUploadLimitBytes(limitMB) + skillUploadMultipartOverheadBytes
}

func skillUploadTooLargeMessage(size int64, limitMB int) string {
	return fmt.Sprintf("skill ZIP size %s exceeds %d MB limit", formatUploadBytes(size), limitMB)
}

func formatUploadBytes(size int64) string {
	const mb = int64(1 << 20)
	if size >= mb {
		return fmt.Sprintf("%.1f MB", float64(size)/float64(mb))
	}
	return fmt.Sprintf("%d bytes", size)
}
