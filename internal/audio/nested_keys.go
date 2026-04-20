package audio

// SetNested sets a value in a map[string]any using a dot-separated key path.
// Intermediate maps are created on demand. If an intermediate path segment
// already holds a non-map value it is replaced with a new map.
//
// Example:
//
//	SetNested(m, "voice_settings.stability", 0.5)
//	// m = {"voice_settings": {"stability": 0.5}}
func SetNested(m map[string]any, key string, value any) {
	parts, err := parseKeyPath(key)
	if err != nil {
		return // malformed key — silently skip
	}
	cur := m
	for _, part := range parts[:len(parts)-1] {
		next, ok := cur[part].(map[string]any)
		if !ok {
			next = make(map[string]any)
			cur[part] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = value
}

// GetNested retrieves a value from a map[string]any using a dot-separated key
// path. Returns (nil, false) if any segment is missing or non-traversable.
func GetNested(m map[string]any, key string) (any, bool) {
	parts, err := parseKeyPath(key)
	if err != nil {
		return nil, false
	}
	cur := m
	for _, part := range parts[:len(parts)-1] {
		next, ok := cur[part].(map[string]any)
		if !ok {
			return nil, false
		}
		cur = next
	}
	v, ok := cur[parts[len(parts)-1]]
	return v, ok
}
