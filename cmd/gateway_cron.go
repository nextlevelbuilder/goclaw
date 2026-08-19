package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/cronexec"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
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

// cronCLISessionReset clears the Claude CLI on-disk session (.jsonl + CLAUDE.md)
// for a session key, mirroring the sessions.reset RPC. Indirected through a var
// so the stateless-reset behavior can be unit-tested without filesystem effects.
var cronCLISessionReset = providers.ResetCLISession

// cronTenantContext scopes a context to the job's tenant. It sets BOTH the
// tenant ID and the tenant SLUG: tenant-scoped filesystem paths
// (skills-store, workspace, media via config.TenantScopedDir) key off the
// slug, and resolve to an id-based path when the slug is absent — a different
// directory than where HTTP/WS upload materialized the files. Without the
// slug, a cron agent turn sees NONE of its tenant's managed skills. tenantStore
// may be nil (older wiring) — then only the tenant ID is set, preserving prior
// behavior. Master tenant needs no slug (TenantScopedDir returns the base).
func cronTenantContext(ctx context.Context, tenantStore store.TenantStore, tenantID uuid.UUID) context.Context {
	ctx = store.WithTenantID(ctx, tenantID)
	if tenantStore == nil || tenantID == uuid.Nil || tenantID == store.MasterTenantID {
		return ctx
	}
	tenant, err := tenantStore.GetTenant(ctx, tenantID)
	if err != nil || tenant == nil || tenant.Slug == "" {
		slog.Warn("cron: could not resolve tenant slug; tenant-scoped skills/workspace may be invisible",
			"tenant_id", tenantID, "error", err)
		return ctx
	}
	return store.WithTenantSlug(ctx, tenant.Slug)
}

