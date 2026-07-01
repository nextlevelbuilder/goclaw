package bitrix24

import (
	"testing"
)

// TestParseEntityContext_TableDriven covers every chat surface goclaw currently
// receives from a tamgiac.bitrix24.com portal (verified via raw webhook dumps)
// plus a handful of malformed-input cases so future Bitrix schema drift does
// not crash the handler. Each row asserts on the produced metadata map — the
// unit under test is the composition of ParseEntityContext + ToMeta, matching
// what handle.go actually calls.
func TestParseEntityContext_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		params   EventParams
		wantOK   bool
		wantKeys map[string]string // subset — the row's "must have" keys
		absent   []string          // keys that MUST NOT appear
	}{
		{
			name: "openline facebook full payload with lead",
			params: EventParams{
				ChatEntityType:  "LINES",
				ChatEntityID:    "facebook|34|36305652112415023|1074",
				ChatEntityData1: "Y|LEAD|360|N|N|1018|1782879230|0|0|",
				ChatEntityData2: "LEAD|360|COMPANY|0|CONTACT|0|DEAL|0",
				ChatEntityData3: "N",
				ChatTitle:       "Tình Đặng - TEst Fanpage 1",
				ChatType:        "L",
			},
			wantOK: true,
			wantKeys: map[string]string{
				MetaKeyChatTitle:        "Tình Đặng - TEst Fanpage 1",
				MetaKeyChatType:         "L",
				MetaKeyOLConnectorCode:  "facebook",
				MetaKeyOLLineID:         "34",
				MetaKeyOLExternalUID:    "36305652112415023",
				MetaKeyOLBitrixUserID:   "1074",
				MetaKeyOLLineConfigID:   "1018",
				MetaKeyOLSessionStarted: "1782879230",
				MetaKeyOLActiveCRMType:  "LEAD",
				MetaKeyOLActiveCRMID:    "360",
				MetaKeyCRMLeadID:        "360",
			},
			absent: []string{
				MetaKeyCRMCompanyID, MetaKeyCRMContactID, MetaKeyCRMDealID,
				MetaKeyTaskID, MetaKeyWorkgroupID, MetaKeyMailID,
			},
		},
		{
			name: "openline zalo OA with contact and deal linked",
			params: EventParams{
				ChatEntityType:  "LINES",
				ChatEntityID:    "synity_zalo_oa_chat|24|zalo_chat_3726955208051041740|840",
				ChatEntityData1: "Y|DEAL|2054|N|N|1020|1782879518|0|0|0",
				ChatEntityData2: "LEAD|0|COMPANY|0|CONTACT|1266|DEAL|2054",
				ChatTitle:       "Đặng Tình - ZaloOA SYNITY",
				ChatType:        "L",
			},
			wantOK: true,
			wantKeys: map[string]string{
				MetaKeyOLConnectorCode: "synity_zalo_oa_chat",
				MetaKeyOLLineID:        "24",
				MetaKeyOLLineConfigID:  "1020",
				MetaKeyOLActiveCRMType: "DEAL",
				MetaKeyOLActiveCRMID:   "2054",
				MetaKeyCRMContactID:    "1266",
				MetaKeyCRMDealID:       "2054",
			},
			absent: []string{
				MetaKeyCRMLeadID, MetaKeyCRMCompanyID, // both were 0 in DATA_2
			},
		},
		{
			name: "openline zalo personal with empty DATA_2 (no auto-CRM)",
			params: EventParams{
				ChatEntityType:  "LINES",
				ChatEntityID:    "synity_zalo_personal|20|zpersonal_1623524631958449211|878",
				ChatEntityData1: "N|NONE|0|N|N|1002|1782787104|0|0|0",
				ChatEntityData2: "", // connector without auto-CRM config
				ChatTitle:       "Thân Công Huy - Zalo Synity 0964575404",
				ChatType:        "L",
			},
			wantOK: true,
			wantKeys: map[string]string{
				MetaKeyOLConnectorCode: "synity_zalo_personal",
				MetaKeyOLLineID:        "20",
				MetaKeyOLLineConfigID:  "1002",
			},
			absent: []string{
				// pos 2 == "NONE" → active CRM must NOT be emitted.
				MetaKeyOLActiveCRMType, MetaKeyOLActiveCRMID,
				// DATA_2 empty → no CRM linkage keys at all.
				MetaKeyCRMLeadID, MetaKeyCRMCompanyID,
				MetaKeyCRMContactID, MetaKeyCRMDealID,
			},
		},
		{
			name: "native CRM contact chat",
			params: EventParams{
				ChatEntityType: "CRM",
				ChatEntityID:   "CONTACT|1780",
				ChatType:       "C",
			},
			wantOK: true,
			wantKeys: map[string]string{
				MetaKeyChatType:      "C",
				MetaKeyCRMEntityType: "CONTACT",
				MetaKeyCRMEntityID:   "1780",
				MetaKeyCRMContactID:  "1780", // mirrored for type-agnostic lookup
			},
			absent: []string{
				MetaKeyCRMLeadID, MetaKeyCRMCompanyID, MetaKeyCRMDealID,
				MetaKeyOLConnectorCode, MetaKeyTaskID,
			},
		},
		{
			name: "native CRM deal chat",
			params: EventParams{
				ChatEntityType: "CRM",
				ChatEntityID:   "DEAL|2064",
				ChatType:       "C",
			},
			wantOK: true,
			wantKeys: map[string]string{
				MetaKeyCRMEntityType: "DEAL",
				MetaKeyCRMEntityID:   "2064",
				MetaKeyCRMDealID:     "2064",
			},
		},
		{
			name: "task chat",
			params: EventParams{
				ChatEntityType: "TASKS_TASK",
				ChatEntityID:   "2794",
				ChatTitle:      "Tích hợp channel bitrix24 vào goclaw",
				ChatType:       "X",
			},
			wantOK: true,
			wantKeys: map[string]string{
				MetaKeyChatTitle: "Tích hợp channel bitrix24 vào goclaw",
				MetaKeyChatType:  "X",
				MetaKeyTaskID:    "2794",
			},
			absent: []string{
				MetaKeyWorkgroupID, MetaKeyMailID, MetaKeyCRMEntityID,
			},
		},
		{
			name: "sonet workgroup chat",
			params: EventParams{
				ChatEntityType: "SONET_GROUP",
				ChatEntityID:   "64",
				ChatTitle:      "Chuyển Đổi",
				ChatType:       "B",
			},
			wantOK: true,
			wantKeys: map[string]string{
				MetaKeyChatTitle:   "Chuyển Đổi",
				MetaKeyChatType:    "B",
				MetaKeyWorkgroupID: "64",
			},
			absent: []string{MetaKeyTaskID, MetaKeyMailID},
		},
		{
			name: "bitrix mail chat",
			params: EventParams{
				ChatEntityType: "MAIL",
				ChatEntityID:   "31200",
				ChatType:       "C",
			},
			wantOK: true,
			wantKeys: map[string]string{
				MetaKeyChatType: "C",
				MetaKeyMailID:   "31200",
			},
		},
		{
			name:   "plain group with no entity binding",
			params: EventParams{},
			wantOK: false,
		},
		{
			name: "plain chat with just a title (still exposed)",
			params: EventParams{
				ChatTitle: "Ad-hoc discussion",
				ChatType:  "C",
			},
			wantOK: true,
			wantKeys: map[string]string{
				MetaKeyChatTitle: "Ad-hoc discussion",
				MetaKeyChatType:  "C",
			},
			absent: []string{
				MetaKeyOLConnectorCode, MetaKeyCRMEntityID, MetaKeyTaskID,
			},
		},
		{
			name: "malformed openline ENTITY_ID with only 3 tokens (missing bot user)",
			params: EventParams{
				ChatEntityType: "LINES",
				ChatEntityID:   "facebook|34|abc",
				ChatType:       "L",
			},
			wantOK: true,
			wantKeys: map[string]string{
				MetaKeyOLConnectorCode: "facebook",
				MetaKeyOLLineID:        "34",
				MetaKeyOLExternalUID:   "abc",
			},
			absent: []string{MetaKeyOLBitrixUserID},
		},
		{
			name: "malformed DATA_1 with only 5 tokens (still extract active CRM)",
			params: EventParams{
				ChatEntityType:  "LINES",
				ChatEntityID:    "facebook|34|x|y",
				ChatEntityData1: "Y|LEAD|360|N|N",
				ChatType:        "L",
			},
			wantOK: true,
			wantKeys: map[string]string{
				MetaKeyOLActiveCRMType: "LEAD",
				MetaKeyOLActiveCRMID:   "360",
			},
			absent: []string{
				MetaKeyOLLineConfigID, MetaKeyOLSessionStarted,
			},
		},
		{
			name: "malformed CRM ENTITY_ID with single token (log warn, skip)",
			params: EventParams{
				ChatEntityType: "CRM",
				ChatEntityID:   "12345",
				ChatType:       "C",
			},
			wantOK: true,
			wantKeys: map[string]string{
				MetaKeyChatType: "C",
			},
			absent: []string{
				MetaKeyCRMEntityType, MetaKeyCRMEntityID,
			},
		},
		{
			name: "unknown ENTITY_TYPE still surfaces title / chat_type",
			params: EventParams{
				ChatEntityType: "CALENDAR_EVENT",
				ChatEntityID:   "999",
				ChatTitle:      "Standup",
				ChatType:       "C",
			},
			wantOK: true,
			wantKeys: map[string]string{
				MetaKeyChatTitle: "Standup",
				MetaKeyChatType:  "C",
			},
			absent: []string{MetaKeyTaskID, MetaKeyMailID},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ec, ok := ParseEntityContext(&tc.params)
			if ok != tc.wantOK {
				t.Fatalf("ParseEntityContext ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			meta := ec.ToMeta(&tc.params)
			for k, want := range tc.wantKeys {
				got, present := meta[k]
				if !present {
					t.Errorf("meta[%q] missing, want %q", k, want)
					continue
				}
				if got != want {
					t.Errorf("meta[%q] = %q, want %q", k, got, want)
				}
			}
			for _, k := range tc.absent {
				if v, present := meta[k]; present {
					t.Errorf("meta[%q] = %q, want absent", k, v)
				}
			}
		})
	}
}

