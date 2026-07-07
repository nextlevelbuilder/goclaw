package mcp

// crud_fakes_test.go holds hand-written in-memory fake stores shared across
// the crud_*_test.go files. Following the convention already used in
// internal/gateway/methods/*_test.go (see hooks_test.go, sessions_test.go):
// each fake embeds the real store interface (nil) so any method the tests
// don't exercise panics loudly instead of silently returning zero values —
// intentional, since a panic means a test started depending on untested
// behavior and should implement it explicitly.

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ---- fakeAgentStore ----

type fakeAgentStore struct {
	store.AgentStore
	byID         map[uuid.UUID]*store.AgentData
	byKey        map[string]*store.AgentData
	contextFiles map[uuid.UUID][]store.AgentContextFileData
	createErr    error
	updateErr    error
	deleteErr    error
	lastUpdate   map[string]any
	lastUpdateID uuid.UUID
	propagateN   int
}

func newFakeAgentStore() *fakeAgentStore {
	return &fakeAgentStore{
		byID:         map[uuid.UUID]*store.AgentData{},
		byKey:        map[string]*store.AgentData{},
		contextFiles: map[uuid.UUID][]store.AgentContextFileData{},
	}
}

func (f *fakeAgentStore) add(ag *store.AgentData) {
	f.byID[ag.ID] = ag
	f.byKey[ag.AgentKey] = ag
}

func (f *fakeAgentStore) Create(_ context.Context, agent *store.AgentData) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.add(agent)
	return nil
}

func (f *fakeAgentStore) GetByID(_ context.Context, id uuid.UUID) (*store.AgentData, error) {
	if ag, ok := f.byID[id]; ok {
		return ag, nil
	}
	return nil, errors.New("agent not found")
}

func (f *fakeAgentStore) GetByKey(_ context.Context, key string) (*store.AgentData, error) {
	if ag, ok := f.byKey[key]; ok {
		return ag, nil
	}
	return nil, errors.New("agent not found")
}

func (f *fakeAgentStore) List(_ context.Context, ownerID string) ([]store.AgentData, error) {
	var out []store.AgentData
	for _, ag := range f.byID {
		if ownerID != "" && ag.OwnerID != ownerID {
			continue
		}
		out = append(out, *ag)
	}
	return out, nil
}

func (f *fakeAgentStore) Update(_ context.Context, id uuid.UUID, updates map[string]any) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	ag, ok := f.byID[id]
	if !ok {
		return errors.New("agent not found")
	}
	f.lastUpdate = updates
	f.lastUpdateID = id
	if v, ok := updates["display_name"].(string); ok {
		ag.DisplayName = v
	}
	if v, ok := updates["status"].(string); ok {
		ag.Status = v
	}
	return nil
}

func (f *fakeAgentStore) Delete(_ context.Context, id uuid.UUID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	ag, ok := f.byID[id]
	if !ok {
		return errors.New("agent not found")
	}
	delete(f.byID, id)
	delete(f.byKey, ag.AgentKey)
	return nil
}

func (f *fakeAgentStore) GetAgentContextFiles(_ context.Context, agentID uuid.UUID) ([]store.AgentContextFileData, error) {
	return f.contextFiles[agentID], nil
}

func (f *fakeAgentStore) SetAgentContextFile(_ context.Context, agentID uuid.UUID, fileName, content string) error {
	files := f.contextFiles[agentID]
	for i, existing := range files {
		if existing.FileName == fileName {
			files[i].Content = content
			f.contextFiles[agentID] = files
			return nil
		}
	}
	f.contextFiles[agentID] = append(files, store.AgentContextFileData{FileName: fileName, Content: content})
	return nil
}

func (f *fakeAgentStore) PropagateContextFile(_ context.Context, _ uuid.UUID, _ string) (int, error) {
	return f.propagateN, nil
}

// ---- fakeSessionStore ----

type fakeSessionStore struct {
	store.SessionStore
	sessions  map[string]*store.SessionData
	deleted   []string
	resetKeys []string
	labels    map[string]string
	metadata  map[string]map[string]string
	history   map[string][]providers.Message
	deleteErr error
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{
		sessions: map[string]*store.SessionData{},
		labels:   map[string]string{},
		metadata: map[string]map[string]string{},
		history:  map[string][]providers.Message{},
	}
}

