package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	ChannelScopeTypeChannel = "channel"
	ChannelScopeTypeGroup   = "group"
	ChannelScopeTypeUser    = "user"
	ChannelScopeTypeRole    = "role"
)

// ChannelContextScope identifies the channel-level context used for scoped
// grants and credentials. ChannelInstanceName lets runtime code resolve scope
// without a channel_instances lookup at the call site.
type ChannelContextScope struct {
	ChannelInstanceID   uuid.UUID `json:"channel_instance_id,omitempty"`
	ChannelInstanceName string    `json:"channel_instance_name,omitempty"`
	ScopeType           string    `json:"scope_type"`
	ScopeKey            string    `json:"scope_key"`
}

func (s ChannelContextScope) Valid() bool {
	if s.ScopeType == "" {
		return false
	}
	if s.ChannelInstanceID == uuid.Nil && s.ChannelInstanceName == "" {
		return false
	}
	return s.ScopeType == ChannelScopeTypeChannel || s.ScopeKey != ""
}

func WithChannelContextScope(ctx context.Context, scope ChannelContextScope) context.Context {
	return context.WithValue(ctx, ChannelContextScopeKey, scope)
}

func ChannelContextScopeFromContext(ctx context.Context) (ChannelContextScope, bool) {
	if v, ok := ctx.Value(ChannelContextScopeKey).(ChannelContextScope); ok && v.Valid() {
		return v, true
	}
	if rc := RunContextFromCtx(ctx); rc != nil && rc.ChannelContextScope.Valid() {
		return rc.ChannelContextScope, true
	}
	return ChannelContextScope{}, false
}

// ChannelContextScopeChainFromContext returns context scopes ordered from
// broadest to most specific. Applying the chain in order gives channel
// defaults first, then group/user/role overrides.
func ChannelContextScopeChainFromContext(ctx context.Context) []ChannelContextScope {
	scope, ok := ChannelContextScopeFromContext(ctx)
	if !ok {
		return nil
	}
	if scope.ScopeType == ChannelScopeTypeChannel {
		return []ChannelContextScope{scope}
	}
	channelKey := scope.ChannelInstanceName
	if channelKey == "" && scope.ScopeType == ChannelScopeTypeChannel {
		channelKey = scope.ScopeKey
	}
	if channelKey == "" {
		return []ChannelContextScope{scope}
	}
	channelScope := ChannelContextScope{
		ChannelInstanceID:   scope.ChannelInstanceID,
		ChannelInstanceName: scope.ChannelInstanceName,
		ScopeType:           ChannelScopeTypeChannel,
		ScopeKey:            channelKey,
	}
	return []ChannelContextScope{channelScope, scope}
}

type MCPContextGrant struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	ChannelInstanceID uuid.UUID       `json:"channel_instance_id" db:"channel_instance_id"`
	ScopeType         string          `json:"scope_type" db:"scope_type"`
	ScopeKey          string          `json:"scope_key" db:"scope_key"`
	ServerID          uuid.UUID       `json:"server_id" db:"server_id"`
	Enabled           bool            `json:"enabled" db:"enabled"`
	ToolAllow         json.RawMessage `json:"tool_allow,omitempty" db:"tool_allow"`
	ToolDeny          json.RawMessage `json:"tool_deny,omitempty" db:"tool_deny"`
	ConfigOverrides   json.RawMessage `json:"config_overrides,omitempty" db:"config_overrides"`
	GrantedBy         string          `json:"granted_by" db:"granted_by"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at" db:"updated_at"`
}

type MCPContextCredentials struct {
	ID                uuid.UUID         `json:"id" db:"id"`
	ChannelInstanceID uuid.UUID         `json:"channel_instance_id" db:"channel_instance_id"`
	ScopeType         string            `json:"scope_type" db:"scope_type"`
	ScopeKey          string            `json:"scope_key" db:"scope_key"`
	ServerID          uuid.UUID         `json:"server_id" db:"server_id"`
	APIKey            string            `json:"api_key,omitempty" db:"-"`
	Headers           map[string]string `json:"headers,omitempty" db:"-"`
	Env               map[string]string `json:"env,omitempty" db:"-"`
	CreatedBy         string            `json:"created_by" db:"created_by"`
	CreatedAt         time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at" db:"updated_at"`
}

