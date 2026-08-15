package methods

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func TestConfigPatchPersistsInboundDebounceMs(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	methods := NewConfigMethods(cfg, cfgPath, nil, nil)
	client, responses := gateway.NewCapturingTestClient(permissions.RoleOwner, store.MasterTenantID, "owner", 1)
	params, err := json.Marshal(map[string]string{
		"raw": `{"gateway":{"inbound_debounce_ms":500}}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	methods.handlePatch(
		store.WithTenantID(context.Background(), store.MasterTenantID),
		client,
		&protocol.RequestFrame{
			Type:   protocol.FrameTypeRequest,
			ID:     "patch-inbound-debounce",
			Method: protocol.MethodConfigPatch,
			Params: params,
		},
	)

	res := readConfigPatchResponse(t, responses)
	if !res.OK {
		t.Fatalf("config.patch failed: %#v", res.Error)
	}
	if cfg.Gateway.InboundDebounceMs != 500 {
		t.Fatalf("in-memory inbound_debounce_ms = %d, want 500", cfg.Gateway.InboundDebounceMs)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"inbound_debounce_ms": 500`)) {
		t.Fatalf("saved config missing inbound_debounce_ms=500:\n%s", data)
	}
}

func TestConfigPatchPersistsShellDenyGroupsFalse(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	methods := NewConfigMethods(cfg, cfgPath, nil, nil)
	client, responses := gateway.NewCapturingTestClient(permissions.RoleOwner, store.MasterTenantID, "owner", 1)
	params, err := json.Marshal(map[string]string{
		"raw": `{"tools":{"shellDenyGroups":{"package_install":false}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	methods.handlePatch(
		ctx,
		client,
		&protocol.RequestFrame{
			Type:   protocol.FrameTypeRequest,
			ID:     "patch-shell-deny-groups",
			Method: protocol.MethodConfigPatch,
			Params: params,
		},
	)

	res := readConfigPatchResponse(t, responses)
	if !res.OK {
		t.Fatalf("config.patch failed: %#v", res.Error)
	}
	shellDenyGroups := cfg.ShellDenyGroupsSnapshot()
	if v, ok := shellDenyGroups["package_install"]; !ok || v {
		t.Fatalf("in-memory shellDenyGroups package_install = %v (ok=%v), want false", v, ok)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"package_install": false`)) {
		t.Fatalf("saved config missing package_install=false:\n%s", data)
	}

	methods.handleGet(
		ctx,
		client,
		&protocol.RequestFrame{
			Type:   protocol.FrameTypeRequest,
			ID:     "get-shell-deny-groups",
			Method: protocol.MethodConfigGet,
		},
	)
	getRes := readConfigPatchResponse(t, responses)
	if !getRes.OK {
		t.Fatalf("config.get failed: %#v", getRes.Error)
	}
	var payload struct {
		Config struct {
			Tools struct {
				ShellDenyGroups map[string]bool `json:"shellDenyGroups"`
			} `json:"tools"`
		} `json:"config"`
	}
	rawPayload, err := json.Marshal(getRes.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		t.Fatal(err)
	}
	if v, ok := payload.Config.Tools.ShellDenyGroups["package_install"]; !ok || v {
		t.Fatalf("config.get shellDenyGroups package_install = %v (ok=%v), want false", v, ok)
	}
}

func readConfigPatchResponse(t *testing.T, responses <-chan []byte) protocol.ResponseFrame {
	t.Helper()
	select {
	case raw := <-responses:
		var res protocol.ResponseFrame
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatal(err)
		}
		return res
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for config.patch response")
		return protocol.ResponseFrame{}
	}
}