func (f *fakeSessionStore) add(key string, sess *store.SessionData) {
	f.sessions[key] = sess
	f.history[key] = sess.Messages
}

func (f *fakeSessionStore) Get(_ context.Context, key string) *store.SessionData {
	return f.sessions[key]
}

func (f *fakeSessionStore) List(_ context.Context, _ string) []store.SessionInfo {
	var out []store.SessionInfo
	for k := range f.sessions {
		out = append(out, store.SessionInfo{Key: k})
	}
	return out
}

func (f *fakeSessionStore) GetHistory(_ context.Context, key string) []providers.Message {
	return f.history[key]
}

func (f *fakeSessionStore) AddMessage(_ context.Context, key string, msg providers.Message) {
	f.history[key] = append(f.history[key], msg)
}

func (f *fakeSessionStore) SetLabel(_ context.Context, key, label string) {
	f.labels[key] = label
}

func (f *fakeSessionStore) UpdateMetadata(_ context.Context, _, _, _, _ string) {}

func (f *fakeSessionStore) SetSessionMetadata(_ context.Context, key string, metadata map[string]string) {
	f.metadata[key] = metadata
}

func (f *fakeSessionStore) Delete(_ context.Context, key string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, key)
	delete(f.sessions, key)
	return nil
}

func (f *fakeSessionStore) Reset(_ context.Context, key string) {
	f.resetKeys = append(f.resetKeys, key)
}

func (f *fakeSessionStore) TruncateHistory(_ context.Context, key string, keepLast int) {
	h := f.history[key]
	if len(h) > keepLast {
		f.history[key] = h[len(h)-keepLast:]
	}
}

// ---- fakeSkillStore (+ manage) ----

type fakeSkillStore struct {
	store.SkillStore
	skills      map[string]store.SkillInfo
	bumpCount   int
	updateCalls map[uuid.UUID]map[string]any
	updateErr   error
}

func newFakeSkillStore() *fakeSkillStore {
	return &fakeSkillStore{
		skills:      map[string]store.SkillInfo{},
		updateCalls: map[uuid.UUID]map[string]any{},
	}
}

func (f *fakeSkillStore) ListSkills(_ context.Context) []store.SkillInfo {
	var out []store.SkillInfo
	for _, s := range f.skills {
		out = append(out, s)
	}
	return out
}

func (f *fakeSkillStore) GetSkill(_ context.Context, name string) (*store.SkillInfo, bool) {
	s, ok := f.skills[name]
	if !ok {
		return nil, false
	}
	return &s, true
}

func (f *fakeSkillStore) BumpVersion() {
	f.bumpCount++
}

// fakeSkillManageStore implements store.SkillManageStore (SkillStore +
// management CRUD). It does not embed fakeSkillStore directly: both
// fakeSkillStore and store.SkillManageStore separately embed store.SkillStore
// (nil), and Go forbids ambiguous promoted-method satisfaction of an
// interface — so this type re-implements the read path against its own maps
// instead of delegating to a fakeSkillStore instance.
type fakeSkillManageStore struct {
	store.SkillManageStore
	skills      map[string]store.SkillInfo
	bumpCount   int
	updateCalls map[uuid.UUID]map[string]any
	updateErr   error
}

func newFakeSkillManageStore() *fakeSkillManageStore {
	return &fakeSkillManageStore{
		skills:      map[string]store.SkillInfo{},
		updateCalls: map[uuid.UUID]map[string]any{},
	}
}

func (f *fakeSkillManageStore) ListSkills(_ context.Context) []store.SkillInfo {
	var out []store.SkillInfo
	for _, s := range f.skills {
		out = append(out, s)
	}
	return out
}

func (f *fakeSkillManageStore) GetSkill(_ context.Context, name string) (*store.SkillInfo, bool) {
	s, ok := f.skills[name]
	if !ok {
		return nil, false
	}
	return &s, true
}

func (f *fakeSkillManageStore) BumpVersion() {
	f.bumpCount++
}

func (f *fakeSkillManageStore) UpdateSkill(_ context.Context, id uuid.UUID, updates map[string]any) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updateCalls[id] = updates
	return nil
}

