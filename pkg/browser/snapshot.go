package browser

import (
	"fmt"
	"strings"

	"github.com/go-rod/rod/lib/proto"
)

// axValue extracts a string value from an AXValue.
// Ported from TS cdp.ts:170-190 (axValue function).
func axValue(v *proto.AccessibilityAXValue) string {
	if v == nil {
		return ""
	}
	// gson.JSON — call Str() for string, or String() for raw JSON
	s := v.Value.Str()
	if s != "" {
		return s
	}
	// Try raw string representation for numbers/booleans
	raw := v.Value.String()
	if raw == "" || raw == "null" || raw == "\"\"" {
		return ""
	}
	return raw
}

// axNodeTree is an internal tree node built from flat AX nodes.
type axNodeTree struct {
	node  *proto.AccessibilityAXNode
	depth int
}

// buildAXTree converts flat CDP AX nodes into a tree structure.
// Ported from TS cdp.ts:192-249 (formatAriaSnapshot).
func buildAXTree(nodes []*proto.AccessibilityAXNode, limit int) []*axNodeTree {
	if len(nodes) == 0 {
		return nil
	}

	byID := make(map[proto.AccessibilityAXNodeID]*proto.AccessibilityAXNode, len(nodes))
	for _, n := range nodes {
		if n.NodeID != "" {
			byID[n.NodeID] = n
		}
	}

	// Find root: a node not referenced as a child by any other node
	referenced := make(map[proto.AccessibilityAXNodeID]bool)
	for _, n := range nodes {
		for _, cid := range n.ChildIDs {
			referenced[cid] = true
		}
	}

	var root *proto.AccessibilityAXNode
	for _, n := range nodes {
		if n.NodeID != "" && !referenced[n.NodeID] {
			root = n
			break
		}
	}
	if root == nil && len(nodes) > 0 {
		root = nodes[0]
	}
	if root == nil || root.NodeID == "" {
		return nil
	}

	// DFS traversal using explicit stack (matching TS behavior)
	var result []*axNodeTree
	type stackItem struct {
		id    proto.AccessibilityAXNodeID
		depth int
	}
	stack := []stackItem{{id: root.NodeID, depth: 0}}

	for len(stack) > 0 && len(result) < limit {
		item := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		n, ok := byID[item.id]
		if !ok {
			continue
		}

		treeNode := &axNodeTree{
			node:  n,
			depth: item.depth,
		}
		result = append(result, treeNode)

		// Push children in reverse order so first child is processed first
		children := n.ChildIDs
		for i := len(children) - 1; i >= 0; i-- {
			cid := children[i]
			if _, exists := byID[cid]; exists {
				stack = append(stack, stackItem{id: cid, depth: item.depth + 1})
			}
		}
	}

	return result
}

// roleNameTracker tracks role+name combinations for nth deduplication.
// Ported from TS pw-role-snapshot.ts:129-170.
type roleNameTracker struct {
	counts    map[string]int
	refsByKey map[string][]string
}

func newRoleNameTracker() *roleNameTracker {
	return &roleNameTracker{
		counts:    make(map[string]int),
		refsByKey: make(map[string][]string),
	}
}

func (t *roleNameTracker) key(role, name string) string {
	return role + ":" + name
}

func (t *roleNameTracker) getNextIndex(role, name string) int {
	k := t.key(role, name)
	idx := t.counts[k]
	t.counts[k] = idx + 1
	return idx
}

func (t *roleNameTracker) trackRef(role, name, ref string) {
	k := t.key(role, name)
	t.refsByKey[k] = append(t.refsByKey[k], ref)
}

func (t *roleNameTracker) getDuplicateKeys() map[string]bool {
	dups := make(map[string]bool)
	for k, refs := range t.refsByKey {
		if len(refs) > 1 {
			dups[k] = true
		}
	}
	return dups
}

// removeNthFromNonDuplicates cleans up nth=0 from refs that have no duplicates.
func removeNthFromNonDuplicates(refs map[string]RoleRef, tracker *roleNameTracker) {
	dups := tracker.getDuplicateKeys()
	for ref, data := range refs {
		k := tracker.key(data.Role, data.Name)
		if !dups[k] {
			data.Nth = 0
			refs[ref] = data
		}
	}
}