func makeCronJobHandler(sched *scheduler.Scheduler, msgBus *bus.MessageBus, cfg *config.Config, channelMgr *channels.Manager, sessionMgr store.SessionStore, agentStore store.AgentStore, tenantStore store.TenantStore, providerStore store.ProviderStore, providerReg *providers.Registry, missionStore store.MissionStore) func(job *store.CronJob) (*store.CronJobResult, error) {
	return func(job *store.CronJob) (*store.CronJobResult, error) {
		// Mission payload: a scheduled mission tick. The mission ID is carried in
		// job.Payload.Message (KISS — the CronPayload struct is untouched). The
		// owning agent continues the mission's session through the scheduler cron
		// lane, exactly as an agent-turn job would.
		if job.Payload.Kind == store.CronPayloadKindMission {
			return runMissionCronJob(sched, cfg, channelMgr, job, missionStore, tenantStore, msgBus)
		}

		agentID := job.AgentID
		if agentID == "" && agentStore != nil {
			// Resolve real default agent from DB instead of using literal "default" string.
			tenantCtx := cronTenantContext(context.Background(), tenantStore, job.TenantID)
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
			cronCtx := cronTenantContext(context.Background(), tenantStore, job.TenantID)
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

		// Deterministic command payload: run the shell command in-process WITHOUT
		// an LLM/agent turn (zero model tokens). Gated by cron.command_enabled.
		if job.Payload.IsCommand() {
			return runCommandCronJob(cfg, job, tenantStore, msgBus, peerKind)
		}

		// Build cron context so the agent knows delivery target and requester.
		var extraPrompt string
		if job.Deliver && job.DeliverChannel != "" && job.DeliverTo != "" {
			extraPrompt = fmt.Sprintf(
				"[Cron Job]\nThis is scheduled job \"%s\" (ID: %s).\n"+
					"Requester: user %s on channel \"%s\" (chat %s).\n"+
					"Your response will be automatically delivered to that chat — just produce the content directly.",
				job.Name, job.ID, job.UserID, job.DeliverChannel, job.DeliverTo,
			)
		} else {
			extraPrompt = fmt.Sprintf(
				"[Cron Job]\nThis is scheduled job \"%s\" (ID: %s), created by user %s.\n"+
					"Delivery is not configured — respond normally.",
				job.Name, job.ID, job.UserID,
			)
		}

		// Build context with tenant scope and timeout so agent loop events are
		// scoped correctly and a hung agent can't block the cron scheduler forever.
		jobTimeout := cfg.Cron.JobTimeoutDuration()
		cronCtx, cancelCron := context.WithTimeout(context.Background(), jobTimeout)
		defer cancelCron()
		cronCtx = cronTenantContext(cronCtx, tenantStore, job.TenantID)
		if job.Payload.CredentialUserID != "" {
			cronCtx = store.WithCredentialUserID(cronCtx, job.Payload.CredentialUserID)
		}
		chatTitle := resolveGroupDisplayTitle(cronCtx, channelMgr, channel, job.DeliverTo, peerKind, "")

		// Reset the session before each STATELESS cron run so the run starts fresh:
		// no carried-over history (the whole point of stateless — saves tokens) and
		// no tool errors from a previous run polluting the context (#294). Clear BOTH
		// layers — the goclaw session store AND the Claude CLI's own on-disk session —
		// because a claude-cli agent resumes its .jsonl by a deterministic per-key
		// UUID and would otherwise replay the full accumulated history every run
		// regardless of this flag (i.e. "stateless" had no effect on what the model
		// actually sees). Stateful jobs (stateless=false) intentionally keep their
		// session across runs.
		//
		// NOTE: this condition was previously `!job.Stateless`, which inverted the
		// flag — stateless jobs accumulated unbounded history while stateful jobs were
		// wiped each run.
		if job.Stateless {
			if sessionMgr != nil {
				sessionMgr.Reset(cronCtx, sessionKey)
				sessionMgr.Save(cronCtx, sessionKey)
			}
			cronCLISessionReset("", sessionKey)
		}

		// Resolve per-job provider/model override (mirrors heartbeat). Unset → agent default.
		var providerOverride providers.Provider
		if job.ProviderID != nil && providerStore != nil && providerReg != nil {
			if provData, perr := providerStore.GetProvider(cronCtx, *job.ProviderID); perr == nil {
				if prov, gerr := providerReg.GetForTenant(job.TenantID, provData.Name); gerr == nil {
					providerOverride = prov
				} else {
					slog.Warn("cron.provider_not_in_registry", "job", job.ID, "provider_id", job.ProviderID, "error", gerr)
				}
			} else {
				slog.Warn("cron.provider_not_found", "job", job.ID, "provider_id", job.ProviderID, "error", perr)
			}
		}
		var modelOverride string
		if job.Model != nil {
			modelOverride = *job.Model

		}

		// Schedule through cron lane — scheduler handles agent resolution and concurrency
		outCh := sched.Schedule(cronCtx, scheduler.LaneCron, agent.RunRequest{
			SessionKey:        sessionKey,
			Message:           job.Payload.Message,
			Channel:           channel,
			ChannelType:       channelType,
			ChatID:            job.DeliverTo,
			ChatTitle:         chatTitle,
			PeerKind:          peerKind,
			UserID:            job.UserID,
			RunID:             fmt.Sprintf("cron:%s", job.ID),
			Stream:            false,
			ModelOverride:     modelOverride,
			ProviderOverride:  providerOverride,
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
		deliverCronOutput(msgBus, job, result.Content, result.Media, peerKind)

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

// deliverCronOutput publishes a cron job's output to the configured delivery
// channel, honoring the NO_REPLY sentinel. Shared by the agent-turn and the
// deterministic command-payload paths.
func deliverCronOutput(msgBus *bus.MessageBus, job *store.CronJob, content string, media []agent.MediaResult, peerKind string) {
	if job.Deliver && job.DeliverChannel != "" && job.DeliverTo != "" {
		if cronOutputContainsNoReplySentinel(content) {
			slog.Info("cron: suppressed delivery because output contained NO_REPLY",
				"job_id", job.ID,
				"job_name", job.Name,
				"channel", job.DeliverChannel,
				"to", job.DeliverTo,
				"content_len", len(content),
			)
			return
		}
		outMsg := bus.OutboundMessage{
			Channel: job.DeliverChannel,
			ChatID:  job.DeliverTo,
			Content: content,
		}
		if peerKind == "group" {
			outMsg.Metadata = map[string]string{"group_id": job.DeliverTo}
		}
		appendMediaToOutbound(&outMsg, media)
		msgBus.PublishOutbound(outMsg)
		return
	}
	if job.Deliver {
		slog.Warn("cron: delivery configured but channel/chatID missing — output discarded",
			"job_id", job.ID, "job_name", job.Name, "channel", job.DeliverChannel, "to", job.DeliverTo)
	}
}

// runCommandCronJob executes a deterministic command-payload cron job in-process
// without an LLM turn. On success it delivers the command output (stdout, else
// stderr) like an agent turn. On failure it returns an error so the run is
// recorded as "error" and retried per cron.max_retries — failures are NOT
// delivered, mirroring the agent path where only successful output is announced.
func runCommandCronJob(cfg *config.Config, job *store.CronJob, tenantStore store.TenantStore, msgBus *bus.MessageBus, peerKind string) (*store.CronJobResult, error) {
	if !cfg.Cron.CommandEnabled {
		return nil, fmt.Errorf("cron command payloads are disabled; set cron.command_enabled=true to allow them")
	}
	spec := job.Payload.Command
	if err := store.ValidateCronCommandSpec(spec); err != nil {
		return nil, err
	}

	cmdTimeout := cfg.Cron.CommandTimeoutDuration()
	if spec.TimeoutSeconds > 0 {
		cmdTimeout = time.Duration(spec.TimeoutSeconds) * time.Second
	}
	// The job timeout is a hard ceiling above the per-command timeout.
	ctx, cancel := context.WithTimeout(cronTenantContext(context.Background(), tenantStore, job.TenantID), cfg.Cron.JobTimeoutDuration())
	defer cancel()

	res := cronexec.Run(ctx, cronexec.Spec{
		Argv:            spec.Argv,
		Cwd:             spec.Cwd,
		Env:             spec.Env,
		Input:           spec.Input,
		Timeout:         cmdTimeout,
		NoOutputTimeout: time.Duration(spec.NoOutputTimeoutSeconds) * time.Second,
		OutputMaxBytes:  spec.OutputMaxBytes,
	})
	if res.Status != cronexec.StatusOK {
		return nil, res.Err
	}

	deliverCronOutput(msgBus, job, res.Summary, nil, peerKind)
	return &store.CronJobResult{Content: res.Summary}, nil
}

// runMissionCronJob resumes a mission on its scheduler cron tick. The cron
// job's payload.Message carries the mission ID (KISS — the CronPayload struct
// is unchanged). It resolves the mission, rejects terminal states, then
// schedules an agent turn on the mission's OWN session key so the owning agent
// continues the mission's goals at the configured cadence. On a successful
// run the mission transitions back to active (matching mission.resume).
func runMissionCronJob(sched *scheduler.Scheduler, cfg *config.Config, channelMgr *channels.Manager, job *store.CronJob, missionStore store.MissionStore, tenantStore store.TenantStore, msgBus *bus.MessageBus) (*store.CronJobResult, error) {
	if missionStore == nil {
		return nil, fmt.Errorf("mission cron job %s: mission store not available", job.ID)
	}
	missionID := job.Payload.Message
	if missionID == "" {
		return nil, fmt.Errorf("mission cron job %s: empty mission ID in payload", job.ID)
	}
	id, err := uuid.Parse(missionID)
	if err != nil {
		return nil, fmt.Errorf("mission cron job %s: invalid mission ID %q", job.ID, missionID)
	}

	// Mission ticks share the tenant scoping and hard timeout of agent-turn
	// cron jobs so a hung continuation can't block the cron scheduler forever.
	jobTimeout := cfg.Cron.JobTimeoutDuration()
	cronCtx, cancel := context.WithTimeout(cronTenantContext(context.Background(), tenantStore, job.TenantID), jobTimeout)
	defer cancel()

	mission, err := missionStore.GetMission(cronCtx, id)
	if err != nil {
		return nil, fmt.Errorf("mission cron job %s: get mission: %w", job.ID, err)
	}
	switch mission.Status {
	case store.MissionStatusCompleted, store.MissionStatusFailed, store.MissionStatusCancelled:
		return nil, fmt.Errorf("mission cron job %s: mission %s is in terminal state %q", job.ID, missionID, mission.Status)
	}
	if mission.SessionKey == "" {
		return nil, fmt.Errorf("mission cron job %s: mission %s has no session key", job.ID, missionID)
	}

	peerKind := resolveCronPeerKind(job)
	// Schedule the continuation through the cron lane so per-session concurrency
	// control (same session can't run concurrently) and /stop integration apply
	// exactly as they do for agent-turn cron jobs.
	outCh := sched.Schedule(cronCtx, scheduler.LaneCron, agent.RunRequest{
		SessionKey:  mission.SessionKey,
		Message:     missionContinuePrompt(mission),
		Channel:     job.DeliverChannel,
		ChannelType: resolveChannelType(channelMgr, job.DeliverChannel),
		ChatID:      job.DeliverTo,
		PeerKind:    peerKind,
		UserID:      job.UserID,
		RunID:       fmt.Sprintf("cron-mission:%s", job.ID),
		Stream:      false,
		TraceName:   fmt.Sprintf("Mission [%s] - %s", mission.Name, mission.SessionKey),
		TraceTags:   []string{"cron", "mission"},
	})

	var outcome scheduler.RunOutcome
	select {
	case outcome = <-outCh:
	case <-cronCtx.Done():
		return nil, fmt.Errorf("mission cron job %s timed out after %s", job.Name, jobTimeout)
	}
	if outcome.Err != nil {
		return nil, outcome.Err
	}

	// A successfully ticked mission is active again (matching mission.resume).
	if err := missionStore.UpdateMissionStatus(cronCtx, id, store.MissionStatusActive); err != nil {
		slog.Warn("cron.mission_status_update_failed", "job_id", job.ID, "mission_id", missionID, "error", err)
	}

	result := outcome.Result
	deliverCronOutput(msgBus, job, result.Content, result.Media, peerKind)

	cronResult := &store.CronJobResult{Content: result.Content}
	if result.Usage != nil {
		cronResult.InputTokens = result.Usage.PromptTokens
		cronResult.OutputTokens = result.Usage.CompletionTokens
	}
	return cronResult, nil
}

// missionContinuePrompt builds the continuation directive handed to the agent
// on each mission cron tick so it keeps the mission's goals, milestones, and
// acceptance criteria in context without requiring a fresh model turn setup.
func missionContinuePrompt(m *store.Mission) string {
	var b strings.Builder
	b.WriteString("Continue working on mission: ")
	b.WriteString(m.Name)
	b.WriteString(".")
	if len(m.Goals) > 0 {
		b.WriteString(" Goals: ")
		b.WriteString(strings.Join(m.Goals, "; "))
		b.WriteString(".")
	}
	if len(m.Milestones) > 0 {
		b.WriteString(" Milestones: ")
		b.WriteString(strings.Join(m.Milestones, "; "))
		b.WriteString(".")
	}
	if len(m.Acceptance) > 0 {
		b.WriteString(" Acceptance criteria: ")
		b.WriteString(strings.Join(m.Acceptance, "; "))
		b.WriteString(".")
	}
	return b.String()
}

func cronOutputContainsNoReplySentinel(content string) bool {
	return agent.IsSilentReply(content)
}

// resolveCronPeerKind infers peer kind from the cron job's user ID.
// Group cron jobs have userID prefixed with "group:" or "guild:" (set during job creation).
func resolveCronPeerKind(job *store.CronJob) string {
	if strings.HasPrefix(job.UserID, "group:") || strings.HasPrefix(job.UserID, "guild:") {
		return "group"
	}
	return ""
}
