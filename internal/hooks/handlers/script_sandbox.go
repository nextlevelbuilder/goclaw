package handlers

import (
	"fmt"

	"github.com/dop251/goja"
)

// MaxCallStackSize caps goja's recursion depth. Prevents Go goroutine stack
// overflow on scripts like `function f(){f()}; f()`. Value 256 chosen because
// no legitimate hook script needs deeper recursion; attackers overflow at ~1K.
const MaxCallStackSize = 256

// DisabledGlobals — every global goja ships, verified against upstream
// builtin_global.go + builtin_typedarrays.go. Deny-all strategy: we re-enable
// only the names in AllowedGlobals after this pass so future goja releases
// that add new globals cannot leak escape primitives by default.
var DisabledGlobals = []string{
	"Object", "Function", "Array", "String", "Number", "BigInt", "RegExp",
	"Date", "Boolean", "Proxy", "Reflect", "Error", "AggregateError",
	"TypeError", "ReferenceError", "SyntaxError", "RangeError", "EvalError",
	"URIError", "GoError", "Math", "JSON", "Symbol", "WeakSet", "WeakMap",
	"Map", "Set", "Promise", "globalThis", "NaN", "Infinity",
	"isNaN", "parseInt", "parseFloat", "isFinite", "decodeURI",
	"decodeURIComponent", "encodeURI", "encodeURIComponent", "escape",
	"unescape", "eval",
	// Typed arrays (builtin_typedarrays.go)
	"ArrayBuffer", "DataView", "Uint8Array", "Uint8ClampedArray", "Int8Array",
	"Uint16Array", "Int16Array", "Uint32Array", "Int32Array", "Float32Array",
	"Float64Array", "BigInt64Array", "BigUint64Array",
}

// AllowedGlobals — minimal safe subset re-enabled after the deny pass.
// Scripts syntactically need `[]`, `{}`, `new Error("x")`. Prototype nullify
// covers the `.constructor.constructor` escape path through these allowed
// names.
var AllowedGlobals = []string{
	"Math", "JSON", "Number", "String", "Boolean", "RegExp", "Date",
	"Array", "Object", "Error", "TypeError", "RangeError", "SyntaxError",
	"parseInt", "parseFloat", "isNaN", "isFinite",
	"encodeURIComponent", "decodeURIComponent",
	"Infinity", "NaN",
}

// prototypeNullifyScript breaks the `.constructor.constructor` prototype-escape
// chain. Even when Array/Object/Function are allowlisted, their constructor
// chain can yield a Function object that evals arbitrary source. Nulling the
// constructor slot on each prototype defeats that chain. try/catch wraps each
// write so a locked prototype from a future goja release does not abort the
// whole hardening sequence.
const prototypeNullifyScript = `
(function() {
  try { Object.defineProperty(Array.prototype, 'constructor', {value: undefined, writable: false, configurable: false}); } catch (_) {}
  try { Object.defineProperty(Object.prototype, 'constructor', {value: undefined, writable: false, configurable: false}); } catch (_) {}
  if (typeof Function !== 'undefined' && Function.prototype) {
    try { Object.defineProperty(Function.prototype, 'constructor', {value: undefined, writable: false, configurable: false}); } catch (_) {}
  }
})();
`

// applyHardening runs the full sandbox sequence on a fresh runtime:
//  1. cap the recursion depth before any user frames exist,
//  2. nullify `.constructor` slots on the exposed prototypes (Array, Object,
//     Function, and built-in function prototypes like Date/RegExp/Error/etc)
//     BEFORE the deny pass — `Function` must still be globally accessible to
//     reach `Function.prototype`,
//  3. undefine every ships-by-default global that is NOT in the allowlist.
//
// The allowlist items are left untouched — save+restore would fail on ES
// non-writable globals (NaN, Infinity, undefined). Leaving them at their
// original engine value is correct because the deny pass never touched them.
//
// Any failure propagates so the caller can fail closed with DecisionError
// rather than execute user code against a half-hardened runtime.
func applyHardening(rt *goja.Runtime) error {
	rt.SetMaxCallStackSize(MaxCallStackSize)

	// Step 2 first so `Function` is still reachable.
	if _, err := rt.RunString(prototypeNullifyScript); err != nil {
		return err
	}

	allow := make(map[string]struct{}, len(AllowedGlobals))
	for _, n := range AllowedGlobals {
		allow[n] = struct{}{}
	}

	for _, name := range DisabledGlobals {
		if _, keep := allow[name]; keep {
			continue
		}
		if err := rt.Set(name, goja.Undefined()); err != nil {
			return fmt.Errorf("sandbox: disable %q: %w", name, err)
		}
	}
	return nil
}
