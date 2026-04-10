package cmd

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func TestSyncWebSearchToolRegistration_RegistersDefaultDDGAndCanDisable(t *testing.T) {
	reg := tools.NewRegistry()
	cfg := config.Default()

	current := syncWebSearchToolRegistration(reg, nil, cfg)
	if current == nil {
		t.Fatal("expected web_search tool")
	}
	if _, ok := reg.Get("web_search"); !ok {
		t.Fatal("web_search tool should be registered")
	}

	cfg.Tools.Web.DuckDuckGo.Enabled = false
	current = syncWebSearchToolRegistration(reg, current, cfg)
	if current != nil {
		t.Fatal("expected web_search tool to be removed")
	}
	if _, ok := reg.Get("web_search"); ok {
		t.Fatal("web_search tool should be unregistered")
	}
}

func TestSyncWebSearchToolRegistration_CreatesToolForConfiguredAPIProvider(t *testing.T) {
	reg := tools.NewRegistry()
	cfg := config.Default()
	cfg.Tools.Web.DuckDuckGo.Enabled = false
	cfg.Tools.Web.Exa.Enabled = true
	cfg.Tools.Web.Exa.APIKey = "exa-secret"

	current := syncWebSearchToolRegistration(reg, nil, cfg)
	if current == nil {
		t.Fatal("expected web_search tool")
	}
	if _, ok := reg.Get("web_search"); !ok {
		t.Fatal("web_search tool should be registered")
	}
}
