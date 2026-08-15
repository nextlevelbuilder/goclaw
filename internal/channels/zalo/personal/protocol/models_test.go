package protocol

import (
	"encoding/json"
	"testing"
)

func TestServerInfo_UnmarshalJSON_CorrectSpelling(t *testing.T) {
	raw := `{"settings":{"features":{"socket":{"ping_interval":30000}},"keepalive":{"alway_keepalive":1}}}`

	var si ServerInfo
	if err := json.Unmarshal([]byte(raw), &si); err != nil {
		t.Fatal(err)
	}
	if si.Settings == nil {
		t.Fatal("Settings is nil")
	}
	if si.Settings.Features.Socket.PingInterval != 30000 {
		t.Errorf("PingInterval = %d, want 30000", si.Settings.Features.Socket.PingInterval)
	}
}

func TestServerInfo_UnmarshalJSON_ZaloTypo(t *testing.T) {
	// Zalo sometimes sends "setttings" (3 t's)
	raw := `{"setttings":{"features":{"socket":{"ping_interval":15000}},"keepalive":{"alway_keepalive":0}}}`

	var si ServerInfo
	if err := json.Unmarshal([]byte(raw), &si); err != nil {
		t.Fatal(err)
	}
	if si.Settings == nil {
		t.Fatal("Settings is nil with Zalo typo")
	}
	if si.Settings.Features.Socket.PingInterval != 15000 {
		t.Errorf("PingInterval = %d, want 15000", si.Settings.Features.Socket.PingInterval)
	}
}

func TestServerInfo_UnmarshalJSON_NoSettings(t *testing.T) {
	raw := `{"other_field": "value"}`

	var si ServerInfo
	if err := json.Unmarshal([]byte(raw), &si); err != nil {
		t.Fatal(err)
	}
	if si.Settings != nil {
		t.Error("Settings should be nil when neither key is present")
	}
}

func TestSocketRetryConfig_ArrayTimes(t *testing.T) {
	raw := `{"max": 5, "times": [1000, 2000, 5000]}`

	var cfg SocketRetryConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Max == nil || *cfg.Max != 5 {
		t.Errorf("Max = %v, want 5", cfg.Max)
	}
	if len(cfg.Times) != 3 || cfg.Times[0] != 1000 || cfg.Times[2] != 5000 {
		t.Errorf("Times = %v, want [1000, 2000, 5000]", cfg.Times)
	}
}

func TestSocketRetryConfig_SingleTime(t *testing.T) {
	// Zalo's OneOrMany: "times" can be a single int
	raw := `{"max": 3, "times": 2000}`

	var cfg SocketRetryConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Times) != 1 || cfg.Times[0] != 2000 {
		t.Errorf("Times = %v, want [2000]", cfg.Times)
	}
}

func TestSocketRetryConfig_NoMax(t *testing.T) {
	raw := `{"times": [1000]}`

	var cfg SocketRetryConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Max != nil {
		t.Errorf("Max = %v, want nil", cfg.Max)
	}
	if len(cfg.Times) != 1 {
		t.Errorf("Times = %v", cfg.Times)
	}
}

func TestResponse_Generic(t *testing.T) {
	raw := `{"error_code": 0, "error_message": "", "data": {"uid": "123"}}`

	var resp Response[struct {
		UID string `json:"uid"`
	}]
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ErrorCode != 0 {
		t.Errorf("ErrorCode = %d", resp.ErrorCode)
	}
	if resp.Data.UID != "123" {
		t.Errorf("Data.UID = %q", resp.Data.UID)
	}
}

func TestResponse_ErrorCode(t *testing.T) {
	raw := `{"error_code": -13, "error_message": "QR login declined", "data": null}`

	var resp Response[*struct{}]
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ErrorCode != -13 {
		t.Errorf("ErrorCode = %d, want -13", resp.ErrorCode)
	}
	if resp.ErrorMessage != "QR login declined" {
		t.Errorf("ErrorMessage = %q", resp.ErrorMessage)
	}
}

func TestLoginInfo_Unmarshal(t *testing.T) {
	raw := `{
		"uid": "12345",
		"zpw_enk": "base64key==",
		"zpw_ws": ["wss://ws1.zalo.me", "wss://ws2.zalo.me"],
		"zpw_service_map_v3": {
			"chat": ["https://chat1.zalo.me"],
			"group": ["https://group1.zalo.me"],
			"file": ["https://file1.zalo.me"]
		}
	}`

	var li LoginInfo
	if err := json.Unmarshal([]byte(raw), &li); err != nil {
		t.Fatal(err)
	}
	if li.UID != "12345" {
		t.Errorf("UID = %q", li.UID)
	}
	if li.ZPWEnk != "base64key==" {
		t.Errorf("ZPWEnk = %q", li.ZPWEnk)
	}
	if len(li.ZpwWebsocket) != 2 {
		t.Errorf("ZpwWebsocket len = %d", len(li.ZpwWebsocket))
	}
	if len(li.ZpwServiceMapV3.Chat) != 1 {
		t.Errorf("Chat URLs len = %d", len(li.ZpwServiceMapV3.Chat))
	}
}

func TestQRGeneratedData_Unmarshal(t *testing.T) {
	raw := `{"code": "abc123", "image": "data:image/png;base64,iVBOR..."}`

	var qr QRGeneratedData
	if err := json.Unmarshal([]byte(raw), &qr); err != nil {
		t.Fatal(err)
	}
	if qr.Code != "abc123" {
		t.Errorf("Code = %q", qr.Code)
	}
	if qr.Image == "" {
		t.Error("Image is empty")
	}
}