type SecureCLIContextGrant struct {
	ID                uuid.UUID                            `json:"id" db:"id"`
	ChannelInstanceID uuid.UUID                            `json:"channel_instance_id" db:"channel_instance_id"`
	ScopeType         string                               `json:"scope_type" db:"scope_type"`
	ScopeKey          string                               `json:"scope_key" db:"scope_key"`
	BinaryID          uuid.UUID                            `json:"binary_id" db:"binary_id"`
	Enabled           bool                                 `json:"enabled" db:"enabled"`
	DenyArgs          *json.RawMessage                     `json:"deny_args,omitempty" db:"deny_args"`
	DenyVerbose       *json.RawMessage                     `json:"deny_verbose,omitempty" db:"deny_verbose"`
	TimeoutSeconds    *int                                 `json:"timeout_seconds,omitempty" db:"timeout_seconds"`
	Tips              *string                              `json:"tips,omitempty" db:"tips"`
	EncryptedEnv      []byte                               `json:"-" db:"encrypted_env"`
	EnvKeys           []string                             `json:"env_keys,omitempty" db:"-"`
	Env               map[string]SecureCLIEnvResponseEntry `json:"env,omitempty" db:"-"`
	EnvSet            bool                                 `json:"env_set" db:"-"`
	GrantedBy         string                               `json:"granted_by" db:"granted_by"`
	CreatedAt         time.Time                            `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time                            `json:"updated_at" db:"updated_at"`
}

type SecureCLIContextCredentials struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	ChannelInstanceID uuid.UUID       `json:"channel_instance_id" db:"channel_instance_id"`
	ScopeType         string          `json:"scope_type" db:"scope_type"`
	ScopeKey          string          `json:"scope_key" db:"scope_key"`
	BinaryID          uuid.UUID       `json:"binary_id" db:"binary_id"`
	EncryptedEnv      []byte          `json:"-" db:"encrypted_env"`
	Metadata          json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	CredentialType    *string         `json:"credential_type,omitempty" db:"credential_type"`
	HostScope         *string         `json:"host_scope,omitempty" db:"host_scope"`
	CreatedBy         string          `json:"created_by" db:"created_by"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at" db:"updated_at"`
}

type MCPContextAdminStore interface {
	UpsertContextGrant(ctx context.Context, grant *MCPContextGrant) error
	DeleteContextGrant(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string, serverID uuid.UUID) error
	ListContextGrants(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string) ([]MCPContextGrant, error)
	ListContextGrantsForScope(ctx context.Context, scope ChannelContextScope) ([]MCPContextGrant, error)

	SetContextCredentials(ctx context.Context, creds *MCPContextCredentials) error
	GetContextCredentials(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string, serverID uuid.UUID) (*MCPContextCredentials, error)
	GetContextCredentialsForScope(ctx context.Context, scope ChannelContextScope, serverID uuid.UUID) (*MCPContextCredentials, error)
	DeleteContextCredentials(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string, serverID uuid.UUID) error
	ListContextCredentials(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string) ([]MCPContextCredentials, error)
	ListContextCredentialsForScope(ctx context.Context, scope ChannelContextScope) ([]MCPContextCredentials, error)
}

type SecureCLIContextAdminStore interface {
	UpsertContextGrant(ctx context.Context, grant *SecureCLIContextGrant) error
	DeleteContextGrant(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string, binaryID uuid.UUID) error
	ListContextGrants(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string) ([]SecureCLIContextGrant, error)
	ListContextGrantsForScope(ctx context.Context, scope ChannelContextScope) ([]SecureCLIContextGrant, error)

	SetContextCredentials(ctx context.Context, creds *SecureCLIContextCredentials) error
	GetContextCredentials(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string, binaryID uuid.UUID) (*SecureCLIContextCredentials, error)
	GetContextCredentialsForScope(ctx context.Context, scope ChannelContextScope, binaryID uuid.UUID) (*SecureCLIContextCredentials, error)
	DeleteContextCredentials(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string, binaryID uuid.UUID) error
	ListContextCredentials(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string) ([]SecureCLIContextCredentials, error)
	ListContextCredentialsForScope(ctx context.Context, scope ChannelContextScope) ([]SecureCLIContextCredentials, error)
}
