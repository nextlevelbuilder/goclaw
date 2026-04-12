package vault

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// phase25TaskLinking implements task-based auto-linking (Phase 04).
// Runs after Phase 2 embed, before Phase 3 classify. Uses dedicated
// link_type LinkTypeTaskAttachment (outside validClassifyTypes) so
// DeleteDocLinksByTypes in classify cannot wipe these links.
//
// Single batched query to BatchGetTaskSiblingsByBasenames + single
// batched CreateLinks call — O(1) DB round trips per chunk.
//
// Nil-tolerant: teamStore may be unset (e.g. during tests), in which
// case the phase is a silent no-op.
func (w *enrichWorker) phase25TaskLinking(ctx context.Context, embedded []enriched, docMap map[string]*store.VaultDocument) {
	if w.teamStore == nil || len(embedded) == 0 {
		return
	}
	first := embedded[0].payload
	tenantUUID, terr := uuid.Parse(first.TenantID)
	if terr != nil {
		slog.Warn("vault.enrich.phase2_5: parse tenant", "err", terr)
		return
	}

	// Collect unique basenames from the chunk using Phase 0's docMap.
	baseNames := make([]string, 0, len(embedded))
	docByBasename := make(map[string][]*enriched, len(embedded))
	seenBase := make(map[string]bool, len(embedded))
	for i := range embedded {
		ex := docMap[embedded[i].payload.DocID]
		if ex == nil || ex.PathBasename == "" {
			continue
		}
		if !seenBase[ex.PathBasename] {
			seenBase[ex.PathBasename] = true
			baseNames = append(baseNames, ex.PathBasename)
		}
		docByBasename[ex.PathBasename] = append(docByBasename[ex.PathBasename], &embedded[i])
	}
	if len(baseNames) == 0 {
		return
	}

	siblingsByBase, err := w.teamStore.BatchGetTaskSiblingsByBasenames(
		ctx, tenantUUID, baseNames, enrichTaskSiblingCap,
	)
	if err != nil {
		slog.Warn("vault.enrich.phase2_5: batch_task_siblings", "err", err)
		return
	}
	if len(siblingsByBase) == 0 {
		return
	}

	var links []store.VaultLink
	for baseName, docs := range docByBasename {
		sibs := siblingsByBase[baseName]
		if len(sibs) == 0 {
			continue
		}
		for _, d := range docs {
			seen := make(map[string]bool, len(sibs))
			for _, sib := range sibs {
				sibID := sib.DocID.String()
				if seen[sibID] || sibID == d.payload.DocID {
					continue
				}
				seen[sibID] = true
				source := "task:" + sib.TaskID.String()
				links = append(links, store.VaultLink{
					FromDocID: d.payload.DocID,
					ToDocID:   sibID,
					LinkType:  LinkTypeTaskAttachment,
					Context:   source, // ONLY task id ref — no subject text (PII safety)
					Metadata:  map[string]any{"source": source},
				})
			}
		}
	}
	if len(links) == 0 {
		return
	}
	if err := w.vault.CreateLinks(ctx, links); err != nil {
		slog.Debug("vault.enrich.phase2_5: create_links", "err", err)
		return
	}
	slog.Info("vault.link.created",
		"source_type", "task",
		"count", len(links))
}

// phase26DelegationLinking implements delegation-based auto-linking
// (Phase 05). Runs after Phase 2.5, before Phase 3 classify. Uses
// dedicated link_type LinkTypeDelegationAttachment.
//
// Batched query via VaultStore.BatchFindByDelegationIDs; single
// CreateLinks call. No-op when no embedded doc carries metadata.delegation_id.
func (w *enrichWorker) phase26DelegationLinking(ctx context.Context, embedded []enriched, docMap map[string]*store.VaultDocument) {
	if len(embedded) == 0 {
		return
	}
	first := embedded[0].payload
	tenant := first.TenantID

	delegIDs := make([]string, 0, len(embedded))
	docsByDelegID := make(map[string][]*store.VaultDocument, len(embedded))
	excludeDocIDs := make([]string, 0, len(embedded))
	seenDeleg := make(map[string]bool, len(embedded))

	for i := range embedded {
		existing := docMap[embedded[i].payload.DocID]
		if existing == nil || len(existing.Metadata) == 0 {
			continue
		}
		delegID, _ := existing.Metadata["delegation_id"].(string)
		if delegID == "" {
			continue
		}
		if !seenDeleg[delegID] {
			seenDeleg[delegID] = true
			delegIDs = append(delegIDs, delegID)
		}
		docsByDelegID[delegID] = append(docsByDelegID[delegID], existing)
		excludeDocIDs = append(excludeDocIDs, existing.ID)
	}
	if len(delegIDs) == 0 {
		return
	}

	siblingsByDeleg, err := w.vault.BatchFindByDelegationIDs(
		ctx, tenant, delegIDs, enrichTaskSiblingCap, excludeDocIDs,
	)
	if err != nil {
		slog.Debug("vault.enrich.phase2_6: delegation_siblings_batch", "err", err)
		return
	}

	var links []store.VaultLink
	for delegID, srcDocs := range docsByDelegID {
		sibs := siblingsByDeleg[delegID]
		if len(sibs) == 0 {
			continue
		}
		source := "delegation:" + delegID
		for _, src := range srcDocs {
			for _, sib := range sibs {
				if sib.ID == src.ID {
					continue
				}
				links = append(links, store.VaultLink{
					FromDocID: src.ID,
					ToDocID:   sib.ID,
					LinkType:  LinkTypeDelegationAttachment,
					Context:   source,
					Metadata:  map[string]any{"source": source},
				})
			}
		}
	}
	if len(links) == 0 {
		return
	}
	if err := w.vault.CreateLinks(ctx, links); err != nil {
		slog.Debug("vault.enrich.phase2_6: create_links", "err", err)
		return
	}
	slog.Info("vault.link.created",
		"source_type", "delegation",
		"count", len(links))
}
