package vault

import (
	"context"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// wikilinkRe matches [[target]] and [[target|display text]].
var wikilinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)

// WikilinkMatch is a single parsed wikilink.
type WikilinkMatch struct {
	Target  string // resolved target path (no display text)
	Context string // ~50 chars surrounding the link
	Offset  int    // byte offset in content
}

// ExtractWikilinks parses all [[wikilinks]] from markdown content.
func ExtractWikilinks(content string) []WikilinkMatch {
	matches := wikilinkRe.FindAllStringSubmatchIndex(content, -1)
	var result []WikilinkMatch
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		target := strings.TrimSpace(content[m[2]:m[3]])
		if target == "" {
			continue
		}

		// Build context: ~25 chars before and after the link
		start := max(m[0]-25, 0)
		end := min(m[1]+25, len(content))
		ctx := content[start:end]

		result = append(result, WikilinkMatch{
			Target:  target,
			Context: ctx,
			Offset:  m[0],
		})
	}
	return result
}

// ResolveWikilinkTarget finds a vault_document matching the wikilink target.
// Strategy: exact path -> path+.md -> basename match -> nil.
func ResolveWikilinkTarget(ctx context.Context, vs store.VaultStore, target, tenantID, agentID string) (*store.VaultDocument, error) {
	// 1. Exact path match
	doc, err := vs.GetDocument(ctx, tenantID, agentID, target)
	if err == nil && doc != nil {
		return doc, nil
	}

	// 2. With .md suffix
	if !strings.HasSuffix(target, ".md") {
		doc, err = vs.GetDocument(ctx, tenantID, agentID, target+".md")
		if err == nil && doc != nil {
			return doc, nil
		}
	}

	// 3. Basename search: list all docs and find by basename
	docs, err := vs.ListDocuments(ctx, tenantID, agentID, store.VaultListOptions{Limit: 500})
	if err != nil {
		return nil, err
	}
	targetBase := strings.ToLower(filepath.Base(target))
	targetBaseMD := targetBase
	if !strings.HasSuffix(targetBaseMD, ".md") {
		targetBaseMD = targetBase + ".md"
	}
	for i := range docs {
		base := strings.ToLower(filepath.Base(docs[i].Path))
		if base == targetBase || base == targetBaseMD {
			return &docs[i], nil
		}
	}

	return nil, nil // unresolved — not an error
}

// SyncDocLinks extracts wikilinks from content, resolves targets,
// and replaces all vault_links for the source document.
func SyncDocLinks(ctx context.Context, vs store.VaultStore, doc *store.VaultDocument, content, tenantID, agentID string) error {
	matches := ExtractWikilinks(content)
	if len(matches) == 0 {
		// No links — delete existing outbound links
		return vs.DeleteDocLinks(ctx, tenantID, doc.ID)
	}

	// Delete old outbound links first (replace strategy)
	if err := vs.DeleteDocLinks(ctx, tenantID, doc.ID); err != nil {
		return err
	}

	// Resolve and create new links
	for _, m := range matches {
		target, err := ResolveWikilinkTarget(ctx, vs, m.Target, tenantID, agentID)
		if err != nil {
			slog.Debug("vault.link_resolve_error", "target", m.Target, "err", err)
			continue
		}
		if target == nil {
			slog.Debug("vault.link_unresolved", "target", m.Target)
			continue
		}
		link := &store.VaultLink{
			FromDocID: doc.ID,
			ToDocID:   target.ID,
			LinkType:  "wikilink",
			Context:   m.Context,
		}
		if err := vs.CreateLink(ctx, link); err != nil {
			slog.Warn("vault.create_link", "from", doc.Path, "to", target.Path, "err", err)
		}
	}
	return nil
}
