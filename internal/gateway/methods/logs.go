package methods

import (
	"context"
	"encoding/json"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
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

func (m *LogsMethods) handleTail(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		Action string `json:"action"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	switch params.Action {
	case "start":
		m.logTee.Subscribe(client)
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
			"status": "tailing",
		}))
	case "stop":
		m.logTee.Unsubscribe(client.ID())
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
			"status": "stopped",
		}))
	default:
		client.SendResponse(protocol.NewErrorResponse(
			req.ID,
			protocol.ErrInvalidRequest,
			"action must be 'start' or 'stop'",
		))
	}
}
