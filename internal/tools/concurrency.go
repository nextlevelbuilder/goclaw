// Package tools — Per-invocation concurrency classification (CP-02).
// Tools implement ConcurrencyClassifier to declare whether a specific
// invocation (with specific args) is safe for parallel execution.
package tools

// ConcurrencyClassifier can be implemented by tools that support
// per-invocation concurrency classification.
//
// If a tool does not implement this interface, it defaults to EXCLUSIVE
// (safe-by-default means conservative — no parallel execution).
type ConcurrencyClassifier interface {
	// IsConcurrencySafe returns true if this specific invocation can
	// safely run in parallel with other concurrent-safe tools.
	IsConcurrencySafe(args map[string]any) bool
}

// IsConcurrencySafeForTool checks if a tool invocation is safe for
// concurrent execution. Handles tools that don't implement the interface.
//
// Safety contract:
//   - nil tool → false
//   - tool doesn't implement ConcurrencyClassifier → check static metadata
//   - IsConcurrencySafe panics → false (recovered)
//   - input parse failure → false
func IsConcurrencySafeForTool(name string, meta ToolMetadata, classifier ConcurrencyClassifier, args map[string]any) bool {
	// If tool implements per-invocation classifier, use it
	if classifier != nil {
		safe := false
		func() {
			defer func() {
				if r := recover(); r != nil {
					safe = false // panic → exclusive
				}
			}()
			safe = classifier.IsConcurrencySafe(args)
		}()
		return safe
	}

	// Fallback: static metadata
	return meta.IsReadOnly()
}
