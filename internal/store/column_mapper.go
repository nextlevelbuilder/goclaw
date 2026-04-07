package store

// CamelToSnake converts a camelCase string to snake_case for sqlx column mapping.
// Already-snake_case strings pass through unchanged.
// Examples: "agentId" → "agent_id", "parent_trace_id" → "parent_trace_id", "ID" → "id"
func CamelToSnake(s string) string {
	var result []byte
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 && s[i-1] >= 'a' && s[i-1] <= 'z' {
				result = append(result, '_')
			}
			result = append(result, byte(r+32)) // toLower
		} else {
			result = append(result, byte(r))
		}
	}
	return string(result)
}
