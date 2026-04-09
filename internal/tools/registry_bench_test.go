package tools

import (
	"context"
	"fmt"
	"testing"
)

// MockTool implements Tool interface for benchmarking.
type MockTool struct {
	name        string
	description string
	params      map[string]any
}

func (m *MockTool) Name() string {
	return m.name
}

func (m *MockTool) Description() string {
	return m.description
}

func (m *MockTool) Parameters() map[string]any {
	return m.params
}

func (m *MockTool) Execute(ctx context.Context, args map[string]any) *Result {
	return &Result{ForLLM: "ok"}
}

// BenchmarkRegistry_Get_50Tools benchmarks tool lookup in registry with 50 tools.
func BenchmarkRegistry_Get_50Tools(b *testing.B) {
	reg := NewRegistry()
	for i := 0; i < 50; i++ {
		tool := &MockTool{
			name:        fmt.Sprintf("tool_%02d", i),
			description: "Mock tool for benchmarking",
			params: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"param1": map[string]string{"type": "string"},
					"param2": map[string]string{"type": "number"},
				},
			},
		}
		reg.Register(tool)
	}

	lookupName := "tool_25"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reg.Get(lookupName)
	}
}

// BenchmarkRegistry_Get_100Tools benchmarks tool lookup in registry with 100 tools.
func BenchmarkRegistry_Get_100Tools(b *testing.B) {
	reg := NewRegistry()
	for i := 0; i < 100; i++ {
		tool := &MockTool{
			name:        fmt.Sprintf("tool_%03d", i),
			description: "Mock tool for benchmarking",
			params: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"param1": map[string]string{"type": "string"},
					"param2": map[string]string{"type": "number"},
				},
			},
		}
		reg.Register(tool)
	}

	lookupName := "tool_050"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reg.Get(lookupName)
	}
}

// BenchmarkRegistry_List_50Tools benchmarks listing tools from registry with 50 tools.
func BenchmarkRegistry_List_50Tools(b *testing.B) {
	reg := NewRegistry()
	for i := 0; i < 50; i++ {
		tool := &MockTool{
			name:        fmt.Sprintf("tool_%02d", i),
			description: "Mock tool for benchmarking",
		}
		reg.Register(tool)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reg.List()
	}
}

// BenchmarkRegistry_List_100Tools benchmarks listing tools from registry with 100 tools.
func BenchmarkRegistry_List_100Tools(b *testing.B) {
	reg := NewRegistry()
	for i := 0; i < 100; i++ {
		tool := &MockTool{
			name:        fmt.Sprintf("tool_%03d", i),
			description: "Mock tool for benchmarking",
		}
		reg.Register(tool)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reg.List()
	}
}

// BenchmarkRegistry_ProviderDefs_50Tools benchmarks generating provider definitions for 50 tools.
func BenchmarkRegistry_ProviderDefs_50Tools(b *testing.B) {
	reg := NewRegistry()
	for i := 0; i < 50; i++ {
		tool := &MockTool{
			name:        fmt.Sprintf("tool_%02d", i),
			description: "Mock tool for benchmarking",
			params: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"param1": map[string]string{"type": "string"},
				},
			},
		}
		reg.Register(tool)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reg.ProviderDefs()
	}
}

// BenchmarkRegistry_Alias_50Tools benchmarks tool lookup with aliases in registry.
func BenchmarkRegistry_Alias_50Tools(b *testing.B) {
	reg := NewRegistry()
	for i := 0; i < 50; i++ {
		tool := &MockTool{
			name:        fmt.Sprintf("tool_%02d", i),
			description: "Mock tool for benchmarking",
		}
		reg.Register(tool)
		// Register aliases for each tool
		reg.RegisterAlias(fmt.Sprintf("alias_%02d", i), fmt.Sprintf("tool_%02d", i))
	}

	lookupAlias := "alias_25"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reg.Get(lookupAlias)
	}
}

// BenchmarkRegistry_Disable_Enable benchmarks disabling and enabling tools.
func BenchmarkRegistry_Disable_Enable(b *testing.B) {
	reg := NewRegistry()
	for i := 0; i < 50; i++ {
		tool := &MockTool{
			name:        fmt.Sprintf("tool_%02d", i),
			description: "Mock tool for benchmarking",
		}
		reg.Register(tool)
	}

	toolName := "tool_25"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reg.Disable(toolName)
		reg.Enable(toolName)
	}
}

// BenchmarkRegistry_Count benchmarks counting tools in registry.
func BenchmarkRegistry_Count(b *testing.B) {
	reg := NewRegistry()
	for i := 0; i < 50; i++ {
		tool := &MockTool{
			name:        fmt.Sprintf("tool_%02d", i),
			description: "Mock tool for benchmarking",
		}
		reg.Register(tool)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reg.Count()
	}
}

// BenchmarkRegistry_Resolve_WithDisabled benchmarks resolving tools with some disabled.
func BenchmarkRegistry_Resolve_WithDisabled(b *testing.B) {
	reg := NewRegistry()
	for i := 0; i < 50; i++ {
		tool := &MockTool{
			name:        fmt.Sprintf("tool_%02d", i),
			description: "Mock tool for benchmarking",
		}
		reg.Register(tool)
	}

	// Disable half the tools
	for i := 0; i < 25; i++ {
		reg.Disable(fmt.Sprintf("tool_%02d", i))
	}

	lookupName := "tool_40"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reg.Get(lookupName)
	}
}
