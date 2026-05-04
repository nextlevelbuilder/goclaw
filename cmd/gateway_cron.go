package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

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
// cronHeartbeatWakeFn holds the heartbeat wake function, set after ticker creation.
// Safe because cron jobs only fire after Start(), well after this is set.
var cronHeartbeatWakeFn func(agentID string)

func makeCronJobHandler(sched *scheduler.Scheduler, msgBus *bus.MessageBus, cfg *config.Config, channelMgr *channels.Manager, sessionMgr store.SessionStore, agentStore store.AgentStore) func(job *store.CronJob) (*store.CronJobResult, error) {
	return func(job *store.CronJob) (*store.CronJobResult, error) {
		agentID := job.AgentID
		if agentID == "" && agentStore != nil {
			// Resolve real default agent from DB instead of using literal "default" string.
			tenantCtx := store.WithTenantID(context.Background(), job.TenantID)
			if defaultAgent, err := agentStore.GetDefault(tenantCtx); err == nil {
				agentID = defaultAgent.AgentKey
			} else {
				agentID = cfg.ResolveDefaultAgentID()
			}
		} else if agentID == "" {
			agentID = cfg.ResolveDefaultAgentID()
		} else if id, err := uuid.Parse(agentID); err == nil && agentStore != nil {
			// Resolve agentKey from UUID so session key uses agentKey
			// (consistent with chat/WS/team paths, fixes cache invalidation mismatch).
			cronCtx := store.WithTenantID(context.Background(), job.TenantID)
			if ag, err := agentStore.GetByID(cronCtx, id); err == nil {
				agentID = ag.AgentKey
			}
		} else {
			agentID = config.NormalizeAgentID(agentID)
		}

		sessionKey := sessions.BuildCronSessionKey(agentID, job.ID)
		channel := job.DeliverChannel
		if channel == "" {
			channel = "cron"
		}

		// Infer peer kind from the stored session metadata (group chats need it
		// so that tools like message can route correctly via group APIs).
		peerKind := resolveCronPeerKind(job)

		// Resolve channel type for system prompt context.
		channelType := resolveChannelType(channelMgr, channel)

		// Build context with tenant scope and timeout so agent loop events are
		// scoped correctly and a hung agent can't block the cron scheduler forever.
		jobTimeout := cfg.Cron.JobTimeoutDuration()
		cronCtx, cancelCron := context.WithTimeout(context.Background(), jobTimeout)
		defer cancelCron()
		cronCtx = store.WithTenantID(cronCtx, job.TenantID)

		// Localised cron meta block. Locale is resolved from ctx — for cron the
		// ctx has no caller-supplied locale so store.LocaleFromContext cascades
		// through GOCLAW_DEFAULT_LOCALE → POSIX system locale → "en". The agent
		// loop sees this prompt prefixed to its request and will mirror language.
		extraPrompt := buildCronExtraPrompt(cronCtx, job)

		// Reset session before each cron run to prevent tool errors from previous
		// runs from polluting the context and blocking future executions (#294).
		// Save() persists the empty session to DB so stale data won't reload after restart.
		// Stateless jobs skip this — they intentionally carry no session history.
		if !job.Stateless {
			sessionMgr.Reset(cronCtx, sessionKey)
			sessionMgr.Save(cronCtx, sessionKey)
		}

		// Schedule through cron lane — scheduler handles agent resolution and concurrency
		outCh := sched.Schedule(cronCtx, scheduler.LaneCron, agent.RunRequest{
			SessionKey:        sessionKey,
			Message:           job.Payload.Message,
			Channel:           channel,
			ChannelType:       channelType,
			ChatID:            job.DeliverTo,
			PeerKind:          peerKind,
			UserID:            job.UserID,
			RunID:             fmt.Sprintf("cron:%s", job.ID),
			Stream:            false,
			ExtraSystemPrompt: extraPrompt,
			TraceName:         fmt.Sprintf("Cron [%s] - %s", job.Name, agentID),
			TraceTags:         []string{"cron"},
		})

		// Block until the scheduled run completes or the timeout fires.
		var outcome scheduler.RunOutcome
		select {
		case outcome = <-outCh:
		case <-cronCtx.Done():
			return nil, fmt.Errorf("cron job %s timed out after %s", job.Name, jobTimeout)
		}
		if outcome.Err != nil {
			return nil, outcome.Err
		}

		result := outcome.Result

		// If job wants delivery to a channel, send the agent response to the target chat.
		if job.Deliver && job.DeliverChannel != "" && job.DeliverTo != "" {
			outMsg := bus.OutboundMessage{
				Channel: job.DeliverChannel,
				ChatID:  job.DeliverTo,
				Content: result.Content,
			}
			if peerKind == "group" {
				outMsg.Metadata = map[string]string{"group_id": job.DeliverTo}
			}
			appendMediaToOutbound(&outMsg, result.Media)
			msgBus.PublishOutbound(outMsg)
		} else if job.Deliver {
			slog.Warn("cron: delivery configured but channel/chatID missing — output discarded",
				"job_id", job.ID, "job_name", job.Name, "channel", job.DeliverChannel, "to", job.DeliverTo)
		}

		cronResult := &store.CronJobResult{
			Content: result.Content,
		}
		if result.Usage != nil {
			cronResult.InputTokens = result.Usage.PromptTokens
			cronResult.OutputTokens = result.Usage.CompletionTokens
		}

		// wakeMode: trigger heartbeat after cron job completes.
		// Use original job.AgentID (UUID) — cronHeartbeatWakeFn expects UUID for ticker.Wake().
		if job.WakeHeartbeat && cronHeartbeatWakeFn != nil {
			cronHeartbeatWakeFn(job.AgentID)
		}

		return cronResult, nil
	}
}

