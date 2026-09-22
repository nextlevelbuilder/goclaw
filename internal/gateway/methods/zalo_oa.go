package methods

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/hkdf"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	zalooa "github.com/nextlevelbuilder/goclaw/internal/channels/zalo/oa"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const (
	zaloOAStateTTL         = 10 * time.Minute
	zaloOAMaxStatesPerInst = 5 // most-recent-N consent attempts per instance
	zaloOAStateHKDFInfo    = "zalo-oa.oauth_state.v1"
)

// ZaloOAMethods serves the WS handlers backing the paste-code consent flow.
//
// Consent state is process-local: HKDF-signed tokens bind tenant+instance,
// and a bounded in-memory nonce map enforces single use. Multi-replica
// gateways cannot share that nonce table — operators must complete consent
// against the same process that minted the state.
type ZaloOAMethods struct {
	store  store.ChannelInstanceStore
	msgBus *bus.MessageBus

	// publicURL returns the gateway's observed public base URL (may be "").
	publicURL func() string

	stateMu sync.Mutex
	states  map[string]zaloOAStateEntry // key: instanceID|nonce
}

type zaloOAStateEntry struct {
	instID    uuid.UUID
	expiresAt time.Time
}

type zaloOAStatePayload struct {
	InstanceID string `json:"i"`
	TenantID   string `json:"t"`
	Nonce      string `json:"n"`
	ExpiresAt  int64  `json:"e"`
}

// ZaloOAFlowError carries the stable protocol code and localized message for
// both WebSocket RPC and authenticated HTTP callers.
type ZaloOAFlowError struct {
	Code    string
	Message string
}

func (e *ZaloOAFlowError) Error() string { return e.Message }

type ZaloOAConsentResult struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

type ZaloOACallbackResult struct {
	URL string `json:"url"`
}

type ZaloOAExchangeResult struct {
	OK        bool      `json:"ok"`
	OAID      string    `json:"oa_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Message   string    `json:"message"`
}

// newOAClient builds the OAuth client for code exchange. A var so tests can
// substitute a client pointed at a local token-endpoint fixture.
var newOAClient = func() *zalooa.Client { return zalooa.NewClient(15 * time.Second) }

// NewZaloOAMethods constructs the handler. msgBus may be nil during tests.
func NewZaloOAMethods(s store.ChannelInstanceStore, msgBus *bus.MessageBus) *ZaloOAMethods {
	return &ZaloOAMethods{
		store:  s,
		msgBus: msgBus,
		states: make(map[string]zaloOAStateEntry),
	}
}

// SetPublicURL wires the gateway's public-URL snapshot so callback URLs can
// be auto-generated instead of hand-configured.
func (m *ZaloOAMethods) SetPublicURL(fn func() string) { m.publicURL = fn }

// Register wires the methods into the WS router.
func (m *ZaloOAMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodChannelInstancesZaloOAConsentURL, m.handleConsentURL)
	router.Register(protocol.MethodChannelInstancesZaloOAExchangeCode, m.handleExchangeCode)
	router.Register(protocol.MethodChannelInstancesZaloOACallbackURL, m.handleCallbackURL)
}

// handleConsentURL builds the Zalo authorization URL server-side so the
// frontend doesn't have to assemble the OAuth URL itself; the response
// only echoes the URL plus a state token.
func (m *ZaloOAMethods) handleConsentURL(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		InstanceID string `json:"instance_id"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}
	result, flowErr := m.ConsentURL(ctx, client.TenantID(), params.InstanceID)
	if flowErr != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, flowErr.Code, flowErr.Message))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"url":   result.URL,
		"state": result.State,
	}))
}

// resolveRedirectURI returns the effective redirect URI for an instance:
// the stored value wins (it matches what the operator registered on the
// Zalo console); otherwise auto-generate from the observed public URL.
func (m *ZaloOAMethods) resolveRedirectURI(stored string) string {
	if stored != "" {
		return stored
	}
	if m.publicURL == nil {
		return ""
	}
	url, _ := zalooa.CallbackURL(m.publicURL())
	return url
}

