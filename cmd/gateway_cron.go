package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// makeCronJobHandler creates a cron job handler that routes through the scheduler's cron lane.
// This ensures per-session concurrency control (same job can't run concurrently)
// and integration with /stop, /stopall commands.
func makeCronJobHandler(sched *scheduler.Scheduler, msgBus *bus.MessageBus, cfg *config.Config, channelMgr *channels.Manager) func(job *store.CronJob) (*store.CronJobResult, error) {
	return func(job *store.CronJob) (*store.CronJobResult, error) {
		agentID := job.AgentID
		if agentID == "" {
			agentID = cfg.ResolveDefaultAgentID()
		} else {
			agentID = config.NormalizeAgentID(agentID)
		}

		sessionKey := sessions.BuildCronSessionKey(agentID, job.ID)
		channel := job.Payload.Channel
		if channel == "" {
			channel = "cron"
		}

		// Infer peer kind from the stored session metadata (group chats need it
		// so that tools like message can route correctly via group APIs).
		peerKind := resolveCronPeerKind(job)

		// Resolve channel type for system prompt context.
		channelType := resolveChannelType(channelMgr, channel)

		// Schedule through cron lane — scheduler handles agent resolution and concurrency
		outCh := sched.Schedule(context.Background(), scheduler.LaneCron, agent.RunRequest{
			SessionKey:  sessionKey,
			Message:     job.Payload.Message,
			Channel:     channel,
			ChannelType: channelType,
			ChatID:      job.Payload.To,
			PeerKind:    peerKind,
			UserID:      job.UserID,
			RunID:       fmt.Sprintf("cron:%s", job.ID),
			Stream:      false,
			TraceName:   fmt.Sprintf("Cron [%s] - %s", job.Name, agentID),
			TraceTags:   []string{"cron"},
		})

		// Block until the scheduled run completes
		outcome := <-outCh
		if outcome.Err != nil {
			return nil, outcome.Err
		}

		result := outcome.Result

		// If job wants delivery to a channel, send the agent response to the target chat.
		if job.Payload.Deliver && job.Payload.Channel != "" && job.Payload.To != "" {
			outMsg := bus.OutboundMessage{
				Channel: job.Payload.Channel,
				ChatID:  job.Payload.To,
				Content: result.Content,
			}
			if peerKind == "group" {
				outMsg.Metadata = map[string]string{"group_id": job.Payload.To}
			}
			appendMediaToOutbound(&outMsg, result.Media)
			msgBus.PublishOutbound(outMsg)
		}

		cronResult := &store.CronJobResult{
			Content: result.Content,
		}
		if result.Usage != nil {
			cronResult.InputTokens = result.Usage.PromptTokens
			cronResult.OutputTokens = result.Usage.CompletionTokens
		}

		return cronResult, nil
	}
}

// resolveCronPeerKind infers peer kind from the cron job's user ID.
// Group cron jobs have userID prefixed with "group:" (set during job creation).
func resolveCronPeerKind(job *store.CronJob) string {
	if strings.HasPrefix(job.UserID, "group:") {
		return "group"
	}
	return ""
}