// ---- fakeCronStore ----

type fakeCronStore struct {
	store.CronStore
	jobs        map[string]*store.CronJob
	removeErr   error
	updateErr   error
	runErr      error
	runLogTotal int
}

func newFakeCronStore() *fakeCronStore {
	return &fakeCronStore{jobs: map[string]*store.CronJob{}}
}

func (f *fakeCronStore) AddJob(_ context.Context, name string, schedule store.CronSchedule, message string, deliver bool, channel, to, agentID, userID string) (*store.CronJob, error) {
	job := &store.CronJob{
		ID:       uuid.NewString(),
		Name:     name,
		Schedule: schedule,
		Enabled:  true,
	}
	f.jobs[job.ID] = job
	return job, nil
}

func (f *fakeCronStore) GetJob(_ context.Context, jobID string) (*store.CronJob, bool) {
	j, ok := f.jobs[jobID]
	return j, ok
}

func (f *fakeCronStore) ListJobs(_ context.Context, includeDisabled bool, _, _ string) []store.CronJob {
	var out []store.CronJob
	for _, j := range f.jobs {
		if !includeDisabled && !j.Enabled {
			continue
		}
		out = append(out, *j)
	}
	return out
}

func (f *fakeCronStore) RemoveJob(_ context.Context, jobID string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	if _, ok := f.jobs[jobID]; !ok {
		return errors.New("job not found")
	}
	delete(f.jobs, jobID)
	return nil
}

func (f *fakeCronStore) UpdateJob(_ context.Context, jobID string, patch store.CronJobPatch) (*store.CronJob, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	j, ok := f.jobs[jobID]
	if !ok {
		return nil, errors.New("job not found")
	}
	if patch.Name != "" {
		j.Name = patch.Name
	}
	if patch.Enabled != nil {
		j.Enabled = *patch.Enabled
	}
	return j, nil
}

func (f *fakeCronStore) EnableJob(_ context.Context, jobID string, enabled bool) error {
	j, ok := f.jobs[jobID]
	if !ok {
		return errors.New("job not found")
	}
	j.Enabled = enabled
	return nil
}

func (f *fakeCronStore) RunJob(_ context.Context, jobID string, force bool) (bool, string, error) {
	if f.runErr != nil {
		return false, "", f.runErr
	}
	if _, ok := f.jobs[jobID]; !ok {
		return false, "", errors.New("job not found")
	}
	return true, "", nil
}

func (f *fakeCronStore) GetRunLog(_ context.Context, _ string, _, _ int) ([]store.CronRunLogEntry, int) {
	return nil, f.runLogTotal
}

func (f *fakeCronStore) Status() map[string]any {
	return map[string]any{"jobs": len(f.jobs)}
}

// ---- fakeHookStore (mirrors internal/gateway/methods/hooks_test.go's fakeStore) ----

type fakeHookStore struct {
	created   map[uuid.UUID]hooks.HookConfig
	createErr error
	updateErr error
	deleteErr error
}

func newFakeHookStore() *fakeHookStore {
	return &fakeHookStore{created: map[uuid.UUID]hooks.HookConfig{}}
}

func (f *fakeHookStore) Create(_ context.Context, cfg hooks.HookConfig) (uuid.UUID, error) {
	if f.createErr != nil {
		return uuid.Nil, f.createErr
	}
	id := uuid.New()
	cfg.ID = id
	f.created[id] = cfg
	return id, nil
}

func (f *fakeHookStore) GetByID(_ context.Context, id uuid.UUID) (*hooks.HookConfig, error) {
	if cfg, ok := f.created[id]; ok {
		return &cfg, nil
	}
	return nil, nil
}

func (f *fakeHookStore) List(_ context.Context, _ hooks.ListFilter) ([]hooks.HookConfig, error) {
	out := make([]hooks.HookConfig, 0, len(f.created))
	for _, cfg := range f.created {
		out = append(out, cfg)
	}
	return out, nil
}

func (f *fakeHookStore) Update(_ context.Context, id uuid.UUID, updates map[string]any) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	cfg, ok := f.created[id]
	if !ok {
		return errors.New("not found")
	}
	if v, ok := updates["enabled"].(bool); ok {
		cfg.Enabled = v
	}
	f.created[id] = cfg
	return nil
}