// handleCallbackURL returns the redirect/callback URL the operator must
// register on the Zalo dev console. Auto-generated from the gateway's
// public URL when the instance has no stored redirect_uri.
func (m *ZaloOAMethods) handleCallbackURL(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		InstanceID string `json:"instance_id"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}
	result, flowErr := m.CallbackURL(ctx, client.TenantID(), params.InstanceID)
	if flowErr != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, flowErr.Code, flowErr.Message))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"url": result.URL}))
}

// handleExchangeCode swaps the pasted authorization code for tokens and
// persists them via the store-encrypted credentials blob.
func (m *ZaloOAMethods) handleExchangeCode(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		InstanceID string `json:"instance_id"`
		Code       string `json:"code"`
		State      string `json:"state"`
		OAID       string `json:"oa_id"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}
	result, flowErr := m.ExchangeCode(ctx, client.TenantID(), params.InstanceID, params.Code, params.State, params.OAID)
	if flowErr != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, flowErr.Code, flowErr.Message))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":         result.OK,
		"oa_id":      result.OAID,
		"expires_at": result.ExpiresAt,
		"message":    result.Message,
	}))
}

// ConsentURL starts a tenant-scoped, single-use Zalo OA consent attempt.
func (m *ZaloOAMethods) ConsentURL(ctx context.Context, tenantID uuid.UUID, instanceID string) (ZaloOAConsentResult, *ZaloOAFlowError) {
	locale := store.LocaleFromContext(ctx)
	instID, err := uuid.Parse(instanceID)
	if err != nil {
		return ZaloOAConsentResult{}, &ZaloOAFlowError{Code: protocol.ErrInvalidRequest, Message: i18n.T(locale, i18n.MsgInvalidID, "instance")}
	}
	inst, err := m.store.Get(ctx, instID)
	if err != nil {
		return ZaloOAConsentResult{}, &ZaloOAFlowError{Code: protocol.ErrNotFound, Message: i18n.T(locale, i18n.MsgInstanceNotFound)}
	}
	if inst.TenantID != tenantID {
		slog.Warn("security.cross_tenant_access_attempt",
			"method", "zalo_oa.consent_url",
			"instance_id", instID,
			"instance_tenant_id", inst.TenantID,
			"client_tenant_id", tenantID)
		return ZaloOAConsentResult{}, &ZaloOAFlowError{Code: protocol.ErrNotFound, Message: i18n.T(locale, i18n.MsgInstanceNotFound)}
	}
	if inst.ChannelType != channels.TypeZaloOA {
		return ZaloOAConsentResult{}, &ZaloOAFlowError{Code: protocol.ErrInvalidRequest, Message: i18n.T(locale, i18n.MsgZaloOAInvalidChannelType)}
	}
	creds, err := zalooa.LoadCreds(inst.Credentials)
	if err != nil || creds.AppID == "" {
		return ZaloOAConsentResult{}, &ZaloOAFlowError{Code: protocol.ErrInvalidRequest, Message: i18n.T(locale, i18n.MsgZaloOAMissingAppID)}
	}
	redirectURI := m.resolveRedirectURI(creds.RedirectURI)
	if redirectURI == "" {
		return ZaloOAConsentResult{}, &ZaloOAFlowError{Code: protocol.ErrInvalidRequest, Message: i18n.T(locale, i18n.MsgZaloOARedirectURIRequired)}
	}
	state, err := m.newConsentState(instID, tenantID, creds.SecretKey)
	if err != nil {
		return ZaloOAConsentResult{}, &ZaloOAFlowError{Code: protocol.ErrInternal, Message: i18n.T(locale, i18n.MsgZaloOAStateGenFailed)}
	}
	return ZaloOAConsentResult{
		URL:   zalooa.ConsentURL(creds.AppID, redirectURI, state),
		State: state,
	}, nil
}

