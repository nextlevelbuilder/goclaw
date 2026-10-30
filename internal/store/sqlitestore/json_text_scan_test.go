//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func openJSONScanTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	initSqlx(db)
	return db
}

// tenant_users.metadata defaults to TEXT '{}' and AddUser never sets it, so
// every read path must accept TEXT JSON.
func TestSQLiteTenantUsersReadDefaultTextMetadata(t *testing.T) {
	db := openJSONScanTestDB(t)
	tenantStore := NewSQLiteTenantStore(db)
	ctx := context.Background()

	if err := tenantStore.AddUser(ctx, store.MasterTenantID, "alice", store.TenantRoleMember); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	users, err := tenantStore.ListUsers(ctx, store.MasterTenantID)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 || users[0].UserID != "alice" {
		t.Fatalf("ListUsers = %+v, want alice", users)
	}
	if string(users[0].Metadata) != `{}` {
		t.Fatalf("ListUsers metadata = %q, want {}", users[0].Metadata)
	}

	got, err := tenantStore.GetTenantUser(ctx, users[0].ID)
	if err != nil {
		t.Fatalf("GetTenantUser: %v", err)
	}
	if string(got.Metadata) != `{}` || got.Role != store.TenantRoleMember {
		t.Fatalf("GetTenantUser = %+v", got)
	}

	memberships, err := tenantStore.ListUserTenants(ctx, "alice")
	if err != nil {
		t.Fatalf("ListUserTenants: %v", err)
	}
	if len(memberships) != 1 || memberships[0].TenantID != store.MasterTenantID {
		t.Fatalf("ListUserTenants = %+v", memberships)
	}
}