func (f *fakeHookStore) Delete(_ context.Context, id uuid.UUID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.created[id]; !ok {
		return errors.New("not found")
	}
	delete(f.created, id)
	return nil
}

func (f *fakeHookStore) ResolveForEvent(_ context.Context, _ hooks.Event) ([]hooks.HookConfig, error) {
	return nil, nil
}

func (f *fakeHookStore) WriteExecution(_ context.Context, _ hooks.HookExecution) error { return nil }
func (f *fakeHookStore) SetHookAgents(_ context.Context, _ uuid.UUID, _ []uuid.UUID) error {
	return nil
}
func (f *fakeHookStore) GetHookAgents(_ context.Context, _ uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

// ---- fakeHeartbeatStore ----

type fakeHeartbeatStore struct {
	store.HeartbeatStore
	byAgent   map[uuid.UUID]*store.AgentHeartbeat
	upsertErr error
}

func newFakeHeartbeatStore() *fakeHeartbeatStore {
	return &fakeHeartbeatStore{byAgent: map[uuid.UUID]*store.AgentHeartbeat{}}
}

func (f *fakeHeartbeatStore) Get(_ context.Context, agentID uuid.UUID) (*store.AgentHeartbeat, error) {
	if h, ok := f.byAgent[agentID]; ok {
		return h, nil
	}
	return nil, sql.ErrNoRows
}

func (f *fakeHeartbeatStore) Upsert(_ context.Context, hb *store.AgentHeartbeat) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.byAgent[hb.AgentID] = hb
	return nil
}

func (f *fakeHeartbeatStore) ListLogs(_ context.Context, _ uuid.UUID, _, _ int) ([]store.HeartbeatRunLog, int, error) {
	return nil, 0, nil
}

func (f *fakeHeartbeatStore) ListDeliveryTargets(_ context.Context, _ uuid.UUID) ([]store.DeliveryTarget, error) {
	return nil, nil
}

// ---- fakePairingStore ----

type fakePairingStore struct {
	store.PairingStore
	pending    []store.PairingRequestData
	paired     []store.PairedDeviceData
	requestErr error
	approveErr error
	denyErr    error
	revokeErr  error
}

func newFakePairingStore() *fakePairingStore {
	return &fakePairingStore{}
}

func (f *fakePairingStore) RequestPairing(_ context.Context, senderID, channel, chatID, accountID string, _ map[string]string) (string, error) {
	if f.requestErr != nil {
		return "", f.requestErr
	}
	code := "CODE123"
	f.pending = append(f.pending, store.PairingRequestData{Code: code, SenderID: senderID, Channel: channel, ChatID: chatID, AccountID: accountID})
	return code, nil
}

func (f *fakePairingStore) ApprovePairing(_ context.Context, code, approvedBy string) (*store.PairedDeviceData, error) {
	if f.approveErr != nil {
		return nil, f.approveErr
	}
	for i, p := range f.pending {
		if p.Code == code {
			f.pending = append(f.pending[:i], f.pending[i+1:]...)
			dev := store.PairedDeviceData{SenderID: p.SenderID, Channel: p.Channel, ChatID: p.ChatID, PairedBy: approvedBy}
			f.paired = append(f.paired, dev)
			return &dev, nil
		}
	}
	return nil, errors.New("code not found")
}

func (f *fakePairingStore) DenyPairing(_ context.Context, code string) error {
	if f.denyErr != nil {
		return f.denyErr
	}
	for i, p := range f.pending {
		if p.Code == code {
			f.pending = append(f.pending[:i], f.pending[i+1:]...)
			return nil
		}
	}
	return errors.New("code not found")
}

func (f *fakePairingStore) RevokePairing(_ context.Context, senderID, channel string) error {
	if f.revokeErr != nil {
		return f.revokeErr
	}
	for i, p := range f.paired {
		if p.SenderID == senderID && p.Channel == channel {
			f.paired = append(f.paired[:i], f.paired[i+1:]...)
			return nil
		}
	}
	return errors.New("not paired")
}

