package googlechat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// resolveServiceAccountJSON returns SA JSON bytes from inline content.
func resolveServiceAccountJSON(cfg config.GoogleChatConfig) ([]byte, error) {
	s := strings.TrimSpace(cfg.ServiceAccountJSON)
	if s == "" {
		return nil, fmt.Errorf("service_account_json is required")
	}
	data := []byte(s)
	if !json.Valid(data) {
		return nil, fmt.Errorf("service_account_json is not valid JSON")
	}
	return data, nil
}
