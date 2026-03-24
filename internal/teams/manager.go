package teams

import (
	"encoding/json"
	"os"
)

// Team represents a group of coordinated agents.
type Team struct {
	Name   string   `json:"name"`
	Agents []string `json:"agents"`
}

// ExportTeam saves team configuration to a JSON file.
func ExportTeam(team *Team, filePath string) error {
	data, err := json.MarshalIndent(team, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

// ImportTeam loads team configuration from a JSON file.
func ImportTeam(filePath string) (*Team, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var team Team
	err = json.Unmarshal(data, &team)
	return &team, err
}