// CallbackURL returns the tenant-scoped redirect URI registered for an OA.
func (m *ZaloOAMethods) CallbackURL(ctx context.Context, tenantID uuid.UUID, instanceID string) (ZaloOACallbackResult, *ZaloOAFlowError) {
	locale := store.LocaleFromContext(ctx)
	instID, err := uuid.Parse(instanceID)
	if err != nil {
		return ZaloOACallbackResult{}, &ZaloOAFlowError{Code: protocol.ErrInvalidRequest, Message: i18n.T(locale, i18n.MsgInvalidID, "instance")}
	}
	inst, err := m.store.Get(ctx, instID)
	if err != nil {
		return ZaloOACallbackResult{}, &ZaloOAFlowError{Code: protocol.ErrNotFound, Message: i18n.T(locale, i18n.MsgInstanceNotFound)}
	}
	if inst.TenantID != tenantID {
		slog.Warn("security.cross_tenant_access_attempt",
			"method", "zalo_oa.callback_url",
			"instance_id", instID,
			"instance_tenant_id", inst.TenantID,
			"client_tenant_id", tenantID)
		return ZaloOACallbackResult{}, &ZaloOAFlowError{Code: protocol.ErrNotFound, Message: i18n.T(locale, i18n.MsgInstanceNotFound)}
	}
	if inst.ChannelType != channels.TypeZaloOA {
		return ZaloOACallbackResult{}, &ZaloOAFlowError{Code: protocol.ErrInvalidRequest, Message: i18n.T(locale, i18n.MsgZaloOAInvalidChannelType)}
	}
	creds, err := zalooa.LoadCreds(inst.Credentials)
	if err != nil {
		creds = &zalooa.ChannelCreds{}
	}
	url := m.resolveRedirectURI(creds.RedirectURI)
	if url == "" {
		return ZaloOACallbackResult{}, &ZaloOAFlowError{Code: protocol.ErrUnavailable, Message: i18n.T(locale, i18n.MsgZaloOACallbackUnavailable)}
	}
	return ZaloOACallbackResult{URL: url}, nil
}

