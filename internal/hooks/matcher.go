package hooks

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/google/cel-go/cel"
)

// ───── Regex matcher ─────

// regexCache stores compiled regex patterns keyed by the pattern string.
// Pattern content is the cache key — hook version bumps bypass the cache
// only when the pattern content actually changes (L5 mitigation).
var regexCache sync.Map

// CompileMatcher compiles pattern into a cached *regexp.Regexp.
// Empty pattern returns (nil, nil); callers treat nil as match-all.
// Malformed patterns return a typed error (never panic).
func CompileMatcher(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	if v, ok := regexCache.Load(pattern); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile matcher %q: %w", pattern, err)
	}
	regexCache.Store(pattern, re)
	return re, nil
}

// MatchToolName returns true if re matches toolName. Nil regex matches all.
func MatchToolName(re *regexp.Regexp, toolName string) bool {
	if re == nil {
		return true
	}
	return re.MatchString(toolName)
}

// ───── CEL evaluator ─────

// celCostLimit caps evaluation cost to defeat ReDoS/turing-escape (L5).
const celCostLimit uint64 = 100_000

var (
	celEnvInstance *cel.Env
	celEnvOnce     sync.Once
	celEnvErr      error
	celCache       sync.Map // expr string → cel.Program
)

// buildCELEnv constructs the deliberately-minimal CEL environment.
//
// Exposed variables:
//   - tool_name (string)
//   - tool_input (map<string, dyn>)
//   - depth (int) — sub-agent nesting level; max enforced by dispatcher
//
// Stdlib file/network functions are NOT registered — any reference such as
// readFile()/http.get() fails at compile time (sandbox).
func buildCELEnv() (*cel.Env, error) {
	celEnvOnce.Do(func() {
		celEnvInstance, celEnvErr = cel.NewEnv(
			cel.Variable("tool_name", cel.StringType),
			cel.Variable("tool_input", cel.MapType(cel.StringType, cel.DynType)),
			cel.Variable("depth", cel.IntType),
		)
	})
	return celEnvInstance, celEnvErr
}

// CompileCELExpr parses+type-checks expr and caches the resulting program.
// Empty expr returns (nil, nil); callers treat nil as match-all.
func CompileCELExpr(expr string) (cel.Program, error) {
	if expr == "" {
		return nil, nil
	}
	if v, ok := celCache.Load(expr); ok {
		return v.(cel.Program), nil
	}
	env, err := buildCELEnv()
	if err != nil {
		return nil, fmt.Errorf("cel env: %w", err)
	}
	ast, iss := env.Compile(expr)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("cel parse %q: %w", expr, iss.Err())
	}
	prg, err := env.Program(ast, cel.CostLimit(celCostLimit))
	if err != nil {
		return nil, fmt.Errorf("cel program: %w", err)
	}
	celCache.Store(expr, prg)
	return prg, nil
}

// EvalCEL executes a compiled program against the provided variables.
// Nil program evaluates to true (match-all semantics for empty if_expr).
// Non-bool result is a typed error — defensive against misuse.
func EvalCEL(prg cel.Program, vars map[string]any) (bool, error) {
	if prg == nil {
		return true, nil
	}
	out, _, err := prg.Eval(vars)
	if err != nil {
		return false, fmt.Errorf("cel eval: %w", err)
	}
	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("cel expression must return bool, got %T", out.Value())
	}
	return b, nil
}
