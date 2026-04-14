//go:build integration

package ws_methods

import "testing"

// CONTRACT: skills.list response MUST include skills array.
func TestContract_WS_SkillsList(t *testing.T) {
	wsURL, _ := getTestServer(t)
	client, _ := connect(t, wsURL, nil)

	resp := client.send(t, "skills.list", map[string]any{})

	// Response must have skills array
	assertField(t, resp, "skills", "array")

	// If skills exist, verify each has required fields
	skills, ok := resp["skills"].([]any)
	if !ok || len(skills) == 0 {
		t.Log("No skills returned - skipping field checks")
		return
	}

	skill, ok := skills[0].(map[string]any)
	if !ok {
		t.Error("CONTRACT VIOLATION: skills[0] is not an object")
		return
	}

	// Required skill fields
	assertField(t, skill, "name", "string")
	assertField(t, skill, "description", "string")
}
