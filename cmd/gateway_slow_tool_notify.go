package cmd

import (
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// wireSlowToolNotifySubscriber registers a subscriber that sends direct outbound
// notifications when a tool call exceeds its adaptive slow threshold.
// Always uses direct mode (never leader) to avoid wasting LLM calls.
// Team config (slow_tool enabled/disabled) is resolved in the loop before emitting
// the event, so no DB query is needed here.
func wireSlowToolNotifySubscriber(msgBus *bus.MessageBus) {
	msgBus.Subscribe("consumer.slow-tool-notify", func(event bus.Event) {
		if event.Name != protocol.EventAgent {
			return
		}
		agentEvent, ok := event.Payload.(agent.AgentEvent)
		if !ok || agentEvent.Type != protocol.AgentEventActivity {
			return
		}
		payloadMap, _ := agentEvent.Payload.(map[string]any)
		phase, _ := payloadMap["phase"].(string)
		if phase != "tool_slow" {
			return
		}
		if agentEvent.Channel == "" || agentEvent.ChatID == "" {
			return
		}

		tool, _ := payloadMap["tool"].(string)
		thresholdMs, _ := payloadMap["threshold_ms"].(int64)
		thresholdSec := thresholdMs / 1000
		if thresholdSec <= 0 {
			thresholdSec = 120
		}

		content := fmt.Sprintf("⏳ %s: tool %s running longer than usual (>%ds)", agentEvent.AgentID, tool, thresholdSec)
		msgBus.PublishOutbound(bus.OutboundMessage{
			Channel: agentEvent.Channel,
			ChatID:  agentEvent.ChatID,
			Content: content,
		})
	})
}
