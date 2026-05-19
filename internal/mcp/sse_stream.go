package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// errSSETerminal is a sentinel error a SSEHandler returns indirectly (via
// terminate=true) so PostSSEStream knows to stop reading the stream cleanly.
var errSSETerminal = errors.New("sse terminal event")

// SSEHandler processes one decoded SSE event.
//
//	terminate=true tells PostSSEStream to stop reading the stream and return.
//	err != nil aborts the stream with that error.
//
// Returning (false, nil) means "continue reading the stream".
type SSEHandler func(ctx context.Context, evt sseEvent, sequence int) (terminate bool, err error)

// PostSSEStream POSTs body as JSON to url with Accept: text/event-stream and
// dispatches every decoded event to handler. It is the protocol-level core
// that all SSE-based sub-agent backends share — it knows nothing about the
// business semantics of a particular agent (event names, terminal set,
// progress mapping). Callers attach those concerns inside handler.
//
// toolName is used only as a log prefix ("mcp.<toolName>.sse_post" / "sse_post_failed").
func PostSSEStream(ctx context.Context, client *http.Client, url string, body any, toolName string, handler SSEHandler) error {
	if client == nil {
		client = http.DefaultClient
	}
	if handler == nil {
		return fmt.Errorf("PostSSEStream: handler is required")
	}

	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")

	slog.Info("mcp."+toolName+".sse_post", "url", redactProxyURL(url))
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("POST %s status %d: %s", redactProxyURL(url), resp.StatusCode, string(raw))
	}

	sequence := 0
	readErr := readSSE(ctx, resp.Body, func(evt sseEvent) error {
		sequence++
		terminate, hErr := handler(ctx, evt, sequence)
		if hErr != nil {
			return hErr
		}
		if terminate {
			return errSSETerminal
		}
		return nil
	})
	if errors.Is(readErr, errSSETerminal) {
		return nil
	}
	return readErr
}

// GetSSEStream opens a GET SSE connection to url and dispatches every decoded
// event to handler. Like PostSSEStream but for GET-based SSE endpoints (e.g.
// opencode /global/event). No request body is sent.
func GetSSEStream(ctx context.Context, client *http.Client, url string, toolName string, handler SSEHandler) error {
	if client == nil {
		client = http.DefaultClient
	}
	if handler == nil {
		return fmt.Errorf("GetSSEStream: handler is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	slog.Info("mcp."+toolName+".sse_get", "url", redactProxyURL(url))
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GET %s status %d: %s", redactProxyURL(url), resp.StatusCode, string(raw))
	}

	sequence := 0
	readErr := readSSE(ctx, resp.Body, func(evt sseEvent) error {
		sequence++
		terminate, hErr := handler(ctx, evt, sequence)
		if hErr != nil {
			return hErr
		}
		if terminate {
			return errSSETerminal
		}
		return nil
	})
	if errors.Is(readErr, errSSETerminal) {
		return nil
	}
	return readErr
}