// TestParseEntityContext_NilSafe guards against a nil EventParams pointer —
// callers should not do this but defence-in-depth prevents a panic if the
// bus somehow delivers a stub event.
func TestParseEntityContext_NilSafe(t *testing.T) {
	ec, ok := ParseEntityContext(nil)
	if ok {
		t.Fatalf("ParseEntityContext(nil) ok = true, want false")
	}
	meta := ec.ToMeta(nil)
	if len(meta) != 0 {
		t.Fatalf("ToMeta(nil) returned %d keys, want 0: %v", len(meta), meta)
	}
}

// TestParseEntityContext_ZeroActiveCRMIDIgnored ensures pos 3 == "0" in DATA_1
// does not leak an OL_ACTIVE_CRM_ID="0" into meta — that would look like a real
// id to downstream consumers.
func TestParseEntityContext_ZeroActiveCRMIDIgnored(t *testing.T) {
	p := EventParams{
		ChatEntityType:  "LINES",
		ChatEntityID:    "facebook|34|x|y",
		ChatEntityData1: "Y|LEAD|0|N|N|1018|1782879230|0|0|0",
		ChatType:        "L",
	}
	ec, ok := ParseEntityContext(&p)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	meta := ec.ToMeta(&p)
	if v, present := meta[MetaKeyOLActiveCRMID]; present {
		t.Errorf("meta[%q] = %q, want absent (id was 0)", MetaKeyOLActiveCRMID, v)
	}
	// The type "LEAD" is still meaningful — "we know a Lead is in focus but
	// its id has not been minted yet". Keep it.
	if v := meta[MetaKeyOLActiveCRMType]; v != "LEAD" {
		t.Errorf("meta[%q] = %q, want %q", MetaKeyOLActiveCRMType, v, "LEAD")
	}
}
