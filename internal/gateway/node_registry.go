package gateway

import (
	"slices"
	"sort"
	"sync"
	"time"
)

// NodeConn represents a connected node-host.
type NodeConn struct {
	NodeID       string
	DisplayName  string
	Commands     []string
	Client       *Client
	RegisteredAt time.Time
}

// NodeInfo is a read-only snapshot of a connected node for admin/debug.
type NodeInfo struct {
	NodeID       string    `json:"nodeId"`
	DisplayName  string    `json:"displayName"`
	Commands     []string  `json:"commands"`
	ClientID     string    `json:"clientId"`
	RegisteredAt time.Time `json:"registeredAt"`
}

// NodeRegistry tracks connected node-host instances.
// Thread-safe for concurrent access from agent dispatches and WS handlers.
type NodeRegistry struct {
	mu    sync.RWMutex
	nodes map[string]*NodeConn // keyed by client ID (not node ID, since multiple instances may share a node ID)
	// round-robin index per command for load distribution
	rrIndex map[string]int
}

// NewNodeRegistry creates an empty node registry.
func NewNodeRegistry() *NodeRegistry {
	return &NodeRegistry{
		nodes:   make(map[string]*NodeConn),
		rrIndex: make(map[string]int),
	}
}

// Register adds a node connection. Called when a client connects with role "node".
func (r *NodeRegistry) Register(client *Client, nodeID, displayName string, commands []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[client.id] = &NodeConn{
		NodeID:       nodeID,
		DisplayName:  displayName,
		Commands:     commands,
		Client:       client,
		RegisteredAt: time.Now(),
	}
}

// Unregister removes a node connection. Called on client disconnect.
func (r *NodeRegistry) Unregister(clientID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.nodes, clientID)
}

// Pick selects a node that supports the given command using round-robin.
// Returns nil if no suitable node is connected.
func (r *NodeRegistry) Pick(command string) *NodeConn {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Collect candidates.
	var candidates []*NodeConn
	for _, n := range r.nodes {
		if slices.Contains(n.Commands, command) {
			candidates = append(candidates, n)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	// Sort by client ID for deterministic round-robin (Go map iteration is random).
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Client.id < candidates[j].Client.id
	})

	idx := r.rrIndex[command] % len(candidates)
	r.rrIndex[command] = idx + 1
	return candidates[idx]
}

// List returns a snapshot of all connected nodes.
func (r *NodeRegistry) List() []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]NodeInfo, 0, len(r.nodes))
	for _, n := range r.nodes {
		list = append(list, NodeInfo{
			NodeID:       n.NodeID,
			DisplayName:  n.DisplayName,
			Commands:     n.Commands,
			ClientID:     n.Client.id,
			RegisteredAt: n.RegisteredAt,
		})
	}
	return list
}

// Count returns the number of connected nodes.
func (r *NodeRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.nodes)
}
