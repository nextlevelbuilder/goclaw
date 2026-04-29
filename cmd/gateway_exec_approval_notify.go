package cmd

import (
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// wireExecApprovalNotifySubscriber registers a subscriber that sends direct outbound
// notifications when an exec command requires user approval. The notification is sent
// to the originating channel so users can approve/deny from wherever they are chatting.
func wireExecApprovalNotifySubscriber(msgBus *bus.MessageBus) {
	msgBus.Subscribe("consumer.exec-approval-notify", func(event bus.Event) {
		if event.Name != protocol.EventExecApprovalReq {
			return
		}
		snapshot, ok := event.Payload.(tools.PendingApprovalSnapshot)
		if !ok || snapshot.Channel == "" || snapshot.ChatID == "" {
			return
		}

		content := fmt.Sprintf(
			"Command approval required [%s]\nCommand: %s\n\nReply: /approve %s or /deny %s",
			snapshot.ShortCode,
			truncateApprovalCmd(snapshot.Command, 200),
			snapshot.ShortCode,
			snapshot.ShortCode,
		)

		msgBus.PublishOutbound(bus.OutboundMessage{
			Channel:  snapshot.Channel,
			ChatID:   snapshot.ChatID,
			Content:  content,
			Metadata: map[string]string{
				"exec_approval_id":   snapshot.ID,
				"exec_approval_code": snapshot.ShortCode,
			},
		})
	})
}

func truncateApprovalCmd(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
