package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// ------------------------------------------------------------------
// OpenClaw JSON types (parsed from ~/.openclaw/openclaw.json)
// ------------------------------------------------------------------

type ocConfig struct {
	Env      map[string]string `json:"env"`
	Agents   ocAgents          `json:"agents"`
	Channels ocChannels        `json:"channels"`
	Bindings json.RawMessage   `json:"bindings"`
	Tools    json.RawMessage   `json:"tools"`
}

type ocAgents struct {
	Defaults json.RawMessage `json:"defaults"`
	List     []ocAgent       `json:"list"`
}

type ocAgent struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Workspace string          `json:"workspace"`
	AgentDir  string          `json:"agentDir"`
	Model     string          `json:"model"`
	Identity  *ocIdentity     `json:"identity"`
	Tools     json.RawMessage `json:"tools"`
	Sandbox   json.RawMessage `json:"sandbox"`
	Subagents json.RawMessage `json:"subagents"`
	Heartbeat *ocHeartbeat    `json:"heartbeat"`
}

type ocIdentity struct {
	Name  string `json:"name"`
	Theme string `json:"theme"`
	Emoji string `json:"emoji"`
}

type ocHeartbeat struct {
	Every       string `json:"every"`
	Prompt      string `json:"prompt"`
	Target      string `json:"target"`
	ActiveHours *struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"activeHours"`
}

type ocChannels struct {
	Telegram ocTelegramChannel `json:"telegram"`
	Feishu   ocFeishuChannel   `json:"feishu"`
}

type ocTelegramChannel struct {
	Enabled  bool                      `json:"enabled"`
	BotToken string                    `json:"botToken"`
	Accounts map[string]ocTelegramAcct `json:"accounts"`
}

type ocTelegramAcct struct {
	Name        string                     `json:"name"`
	Enabled     *bool                      `json:"enabled"`
	DMPolicy    string                     `json:"dmPolicy"`
	BotToken    string                     `json:"botToken"`
	Groups      map[string]json.RawMessage `json:"groups"`
	AllowFrom   json.RawMessage            `json:"allowFrom"`
	GroupPolicy string                     `json:"groupPolicy"`
	Streaming   string                     `json:"streaming"`
}

type ocFeishuChannel struct {
	Enabled        bool                    `json:"enabled"`
	Domain         string                  `json:"domain"`
	DefaultAccount string                  `json:"defaultAccount"`
	Accounts       map[string]ocFeishuAcct `json:"accounts"`
}

type ocFeishuAcct struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Domain    string `json:"domain"`
	AppID     string `json:"appId"`
	AppSecret string `json:"appSecret"`
}

// OpenClaw config.json (separate from openclaw.json)
type ocGatewayConfig struct {
	MCPServers map[string]ocMCPServer `json:"mcpServers"`
}

type ocMCPServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// OpenClaw cron/jobs.json
type ocCronFile struct {
	Version int         `json:"version"`
	Jobs    []ocCronJob `json:"jobs"`
}

type ocCronJob struct {
	ID       string          `json:"id"`
	AgentID  string          `json:"agentId"`
	Name     string          `json:"name"`
	Enabled  bool            `json:"enabled"`
	Schedule ocCronSchedule  `json:"schedule"`
	Payload  ocCronPayload   `json:"payload"`
	Delivery *ocCronDelivery `json:"delivery"`
}

type ocCronSchedule struct {
	Kind string `json:"kind"`
	Expr string `json:"expr"`
	TZ   string `json:"tz"`
}

type ocCronPayload struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type ocCronDelivery struct {
	Mode      string `json:"mode"`
	Channel   string `json:"channel"`
	To        string `json:"to"`
	AccountID string `json:"accountId"`
}

// ------------------------------------------------------------------
// Agent mapping helpers
// ------------------------------------------------------------------

var openAgentKeys = map[string]bool{
	"main": true, "buddy": true, "reminder": true, "myfriend": true,
}

func toGocSlug(id string) string {
	return strings.ReplaceAll(id, "_", "-")
}

func splitProviderModel(s string) (string, string) {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "anthropic", s
}

func displayNameForAgent(a ocAgent) string {
	if a.Identity != nil && a.Identity.Name != "" {
		return a.Identity.Name
	}
	if a.Name != "" {
		return a.Name
	}
	return a.ID
}

// cronNameToSlug converts an OpenClaw cron job name to a GoClaw-compatible slug.
// "🌐 GV2 Daily Standup 9h" → "gv2-daily-standup-9h"
// "sunday-meeting-docs-followup" → "sunday-meeting-docs-followup"
func cronNameToSlug(name string) string {
	var buf strings.Builder
	lower := strings.ToLower(name)
	prevDash := false
	for _, r := range lower {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			buf.WriteRune(r)
			prevDash = false
		case r == '-' || r == '_' || r == ' ':
			if buf.Len() > 0 && !prevDash {
				buf.WriteByte('-')
				prevDash = true
			}
		}
	}
	s := buf.String()
	return strings.TrimRight(s, "-")
}

// resolveDeliverChannel builds the GoClaw channel instance name from OpenClaw delivery config.
// e.g., channel="telegram", accountId="gearvn_v2" → "telegram/gearvn-v2"
func resolveDeliverChannel(d *ocCronDelivery) string {
	if d == nil || d.Channel == "" {
		return ""
	}
	acctID := d.AccountID
	if acctID == "" || acctID == "default" {
		return d.Channel
	}
	return d.Channel + "/" + toGocSlug(acctID)
}

// cleanDeliverTo strips channel prefix from delivery target.
// "telegram:-5023742363" → "-5023742363"
func cleanDeliverTo(raw string) string {
	if i := strings.IndexByte(raw, ':'); i >= 0 {
		prefix := raw[:i]
		if prefix == "telegram" || prefix == "feishu" || prefix == "lark" || prefix == "discord" {
			return raw[i+1:]
		}
	}
	return raw
}

// ------------------------------------------------------------------
// Account → agent key mappings
// ------------------------------------------------------------------

var telegramAccountToAgentKey = map[string]string{
	"default":                 "main",
	"buddy":                   "buddy",
	"reminder":                "reminder",
	"myfriend":                "myfriend",
	"finance":                 "finance",
	"tpk":                     "tpk",
	"incentive_hub_assistant": "incentive-hub-assistant",
	"gws_assistant":           "gws-assistant",
	"rebate_assistant":        "rebate-assistant",
	"secondhand":              "secondhand-assistant",
	"gp-tracker-bot":          "gp-pm-tracker",
	"thainha":                 "thainha-assistant",
	"gearvn_v2":               "gearvn-v2",
	"media_mkt":               "media-mkt",
}

var feishuAccountToAgentKey = map[string]string{
	"lark":      "main",
	"gearvn_v2": "gearvn-v2",
}

// ------------------------------------------------------------------
// Command
// ------------------------------------------------------------------

func migrateOpenClawCmd() *cobra.Command {
	var (
		sourceFile      string
		workspaceSource string
		configSource    string
		cronSource      string
		dryRun          bool
		skipAgents      bool
		skipChannels    bool
		skipFiles       bool
		skipMCP         bool
		skipCrons       bool
	)

	cmd := &cobra.Command{
		Use:   "migrate-openclaw",
		Short: "Migrate agents, channels, docs, and settings from OpenClaw",
		Long:  "Reads ~/.openclaw/openclaw.json and migrates agents, channel instances, context files, MCP servers, cron jobs, and workspace files to GoClaw.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrateOpenClaw(sourceFile, workspaceSource, configSource, cronSource, dryRun, skipAgents, skipChannels, skipFiles, skipMCP, skipCrons)
		},
	}

	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&sourceFile, "source", filepath.Join(home, ".openclaw", "openclaw.json"), "path to openclaw.json")
	cmd.Flags().StringVar(&workspaceSource, "workspace-source", filepath.Join(home, ".openclaw", "workspace"), "path to OpenClaw main workspace")
	cmd.Flags().StringVar(&configSource, "config-source", filepath.Join(home, ".openclaw", "config.json"), "path to OpenClaw config.json (for MCP servers)")
	cmd.Flags().StringVar(&cronSource, "cron-source", filepath.Join(home, ".openclaw", "cron", "jobs.json"), "path to OpenClaw cron/jobs.json")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print actions without executing")
	cmd.Flags().BoolVar(&skipAgents, "skip-agents", false, "skip agent creation")
	cmd.Flags().BoolVar(&skipChannels, "skip-channels", false, "skip channel instance creation")
	cmd.Flags().BoolVar(&skipFiles, "skip-files", false, "skip workspace file copy")
	cmd.Flags().BoolVar(&skipMCP, "skip-mcp", false, "skip MCP server creation")
	cmd.Flags().BoolVar(&skipCrons, "skip-crons", false, "skip cron job creation")

	return cmd
}

func runMigrateOpenClaw(sourceFile, workspaceSource, configSource, cronSource string, dryRun, skipAgents, skipChannels, skipFiles, skipMCP, skipCrons bool) error {
	// 1. Parse OpenClaw config
	slog.Info("migrate-openclaw: parsing source", "file", sourceFile)
	data, err := os.ReadFile(sourceFile)
	if err != nil {
		return fmt.Errorf("read openclaw.json: %w", err)
	}
	var oc ocConfig
	if err := json.Unmarshal(data, &oc); err != nil {
		return fmt.Errorf("parse openclaw.json: %w", err)
	}
	slog.Info("migrate-openclaw: parsed", "agents", len(oc.Agents.List),
		"telegram_accounts", len(oc.Channels.Telegram.Accounts),
		"feishu_accounts", len(oc.Channels.Feishu.Accounts))

	if dryRun {
		printDryRun(oc, workspaceSource, configSource, cronSource, skipAgents, skipChannels, skipFiles, skipMCP, skipCrons)
		return nil
	}

	// 2. Connect to PostgreSQL
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("load goclaw config: %w", err)
	}
	dsn := cfg.Database.PostgresDSN
	if dsn == "" {
		return fmt.Errorf("GOCLAW_POSTGRES_DSN not set")
	}
	encKey := os.Getenv("GOCLAW_ENCRYPTION_KEY")

	db, err := pg.OpenDB(dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	agentStore := pg.NewPGAgentStore(db)
	channelStore := pg.NewPGChannelInstanceStore(db, encKey)
	mcpStore := pg.NewPGMCPServerStore(db, encKey)

	ctx := store.WithCrossTenant(context.Background())

	// Track agent IDs by key
	agentIDByKey := make(map[string]uuid.UUID)

	// Load existing agents
	existingAgents, err := agentStore.List(ctx, "")
	if err != nil {
		slog.Warn("migrate-openclaw: could not list existing agents", "error", err)
	}
	for _, ea := range existingAgents {
		agentIDByKey[ea.AgentKey] = ea.ID
	}

	// 3. Create agents
	if !skipAgents {
		slog.Info("migrate-openclaw: creating agents...")
		for _, a := range oc.Agents.List {
			agentKey := toGocSlug(a.ID)
			if _, exists := agentIDByKey[agentKey]; exists {
				slog.Info("  skip (exists)", "agent_key", agentKey)
				continue
			}

			provider, model := splitProviderModel(a.Model)
			agentType := store.AgentTypePredefined
			if openAgentKeys[a.ID] {
				agentType = store.AgentTypeOpen
			}

			workspace := filepath.Join(cfg.Agents.Defaults.Workspace, agentKey)

			agent := &store.AgentData{
				AgentKey:            agentKey,
				DisplayName:         displayNameForAgent(a),
				OwnerID:             "system",
				Provider:            provider,
				Model:               model,
				ContextWindow:       200000,
				MaxToolIterations:   20,
				Workspace:           workspace,
				RestrictToWorkspace: true,
				AgentType:           agentType,
				IsDefault:           false,
				Status:              store.AgentStatusActive,
				ToolsConfig:         a.Tools,
				SandboxConfig:       a.Sandbox,
				SubagentsConfig:     a.Subagents,
				MemoryConfig:        json.RawMessage(`{"enabled":true}`),
				CompactionConfig:    json.RawMessage(`{}`),
			}

			if err := agentStore.Create(ctx, agent); err != nil {
				slog.Error("  FAILED", "agent_key", agentKey, "error", err)
				continue
			}
			agentIDByKey[agentKey] = agent.ID
			slog.Info("  created", "agent_key", agentKey, "id", agent.ID, "type", agentType)
		}
	}

	// 4. Set context files
	if !skipAgents {
		slog.Info("migrate-openclaw: setting context files...")
		for _, a := range oc.Agents.List {
			agentKey := toGocSlug(a.ID)
			agentID, ok := agentIDByKey[agentKey]
			if !ok {
				continue
			}
			setContextFilesForAgent(ctx, agentStore, agentID, agentKey, a, workspaceSource)
		}
	}

	// 5. Create channel instances
	if !skipChannels {
		slog.Info("migrate-openclaw: creating telegram channel instances...")
		createTelegramInstances(ctx, channelStore, agentIDByKey, oc.Channels.Telegram)

		slog.Info("migrate-openclaw: creating feishu channel instances...")
		createFeishuInstances(ctx, channelStore, agentIDByKey, oc.Channels.Feishu)
	}

	// 6. Create MCP servers
	if !skipMCP {
		slog.Info("migrate-openclaw: creating MCP servers...")
		createMCPServers(ctx, db, mcpStore, agentIDByKey, configSource)
	}

	// 7. Create cron jobs
	if !skipCrons {
		slog.Info("migrate-openclaw: creating cron jobs...")
		cronStore := pg.NewPGCronStore(db)
		createCronJobs(ctx, cronStore, agentIDByKey, cronSource)
	}

	// 8. Copy workspace files
	if !skipFiles {
		slog.Info("migrate-openclaw: copying workspace files...")
		copyWorkspaceFiles(workspaceSource, cfg.Agents.Defaults.Workspace, oc.Agents.List)
	}

	slog.Info("migrate-openclaw: DONE",
		"agents_total", len(agentIDByKey),
		"workspace_source", workspaceSource)

	return nil
}

// ------------------------------------------------------------------
// Context files
// ------------------------------------------------------------------

func setContextFilesForAgent(ctx context.Context, agentStore *pg.PGAgentStore, agentID uuid.UUID, agentKey string, a ocAgent, mainWorkspace string) {
	srcWorkspace := mainWorkspace
	if a.Workspace != "" {
		srcWorkspace = a.Workspace
	} else if a.ID != "main" {
		home, _ := os.UserHomeDir()
		candidate := filepath.Join(home, ".openclaw", "agents", a.ID, "workspace")
		if _, err := os.Stat(candidate); err == nil {
			srcWorkspace = candidate
		}
	}

	// Core context files (always set)
	coreFiles := []string{"SOUL.md", "IDENTITY.md", "AGENTS.md"}
	for _, f := range coreFiles {
		path := filepath.Join(srcWorkspace, f)
		content, err := os.ReadFile(path)
		if err != nil {
			slog.Debug("  context file not found", "agent", agentKey, "file", f)
			continue
		}
		if err := agentStore.SetAgentContextFile(ctx, agentID, f, string(content)); err != nil {
			slog.Error("  FAILED set context file", "agent", agentKey, "file", f, "error", err)
			continue
		}
		slog.Info("  set context file", "agent", agentKey, "file", f, "bytes", len(content))
	}

	// Extra context files (set if they exist)
	extraFiles := []string{"HEARTBEAT.md", "RULES.md", "GROUP_RULES.md", "TEAM.md", "TOOLS.md",
		"QUICK-REF.md", "TELEGRAM_FORMAT.md", "MEMORY.md", "TASKS.md", "PROJECTS.md", "USER.md"}
	for _, f := range extraFiles {
		path := filepath.Join(srcWorkspace, f)
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if err := agentStore.SetAgentContextFile(ctx, agentID, f, string(content)); err != nil {
			slog.Error("  FAILED set extra file", "agent", agentKey, "file", f, "error", err)
			continue
		}
		slog.Info("  set extra file", "agent", agentKey, "file", f, "bytes", len(content))
	}
}

// ------------------------------------------------------------------
// Telegram channel instances
// ------------------------------------------------------------------

func createTelegramInstances(ctx context.Context, ciStore store.ChannelInstanceStore, agentIDs map[string]uuid.UUID, tg ocTelegramChannel) {
	for acctID, acct := range tg.Accounts {
		agentKey, ok := telegramAccountToAgentKey[acctID]
		if !ok {
			slog.Warn("  skip unknown telegram account", "account", acctID)
			continue
		}
		agentID, ok := agentIDs[agentKey]
		if !ok {
			slog.Warn("  skip (agent not found)", "account", acctID, "agent_key", agentKey)
			continue
		}

		instanceName := "telegram"
		if acctID != "default" {
			instanceName = "telegram/" + toGocSlug(acctID)
		}

		if existing, _ := ciStore.GetByName(ctx, instanceName); existing != nil {
			slog.Info("  skip (exists)", "instance", instanceName)
			continue
		}

		creds, _ := json.Marshal(map[string]string{"token": acct.BotToken})

		cfgMap := map[string]any{
			"dm_policy":       acct.DMPolicy,
			"group_policy":    acct.GroupPolicy,
			"require_mention": true,
		}
		if acct.Streaming == "partial" {
			cfgMap["dm_stream"] = true
			cfgMap["group_stream"] = true
		}
		if acct.Groups != nil {
			cfgMap["groups"] = acct.Groups
		}
		cfgJSON, _ := json.Marshal(cfgMap)

		enabled := true
		if acct.Enabled != nil {
			enabled = *acct.Enabled
		}

		inst := &store.ChannelInstanceData{
			Name:        instanceName,
			DisplayName: acct.Name,
			ChannelType: "telegram",
			AgentID:     agentID,
			Credentials: creds,
			Config:      cfgJSON,
			Enabled:     enabled,
			CreatedBy:   "system",
		}

		if err := ciStore.Create(ctx, inst); err != nil {
			slog.Error("  FAILED", "instance", instanceName, "error", err)
			continue
		}
		slog.Info("  created", "instance", instanceName, "agent", agentKey)
	}
}

// ------------------------------------------------------------------
// Feishu channel instances
// ------------------------------------------------------------------

func createFeishuInstances(ctx context.Context, ciStore store.ChannelInstanceStore, agentIDs map[string]uuid.UUID, feishu ocFeishuChannel) {
	for acctID, acct := range feishu.Accounts {
		agentKey, ok := feishuAccountToAgentKey[acctID]
		if !ok {
			slog.Warn("  skip unknown feishu account", "account", acctID)
			continue
		}
		agentID, ok := agentIDs[agentKey]
		if !ok {
			slog.Warn("  skip (agent not found)", "account", acctID, "agent_key", agentKey)
			continue
		}

		instanceName := "feishu/" + toGocSlug(acctID)

		if existing, _ := ciStore.GetByName(ctx, instanceName); existing != nil {
			slog.Info("  skip (exists)", "instance", instanceName)
			continue
		}

		creds, _ := json.Marshal(map[string]string{
			"app_id":     acct.AppID,
			"app_secret": acct.AppSecret,
		})

		domain := acct.Domain
		if domain == "" {
			domain = "lark"
		}
		cfgJSON, _ := json.Marshal(map[string]any{
			"domain":          domain,
			"connection_mode": "websocket",
			"dm_policy":       "pairing",
			"group_policy":    "pairing",
			"require_mention": true,
		})

		inst := &store.ChannelInstanceData{
			Name:        instanceName,
			DisplayName: acct.Name,
			ChannelType: "feishu",
			AgentID:     agentID,
			Credentials: creds,
			Config:      cfgJSON,
			Enabled:     acct.Enabled,
			CreatedBy:   "system",
		}

		if err := ciStore.Create(ctx, inst); err != nil {
			slog.Error("  FAILED", "instance", instanceName, "error", err)
			continue
		}
		slog.Info("  created", "instance", instanceName, "agent", agentKey)
	}
}

// ------------------------------------------------------------------
// MCP servers
// ------------------------------------------------------------------

func createMCPServers(ctx context.Context, db *sql.DB, mcpStore *pg.PGMCPServerStore, agentIDs map[string]uuid.UUID, configSource string) {
	data, err := os.ReadFile(configSource)
	if err != nil {
		slog.Warn("migrate-openclaw: could not read OpenClaw config.json for MCP", "error", err)
		return
	}
	var gwCfg ocGatewayConfig
	if err := json.Unmarshal(data, &gwCfg); err != nil {
		slog.Warn("migrate-openclaw: could not parse OpenClaw config.json", "error", err)
		return
	}

	for name, mcp := range gwCfg.MCPServers {
		argsJSON, _ := json.Marshal(mcp.Args)
		var envJSON json.RawMessage
		if len(mcp.Env) > 0 {
			envJSON, _ = json.Marshal(mcp.Env)
		}

		srv := &store.MCPServerData{
			Name:        name,
			DisplayName: name,
			Transport:   "stdio",
			Command:     mcp.Command,
			Args:        argsJSON,
			Env:         envJSON,
			Enabled:     true,
			TimeoutSec:  60,
			CreatedBy:   "system",
		}

		if err := mcpStore.CreateServer(ctx, srv); err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
				slog.Info("  skip (exists)", "mcp", name)
			} else {
				slog.Error("  FAILED", "mcp", name, "error", err)
			}
			continue
		}
		slog.Info("  created MCP server", "name", name, "id", srv.ID)

		grantMCPToAgents(ctx, db, srv.ID, agentIDs)
	}
}

func grantMCPToAgents(ctx context.Context, db *sql.DB, serverID uuid.UUID, agentIDs map[string]uuid.UUID) {
	mcpAgents := []string{"secondhand-assistant", "gp-pm-tracker", "thainha-assistant", "gearvn-v2", "media-mkt"}
	for _, agentKey := range mcpAgents {
		agentID, ok := agentIDs[agentKey]
		if !ok {
			continue
		}
		grantID := store.GenNewID()
		_, err := db.ExecContext(ctx,
			`INSERT INTO mcp_agent_grants (id, server_id, agent_id, enabled, granted_by, created_at, tenant_id)
			 VALUES ($1, $2, $3, true, 'system', NOW(), $4)
			 ON CONFLICT (server_id, agent_id) DO NOTHING`,
			grantID, serverID, agentID, store.MasterTenantID)
		if err != nil {
			slog.Warn("  grant failed", "server", serverID, "agent", agentKey, "error", err)
		} else {
			slog.Info("  granted MCP access", "agent", agentKey)
		}
	}
}

// ------------------------------------------------------------------
// Cron jobs
// ------------------------------------------------------------------

func createCronJobs(ctx context.Context, cronStore *pg.PGCronStore, agentIDs map[string]uuid.UUID, cronSource string) {
	data, err := os.ReadFile(cronSource)
	if err != nil {
		slog.Warn("migrate-openclaw: could not read cron jobs.json", "error", err)
		return
	}
	var cf ocCronFile
	if err := json.Unmarshal(data, &cf); err != nil {
		slog.Warn("migrate-openclaw: could not parse cron jobs.json", "error", err)
		return
	}

	// Load existing cron jobs to detect duplicates by name.
	existing := cronStore.ListJobs(ctx, true, "", "")
	existingNames := make(map[string]bool, len(existing))
	for _, j := range existing {
		existingNames[j.Name] = true
	}

	created, skipped := 0, 0
	for _, j := range cf.Jobs {
		agentKey := toGocSlug(j.AgentID)
		agentUUID, ok := agentIDs[agentKey]
		if !ok {
			slog.Warn("  skip cron (agent not found)", "name", j.Name, "agent", agentKey)
			skipped++
			continue
		}

		slug := cronNameToSlug(j.Name)
		if slug == "" {
			slog.Warn("  skip cron (empty slug)", "name", j.Name)
			skipped++
			continue
		}
		if existingNames[slug] {
			slog.Info("  skip cron (exists)", "name", slug)
			skipped++
			continue
		}

		schedule := store.CronSchedule{
			Kind: j.Schedule.Kind,
			Expr: j.Schedule.Expr,
			TZ:   j.Schedule.TZ,
		}

		deliver := false
		deliverChannel := ""
		deliverTo := ""
		if j.Delivery != nil && j.Delivery.Mode == "announce" {
			deliver = true
			deliverChannel = resolveDeliverChannel(j.Delivery)
			deliverTo = cleanDeliverTo(j.Delivery.To)
		}

		job, err := cronStore.AddJob(ctx, slug, schedule, j.Payload.Message, deliver, deliverChannel, deliverTo, agentUUID.String(), "system")
		if err != nil {
			slog.Error("  FAILED cron", "name", slug, "error", err)
			continue
		}

		// AddJob creates enabled=true; disable if originally disabled.
		if !j.Enabled {
			if err := cronStore.EnableJob(ctx, job.ID, false); err != nil {
				slog.Warn("  could not disable cron", "name", slug, "error", err)
			}
		}

		slog.Info("  created cron", "name", slug, "agent", agentKey, "id", job.ID, "enabled", j.Enabled)
		existingNames[slug] = true
		created++
	}
	slog.Info("migrate-openclaw: cron jobs done", "created", created, "skipped", skipped)
}

// ------------------------------------------------------------------
// Workspace file copy
// ------------------------------------------------------------------

func copyWorkspaceFiles(srcWorkspace, dstBase string, agents []ocAgent) {
	home, _ := os.UserHomeDir()
	dstBase = strings.Replace(dstBase, "~", home, 1)

	mainDst := filepath.Join(dstBase, "main")
	copyDir(srcWorkspace, mainDst, []string{".git", "sessions", ".DS_Store", ".openclaw"})
	slog.Info("  copied main workspace", "src", srcWorkspace, "dst", mainDst)

	for _, a := range agents {
		if a.ID == "main" {
			continue
		}
		agentKey := toGocSlug(a.ID)
		agentDst := filepath.Join(dstBase, agentKey)

		srcPath := a.Workspace
		if srcPath == "" {
			srcPath = filepath.Join(home, ".openclaw", "agents", a.ID, "workspace")
		}

		if _, err := os.Stat(srcPath); err != nil {
			slog.Debug("  no workspace to copy", "agent", agentKey)
			continue
		}

		copyDir(srcPath, agentDst, []string{".git", "sessions", ".DS_Store"})
		slog.Info("  copied workspace", "agent", agentKey, "src", srcPath, "dst", agentDst)
	}
}

func copyDir(src, dst string, excludes []string) {
	_ = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(src, path)
		for _, ex := range excludes {
			if strings.HasPrefix(rel, ex) || strings.Contains(rel, string(filepath.Separator)+ex) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		dstPath := filepath.Join(dst, rel)
		if d.IsDir() {
			_ = os.MkdirAll(dstPath, 0755)
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		_ = os.MkdirAll(filepath.Dir(dstPath), 0755)
		if err := os.WriteFile(dstPath, content, 0644); err != nil {
			slog.Warn("  copy failed", "src", path, "dst", dstPath, "error", err)
		}
		return nil
	})
}

// ------------------------------------------------------------------
// Dry run
// ------------------------------------------------------------------

func printDryRun(oc ocConfig, workspaceSource, configSource, cronSource string, skipAgents, skipChannels, skipFiles, skipMCP, skipCrons bool) {
	fmt.Println("\n=== MIGRATE-OPENCLAW DRY RUN ===")

	if !skipAgents {
		fmt.Printf("AGENTS (%d):\n", len(oc.Agents.List))
		for _, a := range oc.Agents.List {
			agentKey := toGocSlug(a.ID)
			provider, model := splitProviderModel(a.Model)
			agentType := "predefined"
			if openAgentKeys[a.ID] {
				agentType = "open"
			}
			fmt.Printf("  CREATE agent_key=%s type=%s provider=%s model=%s display=%s\n",
				agentKey, agentType, provider, model, displayNameForAgent(a))
		}
		fmt.Println()
	}

	if !skipChannels {
		fmt.Printf("TELEGRAM INSTANCES (%d):\n", len(oc.Channels.Telegram.Accounts))
		for acctID, acct := range oc.Channels.Telegram.Accounts {
			agentKey := telegramAccountToAgentKey[acctID]
			instanceName := "telegram"
			if acctID != "default" {
				instanceName = "telegram/" + toGocSlug(acctID)
			}
			tokenPreview := acct.BotToken
			if len(tokenPreview) > 20 {
				tokenPreview = tokenPreview[:20]
			}
			fmt.Printf("  CREATE instance=%s agent=%s display=%s token=%s...\n",
				instanceName, agentKey, acct.Name, tokenPreview)
		}
		fmt.Println()

		fmt.Printf("FEISHU INSTANCES (%d):\n", len(oc.Channels.Feishu.Accounts))
		for acctID, acct := range oc.Channels.Feishu.Accounts {
			agentKey := feishuAccountToAgentKey[acctID]
			instanceName := "feishu/" + toGocSlug(acctID)
			fmt.Printf("  CREATE instance=%s agent=%s app_id=%s\n",
				instanceName, agentKey, acct.AppID)
		}
		fmt.Println()
	}

	if !skipMCP {
		data, err := os.ReadFile(configSource)
		if err == nil {
			var gwCfg ocGatewayConfig
			if json.Unmarshal(data, &gwCfg) == nil {
				fmt.Printf("MCP SERVERS (%d):\n", len(gwCfg.MCPServers))
				for name, mcp := range gwCfg.MCPServers {
					fmt.Printf("  CREATE name=%s command=%s args=%v\n", name, mcp.Command, mcp.Args)
				}
				fmt.Println()
			}
		}
	}

	if !skipCrons {
		cronData, err := os.ReadFile(cronSource)
		if err == nil {
			var cf ocCronFile
			if json.Unmarshal(cronData, &cf) == nil {
				fmt.Printf("CRON JOBS (%d):\n", len(cf.Jobs))
				for _, j := range cf.Jobs {
					slug := cronNameToSlug(j.Name)
					agentKey := toGocSlug(j.AgentID)
					enabled := "enabled"
					if !j.Enabled {
						enabled = "disabled"
					}
					deliverCh := resolveDeliverChannel(j.Delivery)
					deliverTo := ""
					if j.Delivery != nil {
						deliverTo = cleanDeliverTo(j.Delivery.To)
					}
					fmt.Printf("  CREATE name=%s agent=%s schedule=%q tz=%s %s deliver=%s→%s\n",
						slug, agentKey, j.Schedule.Expr, j.Schedule.TZ, enabled, deliverCh, deliverTo)
				}
				fmt.Println()
			}
		}
	}

	if !skipFiles {
		fmt.Printf("WORKSPACE FILES:\n")
		fmt.Printf("  COPY %s → ~/.goclaw/workspace/main/\n", workspaceSource)
		for _, a := range oc.Agents.List {
			if a.ID == "main" {
				continue
			}
			agentKey := toGocSlug(a.ID)
			src := a.Workspace
			if src == "" {
				home, _ := os.UserHomeDir()
				src = filepath.Join(home, ".openclaw", "agents", a.ID, "workspace")
			}
			fmt.Printf("  COPY %s → ~/.goclaw/workspace/%s/\n", src, agentKey)
		}
		fmt.Println()
	}

	fmt.Println("=== END DRY RUN (no changes made) ===")
}