// ExchangeCode consumes one consent state, exchanges the OAuth code, and
// persists encrypted tokens for the tenant-scoped OA instance.
func (m *ZaloOAMethods) ExchangeCode(ctx context.Context, tenantID uuid.UUID, instanceID, code, state, oaID string) (ZaloOAExchangeResult, *ZaloOAFlowError) {
	locale := store.LocaleFromContext(ctx)
	if len(instanceID) > 256 || len(code) > 2048 || len(oaID) > 256 || len(state) > 2048 {
		return ZaloOAExchangeResult{}, &ZaloOAFlowError{Code: protocol.ErrInvalidRequest, Message: i18n.T(locale, i18n.MsgInvalidRequest, "param too long")}
	}
	instID, err := uuid.Parse(instanceID)
	if err != nil {
		return ZaloOAExchangeResult{}, &ZaloOAFlowError{Code: protocol.ErrInvalidRequest, Message: i18n.T(locale, i18n.MsgInvalidID, "instance")}
	}
	if code == "" {
		return ZaloOAExchangeResult{}, &ZaloOAFlowError{Code: protocol.ErrInvalidRequest, Message: i18n.T(locale, i18n.MsgRequired, "code")}
	}
	inst, err := m.store.Get(ctx, instID)
	if err != nil {
		return ZaloOAExchangeResult{}, &ZaloOAFlowError{Code: protocol.ErrNotFound, Message: i18n.T(locale, i18n.MsgInstanceNotFound)}
	}
	if inst.TenantID != tenantID {
		slog.Warn("security.cross_tenant_access_attempt",
			"method", "zalo_oa.exchange_code",
			"instance_id", instID,
			"instance_tenant_id", inst.TenantID,
			"client_tenant_id", tenantID)
		return ZaloOAExchangeResult{}, &ZaloOAFlowError{Code: protocol.ErrNotFound, Message: i18n.T(locale, i18n.MsgInstanceNotFound)}
	}
	if inst.ChannelType != channels.TypeZaloOA {
		return ZaloOAExchangeResult{}, &ZaloOAFlowError{Code: protocol.ErrInvalidRequest, Message: i18n.T(locale, i18n.MsgZaloOAInvalidChannelType)}
	}
	creds, err := zalooa.LoadCreds(inst.Credentials)
	if err != nil {
		return ZaloOAExchangeResult{}, &ZaloOAFlowError{Code: protocol.ErrInternal, Message: i18n.T(locale, i18n.MsgZaloOACodeExchangeFailed, err.Error())}
	}
	stateValid, err := m.consumeConsentState(instID, tenantID, creds.SecretKey, state)
	if err != nil {
		slog.Error("zalo_oa.state_consume_failed", "instance_id", instID, "error", err)
		return ZaloOAExchangeResult{}, &ZaloOAFlowError{Code: protocol.ErrInternal, Message: i18n.T(locale, i18n.MsgZaloOACodeExchangeFailed, "state store unavailable")}
	}
	if !stateValid {
		return ZaloOAExchangeResult{}, &ZaloOAFlowError{Code: protocol.ErrInvalidRequest, Message: i18n.T(locale, i18n.MsgZaloOAInvalidState)}
	}
	tok, err := newOAClient().ExchangeCode(ctx, creds.AppID, creds.SecretKey, code)
	if err != nil {
		slog.Warn("zalo_oa.exchange_failed", "instance_id", instID, "oa_id", creds.OAID, "error", err)
		return ZaloOAExchangeResult{}, &ZaloOAFlowError{Code: protocol.ErrInternal, Message: i18n.T(locale, i18n.MsgZaloOACodeExchangeFailed, err.Error())}
	}
	creds.WithTokens(tok)
	if oaID != "" {
		if creds.OAID != "" && creds.OAID != oaID {
			slog.Warn("zalo_oa.oaid_mismatch_rejected",
				"instance_id", instID, "bound_oa_id", creds.OAID, "pasted_oa_id", oaID)
			return ZaloOAExchangeResult{}, &ZaloOAFlowError{Code: protocol.ErrInvalidRequest, Message: i18n.T(locale, i18n.MsgZaloOAOAIDMismatch)}
		}
		creds.OAID = oaID
	}
	credsBytes, err := creds.Marshal()
	if err != nil {
		return ZaloOAExchangeResult{}, &ZaloOAFlowError{Code: protocol.ErrInternal, Message: i18n.T(locale, i18n.MsgZaloOACodeExchangeFailed, err.Error())}
	}
	if err := m.store.Update(ctx, instID, map[string]any{"credentials": credsBytes}); err != nil {
		return ZaloOAExchangeResult{}, &ZaloOAFlowError{Code: protocol.ErrInternal, Message: i18n.T(locale, i18n.MsgZaloOACodeExchangeFailed, err.Error())}
	}
	m.emitCacheInvalidate()
	slog.Info("zalo_oa.connected",
		"instance_id", instID,
		"oa_id", creds.OAID,
		"expires_at", tok.ExpiresAt,
		"refresh_expires_at", tok.RefreshTokenExpiresAt)
	return ZaloOAExchangeResult{
		OK:        true,
		OAID:      creds.OAID,
		ExpiresAt: tok.ExpiresAt,
		Message:   i18n.T(locale, i18n.MsgZaloOAConnected, creds.OAID),
	}, nil
}

func (m *ZaloOAMethods) emitCacheInvalidate() {
	if m.msgBus == nil {
		return
	}
	m.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindChannelInstances},
	})
}

func (m *ZaloOAMethods) newConsentState(instID, tenantID uuid.UUID, signingKey string) (string, error) {
	nonce, err := newStateToken()
	if err != nil {
		return "", err
	}
	if signingKey == "" {
		return "", fmt.Errorf("secret_key is required")
	}
	macKey, err := deriveZaloOAStateHMACKey(signingKey, instID)
	if err != nil {
		return "", err
	}
	state, err := encodeZaloOAState(zaloOAStatePayload{
		InstanceID: instID.String(),
		TenantID:   tenantID.String(),
		Nonce:      nonce,
		ExpiresAt:  time.Now().Add(zaloOAStateTTL).Unix(),
	}, macKey)
	if err != nil {
		return "", err
	}
	m.putState(instID, nonce)
	return state, nil
}

