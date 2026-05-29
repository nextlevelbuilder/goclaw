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
