// Package pipeline — Layer 5 of Context Defense (CP-01).
// Context collapse: read-time projection that reduces API payload
// without modifying the actual message buffer.
package pipeline

import (
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// CollapseStrategy defines how old messages are compressed for the API view.
type CollapseStrategy int

const (
	// CollapseStripToolResults removes tool result content, keeps stub.
	CollapseStripToolResults CollapseStrategy = iota
	// CollapseKeepFirstLine keeps only the first line of each message.
	CollapseKeepFirstLine
)

// CollapseRule matches messages by age and applies a compression strategy.
type CollapseRule struct {
	// MinIterationAge: messages from iterations older than this are collapsed.
	MinIterationAge int
	Strategy        CollapseStrategy
}

// ContextCollapser applies read-time projections to messages before API call.
// Original messages in MessageBuffer are NOT modified.
type ContextCollapser struct {
	Rules []CollapseRule
}

// DefaultCollapser creates progressive collapse rules:
//   - Iterations > 20 ago: strip tool result content
//   - Iterations > 40 ago: keep first line only
func DefaultCollapser() *ContextCollapser {
	return &ContextCollapser{
		Rules: []CollapseRule{
			{MinIterationAge: 20, Strategy: CollapseStripToolResults},
			{MinIterationAge: 40, Strategy: CollapseKeepFirstLine},
		},
	}
}

// Project creates a reduced view of messages for API consumption.
// Returns a new slice — original messages are untouched.
func (cc *ContextCollapser) Project(msgs []providers.Message, currentIteration int, totalMsgs int) []providers.Message {
	if cc == nil || len(cc.Rules) == 0 || currentIteration == 0 {
		return msgs
	}

	projected := make([]providers.Message, 0, len(msgs))
	for i, msg := range msgs {
		// System messages always kept as-is
		if msg.Role == "system" {
			projected = append(projected, msg)
			continue
		}

		iterEst := messageIterationEstimate(i, totalMsgs, currentIteration)
		age := currentIteration - iterEst

		collapsed := msg
		for _, rule := range cc.Rules {
			if age >= rule.MinIterationAge {
				collapsed = applyCollapse(collapsed, rule.Strategy)
			}
		}

		content := messageContent(collapsed)
		if content != "" {
			projected = append(projected, collapsed)
		}
	}
	return projected
}

func applyCollapse(msg providers.Message, strategy CollapseStrategy) providers.Message {
	content := messageContent(msg)
	if content == "" {
		return msg
	}

	switch strategy {
	case CollapseStripToolResults:
		if msg.Role == "tool" {
			return replaceMessageContent(msg, "[tool result collapsed — re-run tool if needed]")
		}
	case CollapseKeepFirstLine:
		first := firstLine(content)
		if len(first) < len(content) {
			return replaceMessageContent(msg, first+"...")
		}
	}
	return msg
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx > 0 {
		return s[:idx]
	}
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
