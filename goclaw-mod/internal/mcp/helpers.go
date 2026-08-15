package mcp

import "encoding/json"

// jsonUnmarshal is a thin wrapper to keep the main files import-clean.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
