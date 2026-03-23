package methods

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// --- agent.identity.get ---
// Matching TS src/gateway/server-methods/agent.ts:601-643

func (m *AgentsMethods) handleIdentityGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		AgentID    string `json:"agentId"`
		SessionKey string `json:"sessionKey"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.AgentID == "" {
		// Try to extract from sessionKey: "agent:{agentId}:..."
		if params.SessionKey != "" {
			parts := strings.SplitN(params.SessionKey, ":", 3)
			if len(parts) >= 2 {
				params.AgentID = parts[1]
			}
		}
		if params.AgentID == "" {
			params.AgentID = "default"
		}
	}

	result := map[string]any{
		"agentId": params.AgentID,
	}

	if m.agentStore != nil {
		// --- DB-backed: read identity from store ---
		ag, err := m.agentStore.GetByKey(ctx, params.AgentID)
		if err == nil {
			result["name"] = ag.DisplayName

			// Parse IDENTITY.md from DB bootstrap
			dbFiles, _ := m.agentStore.GetAgentContextFiles(ctx, ag.ID)
			for _, f := range dbFiles {
				if f.FileName == "IDENTITY.md" {
					if identity := parseIdentityContent(f.Content); identity != nil {
						if identity["Name"] != "" {
							result["name"] = identity["Name"]
						}
						if identity["Emoji"] != "" {
							result["emoji"] = identity["Emoji"]
						}
						if identity["Avatar"] != "" {
							result["avatar"] = identity["Avatar"]
						}
						if identity["Description"] != "" {
							result["description"] = identity["Description"]
						}
					}
					break
				}
			}
		}
	} else {
		// --- Fallback: config + filesystem ---
		result["name"] = m.cfg.ResolveDisplayName(params.AgentID)

		if spec, ok := m.cfg.Agents.List[params.AgentID]; ok && spec.Identity != nil {
			if spec.Identity.Emoji != "" {
				result["emoji"] = spec.Identity.Emoji
			}
			if spec.Identity.Name != "" {
				result["name"] = spec.Identity.Name
			}
		}

		ws := m.resolveWorkspace(params.AgentID)
		identityPath := filepath.Join(ws, "IDENTITY.md")
		if identity := parseIdentityFile(identityPath); identity != nil {
			if identity["Name"] != "" {
				result["name"] = identity["Name"]
			}
			if identity["Emoji"] != "" {
				result["emoji"] = identity["Emoji"]
			}
			if identity["Avatar"] != "" {
				result["avatar"] = identity["Avatar"]
			}
			if identity["Description"] != "" {
				result["description"] = identity["Description"]
			}
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, result))
}

// parseIdentityContent parses IDENTITY.md content string and extracts Key: Value fields.
func parseIdentityContent(content string) map[string]string {
	result := make(map[string]string)
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if val != "" {
				result[key] = val
			}
		}
	}
	return result
}

// parseIdentityFile reads IDENTITY.md and extracts Key: Value fields.
func parseIdentityFile(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if val != "" {
				result[key] = val
			}
		}
	}
	return result
}

// buildIdentityContent creates the content for IDENTITY.md from fields.
func buildIdentityContent(name, emoji, avatar string) string {
	var lines []string
	lines = append(lines, "# Identity")
	if name != "" {
		lines = append(lines, "Name: "+name)
	}
	if emoji != "" {
		lines = append(lines, "Emoji: "+emoji)
	}
	if avatar != "" {
		lines = append(lines, "Avatar: "+avatar)
	}
	return strings.Join(lines, "\n") + "\n"
}

// appendIdentityFields appends Name/Emoji/Avatar to IDENTITY.md.
func appendIdentityFields(path string, name, emoji, avatar string) {
	var lines []string
	if name != "" {
		lines = append(lines, "Name: "+name)
	}
	if emoji != "" {
		lines = append(lines, "Emoji: "+emoji)
	}
	if avatar != "" {
		lines = append(lines, "Avatar: "+avatar)
	}
	if len(lines) == 0 {
		return
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString("\n" + strings.Join(lines, "\n") + "\n")
}
