package vault

// Dedicated link_type constants for deterministic auto-linking phases
// (Phase 04 task-based, Phase 05 delegation-based). These types live
// OUTSIDE validClassifyTypes in enrich_classify.go so DeleteDocLinksByTypes
// cannot wipe them on the same enrichment tick (red-team blocker #1).
const (
	LinkTypeTaskAttachment       = "task_attachment"
	LinkTypeDelegationAttachment = "delegation_attachment"
)
