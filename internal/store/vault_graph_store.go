package store

import "context"

// GraphNode is a lightweight vault document for graph visualization.
type GraphNode struct {
	ID      string `json:"id"`
	Title   string `json:"t"`  // short key for wire size
	Path    string `json:"p"`  // workspace-relative path
	DocType string `json:"dt"` // doc_type
	Degree  int    `json:"deg"`
}

// GraphEdge is a lightweight vault link for graph visualization.
type GraphEdge struct {
	ID       string `json:"id"`
	FromID   string `json:"from"`
	ToID     string `json:"to"`
	LinkType string `json:"type"`
}

// KGGraphNode is a lightweight KG entity for graph visualization.
type KGGraphNode struct {
	ID         string  `json:"id"`
	Name       string  `json:"n"`
	EntityType string  `json:"t"`
	Confidence float64 `json:"c"`
}

// KGGraphEdge is a lightweight KG relation for graph visualization.
type KGGraphEdge struct {
	ID           string `json:"id"`
	SourceID     string `json:"src"`
	TargetID     string `json:"tgt"`
	RelationType string `json:"type"`
}

// VaultGraphListOptions configures a vault graph query.
type VaultGraphListOptions struct {
	TeamID  *string
	TeamIDs []string
	Limit   int // max 10000
}

// VaultGraphStore provides read-only graph data for visualization.
type VaultGraphStore interface {
	// ListGraphNodes returns lightweight nodes with pre-computed degree.
	ListGraphNodes(ctx context.Context, tenantID, agentID string, opts VaultGraphListOptions) ([]GraphNode, int, error)
	// ListGraphEdges returns lightweight edges for nodes in scope.
	ListGraphEdges(ctx context.Context, tenantID, agentID string, opts VaultGraphListOptions) ([]GraphEdge, int, error)
}

// KGGraphStore provides read-only KG graph data for visualization.
type KGGraphStore interface {
	// ListKGGraphNodes returns lightweight entities.
	ListKGGraphNodes(ctx context.Context, agentID, userID string, limit int) ([]KGGraphNode, int, error)
	// ListKGGraphEdges returns lightweight relations.
	ListKGGraphEdges(ctx context.Context, agentID, userID string, limit int) ([]KGGraphEdge, int, error)
}
