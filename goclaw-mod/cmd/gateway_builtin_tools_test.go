package cmd

import "testing"

func TestBuiltinToolSeedDataIncludesWait(t *testing.T) {
	t.Parallel()
	for _, def := range builtinToolSeedData() {
		if def.Name != "wait" {
			continue
		}
		if def.Category != "runtime" {
			t.Fatalf("wait category = %q, want runtime", def.Category)
		}
		if !def.Enabled {
			t.Fatal("wait should be enabled by default")
		}
		return
	}
	t.Fatal("builtinToolSeedData() missing wait")
}

func TestBuiltinToolSeedDataIncludesMemoryExpand(t *testing.T) {
	t.Parallel()
	for _, def := range builtinToolSeedData() {
		if def.Name != "memory_expand" {
			continue
		}
		if def.Category != "memory" {
			t.Fatalf("memory_expand category = %q, want memory", def.Category)
		}
		if !def.Enabled {
			t.Fatal("memory_expand should be enabled by default")
		}
		if len(def.Requires) != 1 || def.Requires[0] != "memory" {
			t.Fatalf("memory_expand requires = %#v, want [memory]", def.Requires)
		}
		return
	}
	t.Fatal("builtinToolSeedData() missing memory_expand")
}

func TestBuiltinToolSeedDataMemoryCategoryIncludesRecallTools(t *testing.T) {
	t.Parallel()
	got := map[string]bool{}
	for _, def := range builtinToolSeedData() {
		if def.Category == "memory" {
			got[def.Name] = true
		}
	}
	for _, want := range []string{
		"memory_search",
		"memory_get",
		"memory_expand",
		"knowledge_graph_search",
	} {
		if !got[want] {
			t.Fatalf("memory category missing %s; got %#v", want, got)
		}
	}
}

func TestBuiltinToolSeedDataDoesNotIncludeTelegramManager(t *testing.T) {
	t.Parallel()
	for _, def := range builtinToolSeedData() {
		if def.Name == "telegram_manager" {
			t.Fatal("telegram_manager must be configured from Telegram channel settings, not seeded as a visible builtin tool")
		}
	}
}
