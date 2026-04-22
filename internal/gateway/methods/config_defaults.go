package methods

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Response shape matches TS AgentConfig (ui/web/src/types/agent.ts).
// Nested keys (softTrim.*, hardClear.*) stay aligned with ContextPruningConfig.
type configDefaultsResponse struct {
	Agents configDefaultsAgents `json:"agents"`
}

type configDefaultsAgents struct {
	ContextPruning pruningDefaultsJSON  `json:"contextPruning"`
	Subagents      subagentDefaultsJSON `json:"subagents"`
}

type pruningDefaultsJSON struct {
	KeepLastAssistants   int                   `json:"keepLastAssistants"`
	SoftTrimRatio        float64               `json:"softTrimRatio"`
	HardClearRatio       float64               `json:"hardClearRatio"`
	MinPrunableToolChars int                   `json:"minPrunableToolChars"`
	TTL                  string                `json:"ttl"`
	SoftTrim             pruningSoftTrimJSON   `json:"softTrim"`
	HardClear            pruningHardClearJSON  `json:"hardClear"`
}

type pruningSoftTrimJSON struct {
	MaxChars  int `json:"maxChars"`
	HeadChars int `json:"headChars"`
	TailChars int `json:"tailChars"`
}

type pruningHardClearJSON struct {
	Enabled     bool   `json:"enabled"`
	Placeholder string `json:"placeholder"`
}

type subagentDefaultsJSON struct {
	MaxConcurrent       int `json:"maxConcurrent"`
	MaxSpawnDepth       int `json:"maxSpawnDepth"`
	MaxChildrenPerAgent int `json:"maxChildrenPerAgent"`
	ArchiveAfterMinutes int `json:"archiveAfterMinutes"`
	MaxRetries          int `json:"maxRetries"`
}

// handleDefaults returns nested defaults for UI placeholder consumption.
// Read-only — no mutation; the response is pure SSoT + user overlay snapshot.
func (m *ConfigMethods) handleDefaults(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	client.SendResponse(protocol.NewOKResponse(req.ID, buildConfigDefaults(m.cfg)))
}

// buildConfigDefaults seeds from Go consts (SSoT) then overlays any non-zero
// fields the operator set in cfg.Agents.Defaults. Overlay mirrors the resolver
// semantics in resolvePruningSettings so UI placeholders match runtime behaviour.
func buildConfigDefaults(cfg *config.Config) configDefaultsResponse {
	p := agent.DefaultPruningValues()
	s := tools.DefaultSubagentConfig()

	resp := configDefaultsResponse{
		Agents: configDefaultsAgents{
			ContextPruning: pruningDefaultsJSON{
				KeepLastAssistants:   p.KeepLastAssistants,
				SoftTrimRatio:        p.SoftTrimRatio,
				HardClearRatio:       p.HardClearRatio,
				MinPrunableToolChars: p.MinPrunableToolChars,
				TTL:                  p.TTL,
				SoftTrim: pruningSoftTrimJSON{
					MaxChars:  p.SoftTrimMaxChars,
					HeadChars: p.SoftTrimHeadChars,
					TailChars: p.SoftTrimTailChars,
				},
				HardClear: pruningHardClearJSON{
					Enabled:     p.HardClearEnabled,
					Placeholder: p.HardClearPlaceholder,
				},
			},
			Subagents: subagentDefaultsJSON{
				MaxConcurrent:       s.MaxConcurrent,
				MaxSpawnDepth:       s.MaxSpawnDepth,
				MaxChildrenPerAgent: s.MaxChildrenPerAgent,
				ArchiveAfterMinutes: s.ArchiveAfterMinutes,
				MaxRetries:          s.MaxRetries,
			},
		},
	}

	if cfg == nil {
		return resp
	}
	overlayPruning(&resp.Agents.ContextPruning, cfg.Agents.Defaults.ContextPruning)
	overlaySubagents(&resp.Agents.Subagents, cfg.Agents.Defaults.Subagents)
	return resp
}

func overlayPruning(dst *pruningDefaultsJSON, src *config.ContextPruningConfig) {
	if src == nil {
		return
	}
	if src.KeepLastAssistants > 0 {
		dst.KeepLastAssistants = src.KeepLastAssistants
	}
	if src.SoftTrimRatio > 0 && src.SoftTrimRatio <= 1 {
		dst.SoftTrimRatio = src.SoftTrimRatio
	}
	if src.HardClearRatio > 0 && src.HardClearRatio <= 1 {
		dst.HardClearRatio = src.HardClearRatio
	}
	if src.MinPrunableToolChars > 0 {
		dst.MinPrunableToolChars = src.MinPrunableToolChars
	}
	if src.TTL != "" {
		dst.TTL = src.TTL
	}
	if src.SoftTrim != nil {
		if src.SoftTrim.MaxChars > 0 {
			dst.SoftTrim.MaxChars = src.SoftTrim.MaxChars
		}
		if src.SoftTrim.HeadChars > 0 {
			dst.SoftTrim.HeadChars = src.SoftTrim.HeadChars
		}
		if src.SoftTrim.TailChars > 0 {
			dst.SoftTrim.TailChars = src.SoftTrim.TailChars
		}
	}
	if src.HardClear != nil {
		if src.HardClear.Enabled != nil {
			dst.HardClear.Enabled = *src.HardClear.Enabled
		}
		if src.HardClear.Placeholder != "" {
			dst.HardClear.Placeholder = src.HardClear.Placeholder
		}
	}
}

func overlaySubagents(dst *subagentDefaultsJSON, src *config.SubagentsConfig) {
	if src == nil {
		return
	}
	if src.MaxConcurrent > 0 {
		dst.MaxConcurrent = src.MaxConcurrent
	}
	// Mirror runtime clamps in cmd/gateway_agents.go so UI placeholders reflect
	// the effective value (not operator's raw over-limit config.json).
	if src.MaxSpawnDepth > 0 {
		dst.MaxSpawnDepth = min(src.MaxSpawnDepth, 5)
	}
	if src.MaxChildrenPerAgent > 0 {
		dst.MaxChildrenPerAgent = min(src.MaxChildrenPerAgent, 20)
	}
	if src.ArchiveAfterMinutes > 0 {
		dst.ArchiveAfterMinutes = src.ArchiveAfterMinutes
	}
	if src.MaxRetries > 0 {
		dst.MaxRetries = src.MaxRetries
	}
}
