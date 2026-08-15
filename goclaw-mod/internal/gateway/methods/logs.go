package methods

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// LogsMethods handles logs.tail (start/stop live log tailing).
type LogsMethods struct {
	logTee *gateway.LogTee
}

func NewLogsMethods(logTee *gateway.LogTee) *LogsMethods {
	return &LogsMethods{logTee: logTee}
}

func (m *LogsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodLogsTail, m.handleTail)
}

func (m *LogsMethods) handleTail(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		Action string `json:"action"`
		Level  string `json:"level"` // "debug", "info", "warn", "error" (default: "info")
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	switch params.Action {
	case "start":
		level := parseLogLevel(params.Level)
		m.logTee.Subscribe(client, level)
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
			"status": "tailing",
			"level":  params.Level,
		}))
	case "stop":
		m.logTee.Unsubscribe(client.ID())
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
			"status": "stopped",
		}))
	default:
		client.SendResponse(protocol.NewErrorResponse(
			req.ID,
			protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidLogAction),
		))
	}
}

func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