// resolveCronPeerKind infers peer kind from the cron job's user ID.
// Group cron jobs have userID prefixed with "group:" or "guild:" (set during job creation).
func resolveCronPeerKind(job *store.CronJob) string {
	if strings.HasPrefix(job.UserID, "group:") || strings.HasPrefix(job.UserID, "guild:") {
		return "group"
	}
	return ""
}

// buildCronExtraPrompt formats the per-job meta block prepended to the agent's
// system prompt. The block tells the LLM this run originated from a cron job
// and where (if anywhere) the response will be delivered.
//
// Locale is resolved from ctx via store.LocaleFromContext, which cascades
// through GOCLAW_DEFAULT_LOCALE → POSIX system locale → "en". For cron the ctx
// carries no caller-supplied locale, so the cascade picks the operator's
// configured language. The block is emitted in that language so the LLM is
// not nudged into English by a contradictory English meta-prompt while the
// agent's own persona expects Korean / Vietnamese / etc output.
func buildCronExtraPrompt(ctx context.Context, job *store.CronJob) string {
	deliveryConfigured := job.Deliver && job.DeliverChannel != "" && job.DeliverTo != ""
	switch store.LocaleFromContext(ctx) {
	case "ko":
		if deliveryConfigured {
			return fmt.Sprintf(
				"[크론 작업]\n예약된 작업 \"%s\" (ID: %s) 실행입니다.\n"+
					"요청자: 사용자 %s, 채널 \"%s\" (chat %s).\n"+
					"응답은 해당 채팅으로 자동 전달되니 본문만 그대로 작성하세요.",
				job.Name, job.ID, job.UserID, job.DeliverChannel, job.DeliverTo,
			)
		}
		return fmt.Sprintf(
			"[크론 작업]\n예약된 작업 \"%s\" (ID: %s), 생성자 %s.\n"+
				"전달 대상이 설정되어 있지 않으니 평소처럼 응답하세요.",
			job.Name, job.ID, job.UserID,
		)
	case "vi":
		if deliveryConfigured {
			return fmt.Sprintf(
				"[Cron Job]\nTác vụ định kỳ \"%s\" (ID: %s) đang chạy.\n"+
					"Người yêu cầu: %s, kênh \"%s\" (chat %s).\n"+
					"Phản hồi sẽ tự động gửi tới cuộc trò chuyện đó — chỉ cần viết nội dung trực tiếp.",
				job.Name, job.ID, job.UserID, job.DeliverChannel, job.DeliverTo,
			)
		}
		return fmt.Sprintf(
			"[Cron Job]\nTác vụ định kỳ \"%s\" (ID: %s), do người dùng %s tạo.\n"+
				"Chưa cấu hình kênh giao — phản hồi như bình thường.",
			job.Name, job.ID, job.UserID,
		)
	case "zh":
		if deliveryConfigured {
			return fmt.Sprintf(
				"[Cron Job]\n计划任务 \"%s\" (ID: %s) 正在执行。\n"+
					"请求者:用户 %s,频道 \"%s\" (chat %s).\n"+
					"响应会自动发送到该聊天 —— 直接生成内容即可。",
				job.Name, job.ID, job.UserID, job.DeliverChannel, job.DeliverTo,
			)
		}
		return fmt.Sprintf(
			"[Cron Job]\n计划任务 \"%s\" (ID: %s),由用户 %s 创建。\n"+
				"未配置投递目标 —— 正常响应即可。",
			job.Name, job.ID, job.UserID,
		)
	}
	// Default English.
	if deliveryConfigured {
		return fmt.Sprintf(
			"[Cron Job]\nThis is scheduled job \"%s\" (ID: %s).\n"+
				"Requester: user %s on channel \"%s\" (chat %s).\n"+
				"Your response will be automatically delivered to that chat — just produce the content directly.",
			job.Name, job.ID, job.UserID, job.DeliverChannel, job.DeliverTo,
		)
	}
	return fmt.Sprintf(
		"[Cron Job]\nThis is scheduled job \"%s\" (ID: %s), created by user %s.\n"+
			"Delivery is not configured — respond normally.",
		job.Name, job.ID, job.UserID,
	)
}
