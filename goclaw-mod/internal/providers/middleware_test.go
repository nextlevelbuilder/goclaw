package providers

import (
	"testing"
)

func TestComposeMiddlewaresNilEntries(t *testing.T) {
	// Test: ComposeMiddlewares with all nil entries returns nil
	result := ComposeMiddlewares(nil, nil, nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestComposeMiddlewaresSingleMiddleware(t *testing.T) {
	// Test: ComposeMiddlewares with one non-nil middleware returns composed version
	mw := func(body map[string]any, cfg MiddlewareConfig) map[string]any {
		body["test"] = "value"
		return body
	}

	composed := ComposeMiddlewares(mw)
	if composed == nil {
		t.Fatal("expected non-nil composed middleware")
	}

	body := map[string]any{}
	cfg := MiddlewareConfig{Provider: "test"}
	result := composed(body, cfg)

	if result["test"] != "value" {
		t.Errorf("expected test=value, got %v", result)
	}
}

func TestComposeMiddlewaresMultipleMiddlewaresLeftToRight(t *testing.T) {
	// Test: ComposeMiddlewares applies middlewares left-to-right (ordering)
	mw1 := func(body map[string]any, cfg MiddlewareConfig) map[string]any {
		body["order"] = append(body["order"].([]string), "mw1")
		return body
	}
	mw2 := func(body map[string]any, cfg MiddlewareConfig) map[string]any {
		body["order"] = append(body["order"].([]string), "mw2")
		return body
	}
	mw3 := func(body map[string]any, cfg MiddlewareConfig) map[string]any {
		body["order"] = append(body["order"].([]string), "mw3")
		return body
	}

	composed := ComposeMiddlewares(mw1, mw2, mw3)
	if composed == nil {
		t.Fatal("expected non-nil composed middleware")
	}

	body := map[string]any{"order": []string{}}
	cfg := MiddlewareConfig{}
	result := composed(body, cfg)

	order := result["order"].([]string)
	expected := []string{"mw1", "mw2", "mw3"}
	if len(order) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("expected order[%d]=%s, got %s", i, v, order[i])
		}
	}
}

func TestComposeMiddlewaresSkipsNilEntries(t *testing.T) {
	// Test: ComposeMiddlewares skips nil entries
	mw1 := func(body map[string]any, cfg MiddlewareConfig) map[string]any {
		body["a"] = 1
		return body
	}
	mw2 := func(body map[string]any, cfg MiddlewareConfig) map[string]any {
		body["b"] = 2
		return body
	}

	composed := ComposeMiddlewares(mw1, nil, mw2, nil)
	if composed == nil {
		t.Fatal("expected non-nil composed middleware")
	}

	body := map[string]any{}
	cfg := MiddlewareConfig{}
	result := composed(body, cfg)

	if result["a"] != 1 || result["b"] != 2 {
		t.Errorf("expected a=1, b=2, got %v", result)
	}
}

func TestApplyMiddlewaresNil(t *testing.T) {
	// Test: ApplyMiddlewares with nil middleware returns body unchanged
	body := map[string]any{"key": "value"}
	cfg := MiddlewareConfig{}

	result := ApplyMiddlewares(body, nil, cfg)

	if result["key"] != "value" {
		t.Errorf("expected key=value, got %v", result)
	}
}

func TestApplyMiddlewaresNotNil(t *testing.T) {
	// Test: ApplyMiddlewares applies middleware
	mw := func(body map[string]any, cfg MiddlewareConfig) map[string]any {
		body["modified"] = true
		return body
	}

	body := map[string]any{}
	cfg := MiddlewareConfig{Model: "test-model", Provider: "openai"}

	result := ApplyMiddlewares(body, mw, cfg)

	if result["modified"] != true {
		t.Errorf("expected modified=true, got %v", result)
	}
}

func TestMiddlewareConfigFields(t *testing.T) {
	// Test: MiddlewareConfig fields are correctly set
	caps := ProviderCapabilities{
		Streaming: true,
	}
	options := map[string]any{"temperature": 0.5}

	cfg := MiddlewareConfig{
		Provider: "anthropic",
		Model:    "claude-opus-4-6",
		Caps:     caps,
		AuthType: "api_key",
		APIBase:  "https://api.anthropic.com",
		Options:  options,
	}

	if cfg.Provider != "anthropic" {
		t.Errorf("expected provider=anthropic, got %s", cfg.Provider)
	}
	if cfg.Model != "claude-opus-4-6" {
		t.Errorf("expected model=claude-opus-4-6, got %s", cfg.Model)
	}
	if !cfg.Caps.Streaming {
		t.Error("expected Streaming=true")
	}
	if cfg.AuthType != "api_key" {
		t.Errorf("expected authType=api_key, got %s", cfg.AuthType)
	}
	if cfg.APIBase != "https://api.anthropic.com" {
		t.Errorf("expected APIBase=https://api.anthropic.com, got %s", cfg.APIBase)
	}
	if cfg.Options["temperature"] != 0.5 {
		t.Errorf("expected temperature=0.5, got %v", cfg.Options["temperature"])
	}
}

func TestComposeMiddlewaresModifiesBodyCorrectly(t *testing.T) {
	// Test: Middlewares can modify the body in sequence
	mw1 := func(body map[string]any, cfg MiddlewareConfig) map[string]any {
		body["count"] = 1
		return body
	}
	mw2 := func(body map[string]any, cfg MiddlewareConfig) map[string]any {
		count := body["count"].(int)
		body["count"] = count + 1
		return body
	}

	composed := ComposeMiddlewares(mw1, mw2)
	body := map[string]any{}
	cfg := MiddlewareConfig{}

	result := composed(body, cfg)

	if result["count"] != 2 {
		t.Errorf("expected count=2, got %v", result["count"])
	}
}

func TestMiddlewareAccessesConfig(t *testing.T) {
	// Test: Middleware can access and use config fields
	mw := func(body map[string]any, cfg MiddlewareConfig) map[string]any {
		body["provider_from_config"] = cfg.Provider
		body["model_from_config"] = cfg.Model
		return body
	}

	body := map[string]any{}
	cfg := MiddlewareConfig{
		Provider: "openai",
		Model:    "gpt-4o",
	}

	result := ApplyMiddlewares(body, mw, cfg)

	if result["provider_from_config"] != "openai" {
		t.Errorf("expected openai, got %v", result["provider_from_config"])
	}
	if result["model_from_config"] != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %v", result["model_from_config"])
	}
}