// FormatSnapshot converts raw CDP AX nodes into a text tree with refs.
// This is the core algorithm combining:
// - TS cdp.ts:192-249 (formatAriaSnapshot) — tree building
// - TS pw-role-snapshot.ts:207-267 (processLine) — role filtering + ref assignment
func FormatSnapshot(nodes []*proto.AccessibilityAXNode, opts SnapshotOptions) *SnapshotResult {
	if opts.MaxChars == 0 {
		opts.MaxChars = 8000
	}
	if opts.Limit == 0 {
		opts.Limit = 500
	}

	treeNodes := buildAXTree(nodes, opts.Limit)
	if len(treeNodes) == 0 {
		return &SnapshotResult{
			Snapshot: "(empty page)",
			Refs:     map[string]RoleRef{},
			Stats:    SnapshotStats{Lines: 1, Chars: 12},
		}
	}

	refs := make(map[string]RoleRef)
	tracker := newRoleNameTracker()
	refCounter := 0
	nextRef := func() string {
		refCounter++
		return fmt.Sprintf("e%d", refCounter)
	}

	var lines []string
	interactiveCount := 0

	for _, tn := range treeNodes {
		role := strings.ToLower(axValue(tn.node.Role))
		name := axValue(tn.node.Name)
		value := axValue(tn.node.Value)
		description := axValue(tn.node.Description)

		// Skip empty/invisible roles
		if role == "" || role == "none" || role == "unknown" {
			if name == "" {
				continue
			}
		}

		// Skip low-value leaf roles that clutter the tree.
		// statictext and inlinetextbox are internal text representation nodes.
		if role == "statictext" || role == "inlinetextbox" {
			continue
		}

		// Apply depth filter
		if opts.MaxDepth > 0 && tn.depth > opts.MaxDepth {
			continue
		}

		isInteractive := IsInteractive(role)
		isContent := IsContent(role)
		isStruct := IsStructural(role)

		// Interactive-only mode: skip non-interactive
		if opts.Interactive && !isInteractive {
			continue
		}

		// Compact mode: skip unnamed structural elements
		if opts.Compact && isStruct && name == "" {
			continue
		}

		// Build the line
		indent := strings.Repeat("  ", tn.depth)
		line := indent + "- " + role

		if name != "" {
			line += fmt.Sprintf(" %q", name)
		}

		// Determine if this element should get a ref
		shouldHaveRef := isInteractive || (isContent && name != "")
		if shouldHaveRef {
			ref := nextRef()
			nth := tracker.getNextIndex(role, name)
			tracker.trackRef(role, name, ref)

			backendNodeID := int(tn.node.BackendDOMNodeID)

			refs[ref] = RoleRef{
				Role:          role,
				Name:          name,
				Nth:           nth,
				BackendNodeID: backendNodeID,
			}

			line += fmt.Sprintf(" [ref=%s]", ref)
			if nth > 0 {
				line += fmt.Sprintf(" [nth=%d]", nth)
			}

			if isInteractive {
				interactiveCount++
			}
		}

		// Append value/description if present
		if value != "" {
			line += fmt.Sprintf(": %q", value)
		}
		if description != "" {
			line += fmt.Sprintf(" (%s)", description)
		}

		lines = append(lines, line)
	}

	// Remove nth from non-duplicate refs
	removeNthFromNonDuplicates(refs, tracker)

	snapshot := strings.Join(lines, "\n")
	if len(lines) == 0 {
		snapshot = "(empty page)"
	}

	// Compact mode: remove structural lines that have no ref descendants
	if opts.Compact && len(lines) > 0 {
		snapshot = compactTree(snapshot)
	}

	// Truncate if needed
	truncated := false
	if opts.MaxChars > 0 && len(snapshot) > opts.MaxChars {
		snapshot = snapshot[:opts.MaxChars] + "\n[...TRUNCATED]"
		truncated = true
	}

	return &SnapshotResult{
		Snapshot:  snapshot,
		Refs:      refs,
		Truncated: truncated,
		Stats: SnapshotStats{
			Lines:       len(lines),
			Chars:       len(snapshot),
			Refs:        len(refs),
			Interactive: interactiveCount,
		},
	}
}

// compactTree removes structural lines that have no ref descendants.
// Ported from TS pw-role-snapshot.ts:172-205.
func compactTree(tree string) string {
	lines := strings.Split(tree, "\n")
	var result []string

	for i, line := range lines {
		// Keep lines with refs
		if strings.Contains(line, "[ref=") {
			result = append(result, line)
			continue
		}
		// Keep lines with values (colon not at end)
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, ":") && !strings.HasSuffix(trimmed, ":") {
			result = append(result, line)
			continue
		}

		// Check if any descendant has a ref
		currentIndent := getIndentLevel(line)
		hasRefDescendant := false
		for j := i + 1; j < len(lines); j++ {
			childIndent := getIndentLevel(lines[j])
			if childIndent <= currentIndent {
				break
			}
			if strings.Contains(lines[j], "[ref=") {
				hasRefDescendant = true
				break
			}
		}
		if hasRefDescendant {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// getIndentLevel returns the indentation level (number of 2-space indents).
func getIndentLevel(line string) int {
	spaces := 0
	for _, c := range line {
		if c == ' ' {
			spaces++
		} else {
			break
		}
	}
	return spaces / 2
}
