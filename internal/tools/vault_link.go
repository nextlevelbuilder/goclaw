package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// VaultLinkTool creates explicit links between vault documents.
type VaultLinkTool struct {
	vaultStore store.VaultStore
}

func NewVaultLinkTool() *VaultLinkTool {
	return &VaultLinkTool{}
}

func (t *VaultLinkTool) SetVaultStore(vs store.VaultStore) {
	t.vaultStore = vs
}

func (t *VaultLinkTool) Name() string { return "vault_link" }

func (t *VaultLinkTool) Description() string {
	return "Create an explicit link between two vault documents. Similar to [[wikilinks]] in markdown."
}

func (t *VaultLinkTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"from": map[string]any{
				"type":        "string",
				"description": "Source document path (workspace-relative)",
			},
			"to": map[string]any{
				"type":        "string",
				"description": "Target document path (workspace-relative)",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "Optional note describing the link relationship",
			},
			"link_type": map[string]any{
				"type":        "string",
				"description": "Link type: wikilink (default) or reference",
				"enum":        []string{"wikilink", "reference"},
			},
		},
		"required": []string{"from", "to"},
	}
}

func (t *VaultLinkTool) Execute(ctx context.Context, args map[string]any) *Result {
	fromPath, _ := args["from"].(string)
	toPath, _ := args["to"].(string)
	linkCtx, _ := args["context"].(string)
	linkType, _ := args["link_type"].(string)
	if linkType == "" {
		linkType = "wikilink"
	}

	if fromPath == "" || toPath == "" {
		return ErrorResult("both 'from' and 'to' paths are required")
	}
	if linkType != "wikilink" && linkType != "reference" {
		return ErrorResult("link_type must be 'wikilink' or 'reference'")
	}

	agentID := store.AgentIDFromContext(ctx)
	tenantID := store.TenantIDFromContext(ctx)
	if t.vaultStore == nil || agentID == uuid.Nil {
		return ErrorResult("vault not available")
	}

	tid := tenantID.String()
	aid := agentID.String()

	// Infer scope and team from context.
	var teamID *string
	scope := "personal"
	if rc := store.RunContextFromCtx(ctx); rc != nil && rc.TeamID != "" {
		teamID = &rc.TeamID
		scope = "team"
	}

	// Resolve source doc (auto-register if not in vault)
	fromDoc, err := t.resolveOrRegister(ctx, tid, aid, teamID, scope, fromPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("cannot resolve source doc: %v", err))
	}

	// Resolve target doc (auto-register if not in vault)
	toDoc, err := t.resolveOrRegister(ctx, tid, aid, teamID, scope, toPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("cannot resolve target doc: %v", err))
	}

	// Block cross-team links (both team docs must be same team).
	if fromDoc.TeamID != nil && toDoc.TeamID != nil && *fromDoc.TeamID != *toDoc.TeamID {
		return ErrorResult("cannot link documents from different teams")
	}

	link := &store.VaultLink{
		FromDocID: fromDoc.ID,
		ToDocID:   toDoc.ID,
		LinkType:  linkType,
		Context:   linkCtx,
	}
	if err := t.vaultStore.CreateLink(ctx, link); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create link: %v", err))
	}

	return NewResult(fmt.Sprintf("Linked %s → %s", fromPath, toPath))
}

// resolveOrRegister finds a vault doc by path, or creates a stub entry with team context.
func (t *VaultLinkTool) resolveOrRegister(ctx context.Context, tenantID, agentID string, teamID *string, scope, path string) (*store.VaultDocument, error) {
	doc, err := t.vaultStore.GetDocument(ctx, tenantID, agentID, path)
	if err == nil && doc != nil {
		return doc, nil
	}
	// Auto-register stub with team context.
	doc = &store.VaultDocument{
		TenantID: tenantID,
		AgentID:  agentID,
		TeamID:   teamID,
		Scope:    scope,
		Path:     path,
		Title:    strings.TrimSuffix(path, ".md"),
		DocType:  inferVaultDocType(path),
	}
	if err := t.vaultStore.UpsertDocument(ctx, doc); err != nil {
		return nil, err
	}
	return t.vaultStore.GetDocument(ctx, tenantID, agentID, path)
}

// VaultBacklinksTool shows all documents linking to a specific document.
type VaultBacklinksTool struct {
	vaultStore store.VaultStore
}

func NewVaultBacklinksTool() *VaultBacklinksTool {
	return &VaultBacklinksTool{}
}

func (t *VaultBacklinksTool) SetVaultStore(vs store.VaultStore) {
	t.vaultStore = vs
}

func (t *VaultBacklinksTool) Name() string { return "vault_backlinks" }

func (t *VaultBacklinksTool) Description() string {
	return "Show all documents that link TO this document. Useful for tracing dependencies and references."
}

func (t *VaultBacklinksTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Document path to find backlinks for",
			},
		},
		"required": []string{"path"},
	}
}

func (t *VaultBacklinksTool) Execute(ctx context.Context, args map[string]any) *Result {
	path, _ := args["path"].(string)
	if path == "" {
		return ErrorResult("path parameter is required")
	}

	agentID := store.AgentIDFromContext(ctx)
	tenantID := store.TenantIDFromContext(ctx)
	if t.vaultStore == nil || agentID == uuid.Nil {
		return ErrorResult("vault not available")
	}

	doc, err := t.vaultStore.GetDocument(ctx, tenantID.String(), agentID.String(), path)
	if err != nil {
		return ErrorResult(fmt.Sprintf("document not found in vault: %s", path))
	}

	backlinks, err := t.vaultStore.GetBacklinks(ctx, tenantID.String(), doc.ID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to get backlinks: %v", err))
	}

	// Determine current team context for filtering.
	var currentTeamID string
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		currentTeamID = rc.TeamID
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Documents linking to %s:\n\n", path))
	count := 0
	for _, bl := range backlinks {
		// Team boundary filter:
		// - Team context: show same-team docs only (hide personal + other teams)
		// - Personal context: show personal docs only (hide team docs)
		if bl.TeamID != nil && *bl.TeamID != "" {
			if currentTeamID == "" || *bl.TeamID != currentTeamID {
				continue
			}
		} else {
			// Personal doc — hide in team context (prevents exfiltration)
			if currentTeamID != "" {
				continue
			}
		}

		count++
		sb.WriteString(fmt.Sprintf("%d. %s (%s)", count, bl.Title, bl.Path))
		if bl.Context != "" {
			sb.WriteString(fmt.Sprintf(" — \"%s\"", bl.Context))
		}
		sb.WriteByte('\n')
	}

	if count == 0 {
		return NewResult(fmt.Sprintf("No documents link to %s", path))
	}
	return NewResult(sb.String())
}
