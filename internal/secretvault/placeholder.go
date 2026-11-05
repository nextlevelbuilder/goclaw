package secretvault

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const placeholderOpen = "{{secret:"

var placeholderRe = regexp.MustCompile(`\{\{secret:([a-z0-9][a-z0-9_-]{0,63})\}\}`)

// ArgOptions constrains how a tool lets placeholders be resolved.
type ArgOptions struct {
	// Kind is the secret kind this tool accepts (store.SecretKindAPIKey, ...).
	Kind string
	// Host is the destination hostname the secret will be sent to. The tool
	// derives it from a field that itself must not contain a placeholder.
	Host string
	// Allow reports whether a placeholder may appear at a path such as
	// "headers.Authorization" or "body.token". Nil allows nothing.
	Allow func(path string) bool
}

// ContainsPlaceholder reports whether any string in v holds a placeholder
// opener. It is used by tools to decide whether vault handling is needed.
func ContainsPlaceholder(v any) bool {
	switch t := v.(type) {
	case string:
		return strings.Contains(t, placeholderOpen)
	case map[string]any:
		for k, item := range t {
			if strings.Contains(k, placeholderOpen) || ContainsPlaceholder(item) {
				return true
			}
		}
	case map[string]string:
		for k, item := range t {
			if strings.Contains(k, placeholderOpen) || strings.Contains(item, placeholderOpen) {
				return true
			}
		}
	case []any:
		for _, item := range t {
			if ContainsPlaceholder(item) {
				return true
			}
		}
	case []string:
		for _, item := range t {
			if strings.Contains(item, placeholderOpen) {
				return true
			}
		}
	}
	return false
}

// ResolveArgs returns a deep copy of args with placeholders replaced by secret
// values. The input map is never modified, so events, traces and session
// history keep the placeholder form. When args hold no placeholder it returns
// args unchanged. A placeholder without a vault runtime in ctx is an error: an
// unresolved placeholder must never be sent onward by a supporting tool.
func ResolveArgs(ctx context.Context, args map[string]any, opt ArgOptions) (map[string]any, error) {
	if !ContainsPlaceholder(args) {
		return args, nil
	}
	rt := RuntimeFrom(ctx)
	if rt == nil {
		return nil, ErrNoRuntime
	}
	w := &walker{ctx: ctx, rt: rt, opt: opt, cache: map[string]string{}}
	out, err := w.walk("", args)
	if err != nil {
		return nil, err
	}
	return out.(map[string]any), nil
}

type walker struct {
	ctx   context.Context
	rt    *Runtime
	opt   ArgOptions
	cache map[string]string // name -> value, so each secret is used/audited once per call
}

func (w *walker) walk(path string, v any) (any, error) {
	switch t := v.(type) {
	case string:
		return w.str(path, t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			if strings.Contains(k, placeholderOpen) {
				return nil, fmt.Errorf("%w: map key", ErrPlaceholderLocation)
			}
			r, err := w.walk(join(path, k), item)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case map[string]string:
		out := make(map[string]any, len(t))
		for k, item := range t {
			if strings.Contains(k, placeholderOpen) {
				return nil, fmt.Errorf("%w: map key", ErrPlaceholderLocation)
			}
			r, err := w.str(join(path, k), item)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			r, err := w.walk(path+"["+strconv.Itoa(i)+"]", item)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	case []string:
		out := make([]any, len(t))
		for i, item := range t {
			r, err := w.str(path+"["+strconv.Itoa(i)+"]", item)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	default:
		return v, nil
	}
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func (w *walker) str(path, s string) (any, error) {
	if !strings.Contains(s, placeholderOpen) {
		return s, nil
	}
	matches := placeholderRe.FindAllStringSubmatchIndex(s, -1)
	if len(matches) != strings.Count(s, placeholderOpen) {
		return nil, fmt.Errorf("%w at %s", ErrInvalidPlaceholder, path)
	}
	if w.opt.Allow == nil || !w.opt.Allow(path) {
		return nil, fmt.Errorf("%w: %s", ErrPlaceholderLocation, path)
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		name := s[m[2]:m[3]]
		val, ok := w.cache[name]
		if !ok {
			sec, err := w.rt.Use(w.ctx, name, w.opt.Kind, w.opt.Host)
			if err != nil {
				return nil, err
			}
			val = sec.Payload.Secret
			w.cache[name] = val
		}
		b.WriteString(s[last:m[0]])
		b.WriteString(val)
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String(), nil
}
