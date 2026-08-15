package tracing

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestTextRedactorSanitizesSpanUpdatesAndEventPayloads(t *testing.T) {
	const secret = "/host/tenant/collaboration/delegations/id/outputs"
	ctx := WithTextRedactor(context.Background(), func(value string) string {
		return strings.ReplaceAll(value, secret, "outputs")
	})

	span := RedactSpan(ctx, store.SpanData{
		InputPreview:  `{"path":"` + secret + `/input.txt"}`,
		OutputPreview: secret + "/result.txt",
		Error:         "failed at " + secret,
		Metadata:      json.RawMessage(`{"root":"` + secret + `"}`),
	})
	encoded, err := json.Marshal(span)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("redacted span leaked host root: %s", encoded)
	}

	updates := RedactSpanUpdates(ctx, map[string]any{
		"output_preview": secret + "/result.txt",
		"metadata":       json.RawMessage(`{"root":"` + secret + `"}`),
	})
	payload := RedactValue(ctx, map[string]any{
		"content": secret,
		"items":   []any{secret + "/child"},
	})
	combined, err := json.Marshal([]any{updates, payload})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(combined), secret) {
		t.Fatalf("redacted values leaked host root: %s", combined)
	}
}
