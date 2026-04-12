package http

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpec_AgentsContractMatchesHTTPHandlers(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal(openapiSpec, &spec); err != nil {
		t.Fatalf("decode embedded openapi spec: %v", err)
	}

	components := spec["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	agentInput := schemas["AgentInput"].(map[string]any)
	props := agentInput["properties"].(map[string]any)

	if _, ok := props["agent_key"]; !ok {
		t.Fatal("AgentInput must document agent_key")
	}
	if _, ok := props["name"]; ok {
		t.Fatal("AgentInput should not document deprecated name field")
	}

	paths := spec["paths"].(map[string]any)
	agentsPath := paths["/v1/agents"].(map[string]any)
	getOp := agentsPath["get"].(map[string]any)
	responses := getOp["responses"].(map[string]any)
	ok200 := responses["200"].(map[string]any)
	content := ok200["content"].(map[string]any)
	appJSON := content["application/json"].(map[string]any)
	schema := appJSON["schema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	agentsField := properties["agents"].(map[string]any)

	if agentsField["type"] != "array" {
		t.Fatalf("GET /v1/agents should document agents as array, got %#v", agentsField["type"])
	}
}