func (m *ZaloOAMethods) consumeConsentState(instID, tenantID uuid.UUID, signingKey, state string) (bool, error) {
	payload, err := decodeZaloOAStateHKDF(state, signingKey, instID)
	if err != nil || payload.InstanceID != instID.String() || payload.TenantID != tenantID.String() {
		slog.Warn("zalo_oa.state_invalid", "instance_id", instID)
		return false, nil
	}
	return m.consumeState(instID, payload.Nonce), nil
}

func deriveZaloOAStateHMACKey(secretKey string, instID uuid.UUID) ([]byte, error) {
	if secretKey == "" {
		return nil, fmt.Errorf("secret_key is required")
	}
	r := hkdf.New(sha256.New, []byte(secretKey), instID[:], []byte(zaloOAStateHKDFInfo))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}

func decodeZaloOAStateHKDF(state, secretKey string, instID uuid.UUID) (*zaloOAStatePayload, error) {
	macKey, err := deriveZaloOAStateHMACKey(secretKey, instID)
	if err != nil {
		return nil, err
	}
	return decodeZaloOAState(state, macKey)
}

func encodeZaloOAState(payload zaloOAStatePayload, signingKey []byte) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, signingKey)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func decodeZaloOAState(state string, signingKey []byte) (*zaloOAStatePayload, error) {
	idx := strings.LastIndexByte(state, '.')
	if idx <= 0 || len(signingKey) == 0 {
		return nil, fmt.Errorf("malformed state")
	}
	encoded, signature := state[:idx], state[idx+1:]
	got, err := hex.DecodeString(signature)
	if err != nil {
		return nil, fmt.Errorf("decode state signature: %w", err)
	}
	mac := hmac.New(sha256.New, signingKey)
	_, _ = mac.Write([]byte(encoded))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return nil, fmt.Errorf("state signature mismatch")
	}
	body, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode state payload: %w", err)
	}
	var payload zaloOAStatePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal state payload: %w", err)
	}
	if payload.InstanceID == "" || payload.TenantID == "" || payload.Nonce == "" || time.Now().Unix() >= payload.ExpiresAt {
		return nil, fmt.Errorf("state payload invalid or expired")
	}
	return &payload, nil
}

// putState records a freshly minted nonce with a 10min TTL. Caps pending
// entries per instance to bound memory abuse from an operator repeatedly
// clicking "Connect" without ever pasting the code.
func (m *ZaloOAMethods) putState(instID uuid.UUID, state string) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	m.gcStatesLocked()
	m.evictOldestForInstanceLocked(instID, zaloOAMaxStatesPerInst-1)
	m.states[stateKey(instID, state)] = zaloOAStateEntry{
		instID:    instID,
		expiresAt: time.Now().Add(zaloOAStateTTL),
	}
}

// evictOldestForInstanceLocked drops oldest-by-expiry entries for instID
// until at most `keep` remain. Caller MUST hold m.stateMu.
func (m *ZaloOAMethods) evictOldestForInstanceLocked(instID uuid.UUID, keep int) {
	type kv struct {
		key string
		exp time.Time
	}
	var entries []kv
	for k, v := range m.states {
		if v.instID == instID {
			entries = append(entries, kv{k, v.expiresAt})
		}
	}
	if len(entries) <= keep {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].exp.Before(entries[j].exp) })
	for i := 0; i < len(entries)-keep; i++ {
		delete(m.states, entries[i].key)
	}
}

// consumeState atomically validates+removes a nonce. Returns false if
// missing or expired.
func (m *ZaloOAMethods) consumeState(instID uuid.UUID, state string) bool {
	if state == "" {
		return false
	}
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	key := stateKey(instID, state)
	entry, ok := m.states[key]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(m.states, key) // GC the expired entry too
		return false
	}
	delete(m.states, key)
	return true
}

func (m *ZaloOAMethods) gcStatesLocked() {
	now := time.Now()
	for k, v := range m.states {
		if now.After(v.expiresAt) {
			delete(m.states, k)
		}
	}
}

func stateKey(instID uuid.UUID, state string) string {
	return fmt.Sprintf("%s|%s", instID, state)
}

func newStateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
