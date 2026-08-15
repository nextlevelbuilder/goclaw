package hooks_test

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// ───── Regex ─────

func TestCompileMatcher_Valid(t *testing.T) {
	re, err := hooks.CompileMatcher("^Write$")
	if err != nil {
		t.Fatalf("CompileMatcher: %v", err)
	}
	if re == nil {
		t.Fatal("expected non-nil regex")
	}
	if !hooks.MatchToolName(re, "Write") {
		t.Error("Write should match")
	}
	if hooks.MatchToolName(re, "Read") {
		t.Error("Read should not match")
	}
}

func TestCompileMatcher_Empty_MatchesAll(t *testing.T) {
	re, err := hooks.CompileMatcher("")
	if err != nil {
		t.Fatalf("CompileMatcher(\"\"): %v", err)
	}
	if re != nil {
		t.Errorf("empty pattern should return nil regex, got %v", re)
	}
	// MatchToolName with nil regex matches everything.
	if !hooks.MatchToolName(nil, "anything") {
		t.Error("nil regex should match all")
	}
}

func TestCompileMatcher_Invalid_ReturnsTypedError(t *testing.T) {
	re, err := hooks.CompileMatcher("[unclosed")
	if err == nil {
		t.Fatal("expected error for malformed regex")
	}
	if re != nil {
		t.Error("expected nil regex on error")
	}
	if !strings.Contains(err.Error(), "compile matcher") {
		t.Errorf("error should mention matcher context; got %q", err)
	}
}

func TestCompileMatcher_CacheReuse(t *testing.T) {
	re1, err1 := hooks.CompileMatcher("^Read$")
	re2, err2 := hooks.CompileMatcher("^Read$")
	if err1 != nil || err2 != nil {
		t.Fatalf("compile err1=%v err2=%v", err1, err2)
	}
	// Cached compile returns the same pointer.
	if re1 != re2 {
		t.Error("expected cache hit to return same regex pointer")
	}
}

// ───── CEL ─────

func TestCompileCELExpr_BoolEval(t *testing.T) {
	prg, err := hooks.CompileCELExpr(`tool_name == "Write"`)
	if err != nil {
		t.Fatalf("CompileCELExpr: %v", err)
	}
	got, err := hooks.EvalCEL(prg, map[string]any{
		"tool_name":  "Write",
		"tool_input": map[string]any{},
		"depth":      int64(0),
	})
	if err != nil {
		t.Fatalf("EvalCEL: %v", err)
	}
	if !got {
		t.Error("expected true for tool_name == \"Write\"")
	}

	got, err = hooks.EvalCEL(prg, map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{},
		"depth":      int64(0),
	})
	if err != nil {
		t.Fatalf("EvalCEL: %v", err)
	}
	if got {
		t.Error("expected false for tool_name != \"Write\"")
	}
}

func TestCompileCELExpr_ToolInputMap(t *testing.T) {
	// Verifies CEL can index into the tool_input map and compare strings.
	prg, err := hooks.CompileCELExpr(`tool_input["path"] == "/etc/passwd"`)
	if err != nil {
		t.Fatalf("CompileCELExpr: %v", err)
	}
	got, err := hooks.EvalCEL(prg, map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"path": "/etc/passwd"},
		"depth":      int64(0),
	})
	if err != nil {
		t.Fatalf("EvalCEL: %v", err)
	}
	if !got {
		t.Error("expected true for matching path")
	}
}

func TestCompileCELExpr_Empty_ReturnsNilProgram(t *testing.T) {
	prg, err := hooks.CompileCELExpr("")
	if err != nil {
		t.Fatalf("CompileCELExpr(\"\"): %v", err)
	}
	if prg != nil {
		t.Error("empty expr should return nil program")
	}
	// Nil program evaluates to true (match-all).
	got, err := hooks.EvalCEL(nil, nil)
	if err != nil {
		t.Errorf("EvalCEL(nil) should not error; got %v", err)
	}
	if !got {
		t.Error("nil program should evaluate true (match-all)")
	}
}

func TestCompileCELExpr_ParseError_ReturnsTypedError(t *testing.T) {
	// Defensive: malformed CEL must not panic — must return error.
	prg, err := hooks.CompileCELExpr(`tool_name ===== "bad"`)
	if err == nil {
		t.Fatal("expected error for malformed CEL")
	}
	if prg != nil {
		t.Error("expected nil program on parse error")
	}
}

func TestCompileCELExpr_Sandbox_NoFileAccess(t *testing.T) {
	// The CEL env exposes only tool_name, tool_input, depth. Any reference
	// to stdlib file/network functions must fail at compile time.
	_, err := hooks.CompileCELExpr(`readFile("/etc/passwd") == "root"`)
	if err == nil {
		t.Error("CEL sandbox should reject readFile() — undeclared function")
	}
	_, err = hooks.CompileCELExpr(`http.get("https://evil.example") == "x"`)
	if err == nil {
		t.Error("CEL sandbox should reject http.get() — undeclared")
	}
}

func TestCompileCELExpr_NonBoolResult_ReturnsError(t *testing.T) {
	// Expression must yield bool; an integer result is a typed error.
	prg, err := hooks.CompileCELExpr(`1 + 2`)
	if err != nil {
		t.Fatalf("CompileCELExpr: %v", err)
	}
	_, err = hooks.EvalCEL(prg, map[string]any{
		"tool_name":  "x",
		"tool_input": map[string]any{},
		"depth":      int64(0),
	})
	if err == nil {
		t.Error("expected error for non-bool CEL result")
	}
}

func TestCompileCELExpr_CacheReuse(t *testing.T) {
	p1, err1 := hooks.CompileCELExpr(`depth < 3`)
	p2, err2 := hooks.CompileCELExpr(`depth < 3`)
	if err1 != nil || err2 != nil {
		t.Fatalf("compile err1=%v err2=%v", err1, err2)
	}
	// Cache hit should return the same cel.Program.
	if p1 != p2 {
		t.Error("expected cache hit to return same CEL program")
	}
}
