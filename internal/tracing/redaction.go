package tracing

import (
	"context"
	"encoding/json"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type redactorContextKey struct{}

// TextRedactor removes runtime-only values before they cross a trace, event, or
// persistence boundary. It must be deterministic and safe to call repeatedly.
type TextRedactor func(string) string

// WithTextRedactor adds a redactor while preserving any boundary installed by
// an outer run.
func WithTextRedactor(ctx context.Context, redactor TextRedactor) context.Context {
	if redactor == nil {
		return ctx
	}
	if outer := textRedactorFromContext(ctx); outer != nil {
		inner := redactor
		redactor = func(value string) string {
			return inner(outer(value))
		}
	}
	return context.WithValue(ctx, redactorContextKey{}, redactor)
}

func textRedactorFromContext(ctx context.Context) TextRedactor {
	if ctx == nil {
		return nil
	}
	redactor, _ := ctx.Value(redactorContextKey{}).(TextRedactor)
	return redactor
}

func RedactText(ctx context.Context, value string) string {
	return RedactTextWith(textRedactorFromContext(ctx), value)
}

func RedactTextWith(redactor TextRedactor, value string) string {
	if redactor == nil || value == "" {
		return value
	}
	return redactor(value)
}

// RedactValue preserves the common payload shapes used by agent events and
// trace metadata while redacting every contained string.
func RedactValue(ctx context.Context, value any) any {
	return RedactValueWith(textRedactorFromContext(ctx), value)
}

func RedactValueWith(redactor TextRedactor, value any) any {
	if redactor == nil || value == nil {
		return value
	}
	switch typed := value.(type) {
	case string:
		return redactor(typed)
	case json.RawMessage:
		return json.RawMessage(redactor(string(typed)))
	case []byte:
		return []byte(redactor(string(typed)))
	case map[string]string:
		copyValue := make(map[string]string, len(typed))
		for key, item := range typed {
			copyValue[key] = redactor(item)
		}
		return copyValue
	case map[string]any:
		copyValue := make(map[string]any, len(typed))
		for key, item := range typed {
			copyValue[key] = RedactValueWith(redactor, item)
		}
		return copyValue
	case []string:
		copyValue := make([]string, len(typed))
		for i, item := range typed {
			copyValue[i] = redactor(item)
		}
		return copyValue
	case []any:
		copyValue := make([]any, len(typed))
		for i, item := range typed {
			copyValue[i] = RedactValueWith(redactor, item)
		}
		return copyValue
	default:
		return value
	}
}

func RedactSpan(ctx context.Context, span store.SpanData) store.SpanData {
	redactor := textRedactorFromContext(ctx)
	if redactor == nil {
		return span
	}
	span.Name = redactor(span.Name)
	span.Error = redactor(span.Error)
	span.InputPreview = redactor(span.InputPreview)
	span.OutputPreview = redactor(span.OutputPreview)
	if len(span.Metadata) > 0 {
		span.Metadata = json.RawMessage(redactor(string(span.Metadata)))
	}
	return span
}

func RedactSpanUpdates(ctx context.Context, updates map[string]any) map[string]any {
	redacted, _ := RedactValue(ctx, updates).(map[string]any)
	return redacted
}