func TestSQLiteTenantUsersMetadataStorageForms(t *testing.T) {
	db := openJSONScanTestDB(t)
	tenantStore := NewSQLiteTenantStore(db)
	ctx := context.Background()

	cases := []struct {
		name   string
		stored any
		want   string
	}{
		{"empty-text", "", ""},
		{"object-text", `{"k":"text"}`, `{"k":"text"}`},
		{"object-blob", []byte(`{"k":"blob"}`), `{"k":"blob"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userID := "user-" + tc.name
			if err := tenantStore.AddUser(ctx, store.MasterTenantID, userID, store.TenantRoleMember); err != nil {
				t.Fatalf("AddUser: %v", err)
			}
			if _, err := db.Exec(`UPDATE tenant_users SET metadata = ? WHERE user_id = ?`, tc.stored, userID); err != nil {
				t.Fatalf("store metadata: %v", err)
			}
			memberships, err := tenantStore.ListUserTenants(ctx, userID)
			if err != nil {
				t.Fatalf("ListUserTenants: %v", err)
			}
			if len(memberships) != 1 || string(memberships[0].Metadata) != tc.want {
				t.Fatalf("ListUserTenants = %+v, want metadata %q", memberships, tc.want)
			}
			got, err := tenantStore.GetTenantUser(ctx, memberships[0].ID)
			if err != nil {
				t.Fatalf("GetTenantUser: %v", err)
			}
			if string(got.Metadata) != tc.want {
				t.Fatalf("GetTenantUser metadata = %q, want %q", got.Metadata, tc.want)
			}
		})
	}
}

func TestSQLiteCreateTenantUserReturningScansTextMetadata(t *testing.T) {
	db := openJSONScanTestDB(t)
	tenantStore := NewSQLiteTenantStore(db)
	ctx := context.Background()

	created, err := tenantStore.CreateTenantUserReturning(ctx, store.MasterTenantID, "bob", "Bob", store.TenantRoleMember)
	if err != nil {
		t.Fatalf("CreateTenantUserReturning(insert): %v", err)
	}
	if created.UserID != "bob" || created.DisplayName == nil || *created.DisplayName != "Bob" {
		t.Fatalf("CreateTenantUserReturning(insert) = %+v", created)
	}
	if string(created.Metadata) != `{}` {
		t.Fatalf("CreateTenantUserReturning(insert) metadata = %q, want {}", created.Metadata)
	}

	if _, err := db.Exec(`UPDATE tenant_users SET metadata = ? WHERE id = ?`, `{"src":"text"}`, created.ID); err != nil {
		t.Fatalf("store metadata: %v", err)
	}
	// Conflict path: existing row is returned with its stored metadata and
	// the previous display name is preserved.
	again, err := tenantStore.CreateTenantUserReturning(ctx, store.MasterTenantID, "bob", "", store.TenantRoleMember)
	if err != nil {
		t.Fatalf("CreateTenantUserReturning(conflict): %v", err)
	}
	if again.ID != created.ID {
		t.Fatalf("conflict returned id %s, want %s", again.ID, created.ID)
	}
	if string(again.Metadata) != `{"src":"text"}` {
		t.Fatalf("CreateTenantUserReturning(conflict) metadata = %q", again.Metadata)
	}
	if again.DisplayName == nil || *again.DisplayName != "Bob" {
		t.Fatalf("CreateTenantUserReturning(conflict) display name = %v, want Bob", again.DisplayName)
	}
}

func newJSONScanMCPServer(t *testing.T, ctx context.Context, mcpStore *SQLiteMCPServerStore, name string) *store.MCPServerData {
	t.Helper()
	srv := &store.MCPServerData{
		Name:       name,
		Transport:  "stdio",
		Command:    "echo",
		Args:       json.RawMessage(`["hello"]`),
		Headers:    json.RawMessage(`{"X-Test":"1"}`),
		Env:        json.RawMessage(`{"FOO":"bar"}`),
		TimeoutSec: 30,
		Enabled:    true,
		CreatedBy:  "admin",
	}
	if err := mcpStore.CreateServer(ctx, srv); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	return srv
}

// readAllMCPServerPaths exercises every sqlx-backed mcp_servers read path and
// returns the GetServer result.
func readAllMCPServerPaths(t *testing.T, ctx context.Context, mcpStore *SQLiteMCPServerStore, srv *store.MCPServerData) *store.MCPServerData {
	t.Helper()
	got, err := mcpStore.GetServer(ctx, srv.ID)
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	byName, err := mcpStore.GetServerByName(ctx, srv.Name)
	if err != nil {
		t.Fatalf("GetServerByName: %v", err)
	}
	if byName.ID != srv.ID || string(byName.Settings) != string(got.Settings) {
		t.Fatalf("GetServerByName = %+v, want same row as GetServer", byName)
	}
	list, err := mcpStore.ListServers(ctx)
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	found := false
	for _, s := range list {
		if s.ID == srv.ID {
			found = true
			if string(s.Settings) != string(got.Settings) {
				t.Fatalf("ListServers settings = %s, want %s", s.Settings, got.Settings)
			}
		}
	}
	if !found {
		t.Fatalf("ListServers did not return server %s", srv.ID)
	}
	return got
}

func TestSQLiteMCPServerReadsAfterCacheToolDescriptions(t *testing.T) {
	db := openJSONScanTestDB(t)
	mcpStore := NewSQLiteMCPServerStore(db, "")
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	srv := newJSONScanMCPServer(t, ctx, mcpStore, "cache-srv")

	err := mcpStore.CacheToolDescriptions(ctx, srv.ID, map[string]store.CachedToolInfo{
		"echo": {Description: "Echo input", Parameters: json.RawMessage(`{"type":"object"}`)},
	})
	if err != nil {
		t.Fatalf("CacheToolDescriptions: %v", err)
	}

	got := readAllMCPServerPaths(t, ctx, mcpStore, srv)
	var settings map[string]map[string]store.CachedToolInfo
	if err := json.Unmarshal(got.Settings, &settings); err != nil {
		t.Fatalf("unmarshal settings %s: %v", got.Settings, err)
	}
	if settings["tool_cache"]["echo"].Description != "Echo input" {
		t.Fatalf("settings = %s, want tool_cache.echo", got.Settings)
	}
	if string(got.Args) != `["hello"]` || string(got.Headers) != `{"X-Test":"1"}` || string(got.Env) != `{"FOO":"bar"}` {
		t.Fatalf("args/headers/env = %s / %s / %s", got.Args, got.Headers, got.Env)
	}
}

func TestSQLiteMCPServerReadsTextJSONColumns(t *testing.T) {
	db := openJSONScanTestDB(t)
	mcpStore := NewSQLiteMCPServerStore(db, "")
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	srv := newJSONScanMCPServer(t, ctx, mcpStore, "text-srv")

	if _, err := db.Exec(`UPDATE mcp_servers SET args = ?, headers = ?, env = ?, settings = ? WHERE id = ?`,
		`["a","b"]`, `{"H":"text"}`, `{"E":"text"}`, `{"s":"text"}`, srv.ID); err != nil {
		t.Fatalf("store text json: %v", err)
	}
	got := readAllMCPServerPaths(t, ctx, mcpStore, srv)
	if string(got.Args) != `["a","b"]` || string(got.Headers) != `{"H":"text"}` ||
		string(got.Env) != `{"E":"text"}` || string(got.Settings) != `{"s":"text"}` {
		t.Fatalf("text columns = %s / %s / %s / %s", got.Args, got.Headers, got.Env, got.Settings)
	}

	// Map-valued UpdateServer goes through sqliteVal, which stores TEXT.
	if err := mcpStore.UpdateServer(ctx, srv.ID, map[string]any{
		"args":     []string{"x"},
		"settings": map[string]any{"via": "update"},
	}); err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}
	got = readAllMCPServerPaths(t, ctx, mcpStore, srv)
	if string(got.Args) != `["x"]` || !strings.Contains(string(got.Settings), `"via":"update"`) {
		t.Fatalf("after UpdateServer args/settings = %s / %s", got.Args, got.Settings)
	}

	// NULL columns (args/headers/env are nullable) scan as absent.
	if _, err := db.Exec(`UPDATE mcp_servers SET args = NULL, headers = NULL, env = NULL WHERE id = ?`, srv.ID); err != nil {
		t.Fatalf("store null json: %v", err)
	}
	got = readAllMCPServerPaths(t, ctx, mcpStore, srv)
	if got.Args != nil || got.Headers != nil || got.Env != nil {
		t.Fatalf("null columns = %q / %q / %q, want nil", got.Args, got.Headers, got.Env)
	}
}

// Encrypted headers/env are stored as JSON strings; TEXT storage must still
// round-trip through decryptJSON.
func TestSQLiteMCPServerReadsTextEncryptedHeaders(t *testing.T) {
	db := openJSONScanTestDB(t)
	mcpStore := NewSQLiteMCPServerStore(db, "0123456789abcdef0123456789abcdef")
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	srv := newJSONScanMCPServer(t, ctx, mcpStore, "enc-srv")

	if _, err := db.Exec(`UPDATE mcp_servers SET headers = CAST(headers AS TEXT), env = CAST(env AS TEXT) WHERE id = ?`, srv.ID); err != nil {
		t.Fatalf("cast to text: %v", err)
	}
	var headersType string
	if err := db.QueryRow(`SELECT typeof(headers) FROM mcp_servers WHERE id = ?`, srv.ID).Scan(&headersType); err != nil {
		t.Fatalf("typeof: %v", err)
	}
	if headersType != "text" {
		t.Fatalf("headers storage = %s, want text", headersType)
	}
	got := readAllMCPServerPaths(t, ctx, mcpStore, srv)
	if string(got.Headers) != `{"X-Test":"1"}` || string(got.Env) != `{"FOO":"bar"}` {
		t.Fatalf("decrypted headers/env = %s / %s", got.Headers, got.Env)
	}
}