func (f *fakePairingStore) IsPaired(_ context.Context, senderID, channel string) (bool, error) {
	for _, p := range f.paired {
		if p.SenderID == senderID && p.Channel == channel {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakePairingStore) ListPending(_ context.Context) []store.PairingRequestData {
	return f.pending
}

func (f *fakePairingStore) ListPaired(_ context.Context) []store.PairedDeviceData {
	return f.paired
}

// ---- fakeTeamStore ----

type fakeTeamStore struct {
	store.TeamStore
	teams     map[uuid.UUID]*store.TeamData
	members   map[uuid.UUID][]store.TeamMemberData
	createErr error
	getErr    error
	deleteErr error
}

func newFakeTeamStore() *fakeTeamStore {
	return &fakeTeamStore{
		teams:   map[uuid.UUID]*store.TeamData{},
		members: map[uuid.UUID][]store.TeamMemberData{},
	}
}

func (f *fakeTeamStore) CreateTeam(_ context.Context, team *store.TeamData) error {
	if f.createErr != nil {
		return f.createErr
	}
	if team.ID == uuid.Nil {
		team.ID = uuid.New()
	}
	f.teams[team.ID] = team
	return nil
}

func (f *fakeTeamStore) GetTeam(_ context.Context, teamID uuid.UUID) (*store.TeamData, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	t, ok := f.teams[teamID]
	if !ok {
		return nil, errors.New("team not found")
	}
	return t, nil
}

func (f *fakeTeamStore) ListTeams(_ context.Context) ([]store.TeamData, error) {
	var out []store.TeamData
	for _, t := range f.teams {
		out = append(out, *t)
	}
	return out, nil
}

func (f *fakeTeamStore) DeleteTeam(_ context.Context, teamID uuid.UUID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.teams[teamID]; !ok {
		return errors.New("team not found")
	}
	delete(f.teams, teamID)
	return nil
}

func (f *fakeTeamStore) UpdateTeam(_ context.Context, teamID uuid.UUID, updates map[string]any) error {
	t, ok := f.teams[teamID]
	if !ok {
		return errors.New("team not found")
	}
	if v, ok := updates["name"].(string); ok {
		t.Name = v
	}
	return nil
}

func (f *fakeTeamStore) AddMember(_ context.Context, teamID, agentID uuid.UUID, role string) error {
	f.members[teamID] = append(f.members[teamID], store.TeamMemberData{TeamID: teamID, AgentID: agentID, Role: role})
	return nil
}

func (f *fakeTeamStore) RemoveMember(_ context.Context, teamID, agentID uuid.UUID) error {
	members := f.members[teamID]
	for i, m := range members {
		if m.AgentID == agentID {
			f.members[teamID] = append(members[:i], members[i+1:]...)
			return nil
		}
	}
	return errors.New("member not found")
}

func (f *fakeTeamStore) ListMembers(_ context.Context, teamID uuid.UUID) ([]store.TeamMemberData, error) {
	return f.members[teamID], nil
}

func (f *fakeTeamStore) GetTeamForAgent(_ context.Context, agentID uuid.UUID) (*store.TeamData, error) {
	for _, t := range f.teams {
		if t.LeadAgentID == agentID {
			return t, nil
		}
	}
	return nil, nil
}

func (f *fakeTeamStore) KnownUserIDs(_ context.Context, _ uuid.UUID, _ int) ([]string, error) {
	return nil, nil
}

func (f *fakeTeamStore) ListTaskScopes(_ context.Context, _ uuid.UUID) ([]store.ScopeEntry, error) {
	return nil, nil
}

func (f *fakeTeamStore) ListTeamEvents(_ context.Context, _ uuid.UUID, _, _ int) ([]store.TeamTaskEventData, error) {
	return nil, nil
}

// ---- fakeChannelInstanceStore ----

type fakeChannelInstanceStore struct {
	store.ChannelInstanceStore
	byID      map[uuid.UUID]*store.ChannelInstanceData
	createErr error
	deleteErr error
}

func newFakeChannelInstanceStore() *fakeChannelInstanceStore {
	return &fakeChannelInstanceStore{byID: map[uuid.UUID]*store.ChannelInstanceData{}}
}

func (f *fakeChannelInstanceStore) Create(_ context.Context, inst *store.ChannelInstanceData) error {
	if f.createErr != nil {
		return f.createErr
	}
	if inst.ID == uuid.Nil {
		inst.ID = uuid.New()
	}
	f.byID[inst.ID] = inst
	return nil
}

func (f *fakeChannelInstanceStore) Get(_ context.Context, id uuid.UUID) (*store.ChannelInstanceData, error) {
	if inst, ok := f.byID[id]; ok {
		return inst, nil
	}
	return nil, errors.New("instance not found")
}

func (f *fakeChannelInstanceStore) Update(_ context.Context, id uuid.UUID, updates map[string]any) error {
	inst, ok := f.byID[id]
	if !ok {
		return errors.New("instance not found")
	}
	if v, ok := updates["enabled"].(bool); ok {
		inst.Enabled = v
	}
	return nil
}

func (f *fakeChannelInstanceStore) Delete(_ context.Context, id uuid.UUID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.byID[id]; !ok {
		return errors.New("instance not found")
	}
	delete(f.byID, id)
	return nil
}

func (f *fakeChannelInstanceStore) ListAll(_ context.Context) ([]store.ChannelInstanceData, error) {
	var out []store.ChannelInstanceData
	for _, inst := range f.byID {
		out = append(out, *inst)
	}
	return out, nil
}

// ---- fakeProviderStore ----

type fakeProviderStore struct {
	store.ProviderStore
	byName map[string]*store.LLMProviderData
}

func newFakeProviderStore() *fakeProviderStore {
	return &fakeProviderStore{byName: map[string]*store.LLMProviderData{}}
}

func (f *fakeProviderStore) GetProviderByName(_ context.Context, name string) (*store.LLMProviderData, error) {
	if p, ok := f.byName[name]; ok {
		return p, nil
	}
	return nil, errors.New("provider not found")
}

// ---- fakeTenantStore ----

type fakeTenantStore struct {
	store.TenantStore
	byID   map[uuid.UUID]*store.TenantData
	bySlug map[string]*store.TenantData
}

func newFakeTenantStore() *fakeTenantStore {
	return &fakeTenantStore{
		byID:   map[uuid.UUID]*store.TenantData{},
		bySlug: map[string]*store.TenantData{},
	}
}

func (f *fakeTenantStore) addTenant(t *store.TenantData) {
	f.byID[t.ID] = t
	f.bySlug[t.Slug] = t
}

func (f *fakeTenantStore) GetTenant(_ context.Context, id uuid.UUID) (*store.TenantData, error) {
	if t, ok := f.byID[id]; ok {
		return t, nil
	}
	return nil, errors.New("tenant not found")
}

func (f *fakeTenantStore) GetTenantBySlug(_ context.Context, slug string) (*store.TenantData, error) {
	if t, ok := f.bySlug[slug]; ok {
		return t, nil
	}
	return nil, errors.New("tenant not found")
}

// ---- fakeChatRunner ----

type fakeChatRunner struct {
	sendResult   *ChatSendResult
	sendErr      error
	abortResult  *ChatAbortResult
	abortErr     error
	statusResult *ChatSessionStatusResult
	statusErr    error
	lastAgentID  string
	lastSessKey  string
	lastMessage  string
}

func (f *fakeChatRunner) Send(_ context.Context, agentID, sessionKey, message string, _ []ChatMediaItem) (*ChatSendResult, error) {
	f.lastAgentID = agentID
	f.lastSessKey = sessionKey
	f.lastMessage = message
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	if f.sendResult != nil {
		return f.sendResult, nil
	}
	return &ChatSendResult{RunID: "run-1", SessionKey: sessionKey, Content: "ok"}, nil
}

func (f *fakeChatRunner) Abort(_ context.Context, _, _ string) (*ChatAbortResult, error) {
	if f.abortErr != nil {
		return nil, f.abortErr
	}
	if f.abortResult != nil {
		return f.abortResult, nil
	}
	return &ChatAbortResult{OK: true}, nil
}

func (f *fakeChatRunner) SessionStatus(_ context.Context, _ string) (*ChatSessionStatusResult, error) {
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	if f.statusResult != nil {
		return f.statusResult, nil
	}
	return &ChatSessionStatusResult{IsRunning: false}, nil
}
