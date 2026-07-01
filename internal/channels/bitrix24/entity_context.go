package bitrix24

import (
	"log/slog"
	"strconv"
	"strings"
)

// EntityContext holds the decoded Bitrix24 chat-entity binding for a single
// inbound message. It is derived from CHAT_ENTITY_TYPE / CHAT_ENTITY_ID plus
// the opaque CHAT_ENTITY_DATA_1 / CHAT_ENTITY_DATA_2 payloads Bitrix ships
// alongside them.
//
// Purpose: give agents (and MCP tools) type-safe access to "which entity does
// this chat represent?" without regex-splitting raw pipe-delimited strings or
// paying an extra RPC. Zero value means the chat has no decodable binding
// (plain group / DM), and ParseEntityContext returns ok=false in that case.
//
// Fields are populated on a best-effort basis: malformed input is logged at
// WARN level but does not fail — partial data (e.g. connector code + line but
// missing bitrix_user_id) is preferred over dropping the whole context.
type EntityContext struct {
	// Openline (ChatEntityType == "LINES") — decoded from CHAT_ENTITY_ID:
	//   "<connector_code>|<line_id>|<external_uid>|<bitrix_user_id>"
	// Example: "facebook|34|36305652112415023|1074"
	OLConnectorCode string
	OLLineID        string
	OLExternalUID   string
	OLBitrixUserID  string

	// Openline session state — decoded from CHAT_ENTITY_DATA_1 (10 tokens):
	//   "<wait>|<activeType>|<activeID>|<silent>|<spam>|<lineCfg>|<started>|<unanswered>|<parent>|<closed>"
	// Example: "Y|DEAL|2054|N|N|1020|1782879518|0|0|0"
	OLLineConfigID   string // pos 6 (line_config_id — 1002 Zalo, 1018 FB, 1020 Zalo OA)
	OLSessionStarted string // pos 7 (unix ts, stored as string to keep the raw wire value)
	OLActiveCRMType  string // pos 2 (LEAD / DEAL / CONTACT / COMPANY / NONE)
	OLActiveCRMID    string // pos 3 (0 = unset)

	// CRM linkage — decoded from CHAT_ENTITY_DATA_2 (4 slots, fixed order):
	//   "LEAD|<id>|COMPANY|<id>|CONTACT|<id>|DEAL|<id>"
	// A value of "0" means the connector has not linked this entity yet.
	// Empty string means Bitrix did not ship a value in the payload.
	CRMLeadID    string
	CRMCompanyID string
	CRMContactID string
	CRMDealID    string

	// Native CRM-integrated chat (ChatEntityType == "CRM"). ENTITY_ID is
	// a 2-token "<TYPE>|<id>" like "CONTACT|1780" or "DEAL|2064".
	CRMType string
	CRMID   string

	// Single-token ENTITY_ID cases: TASKS_TASK / SONET_GROUP / MAIL.
	TaskID      string
	WorkgroupID string
	MailID      string
}

// ParseEntityContext decodes an EventParams into an EntityContext. It returns
// ok=false when the chat carries no entity binding at all (plain group / DM),
// so callers can skip the ToMeta merge cheaply.
//
// The function never panics on malformed input — bad payloads log a WARN and
// return whatever fields could be recovered.
func ParseEntityContext(p *EventParams) (EntityContext, bool) {
	if p == nil {
		return EntityContext{}, false
	}
	if p.ChatEntityType == "" && p.ChatTitle == "" && p.ChatType == "" {
		// No binding, no title, no chat-type letter → nothing to expose.
		return EntityContext{}, false
	}

	ec := EntityContext{}
	switch p.ChatEntityType {
	case "LINES":
		ec.fillOpenline(p)
	case "CRM":
		ec.fillCRMChat(p)
	case "TASKS_TASK":
		ec.TaskID = strings.TrimSpace(p.ChatEntityID)
	case "SONET_GROUP":
		ec.WorkgroupID = strings.TrimSpace(p.ChatEntityID)
	case "MAIL":
		ec.MailID = strings.TrimSpace(p.ChatEntityID)
	case "":
		// No entity type but title / chat_type may still exist — that is
		// fine, ToMeta will still emit those two keys via the caller.
	default:
		// Unknown but non-empty type — record raw ENTITY_ID as best effort.
		// Do not warn: Bitrix may introduce new types (CALENDAR, LIVECHAT,
		// DOCS, ...); noisy WARN spam would obscure real parse errors.
		slog.Debug("bitrix24 entity: unknown ChatEntityType — passing raw",
			"chat_entity_type", p.ChatEntityType,
			"chat_entity_id", p.ChatEntityID)
	}

	return ec, true
}

// fillOpenline decodes the Openline-specific fields off CHAT_ENTITY_ID and
// CHAT_ENTITY_DATA_1 / DATA_2. All three are best-effort — Bitrix has been
// seen to ship empty DATA_2 on Zalo-personal connectors that lack auto-CRM.
func (ec *EntityContext) fillOpenline(p *EventParams) {
	// CHAT_ENTITY_ID: 4 tokens.
	if id := strings.TrimSpace(p.ChatEntityID); id != "" {
		tokens := strings.Split(id, "|")
		if len(tokens) < 4 {
			slog.Warn("bitrix24 entity: openline CHAT_ENTITY_ID has fewer than 4 tokens",
				"chat_entity_id", id,
				"tokens", len(tokens))
		}
		if len(tokens) >= 1 {
			ec.OLConnectorCode = strings.TrimSpace(tokens[0])
		}
		if len(tokens) >= 2 {
			ec.OLLineID = strings.TrimSpace(tokens[1])
		}
		if len(tokens) >= 3 {
			ec.OLExternalUID = strings.TrimSpace(tokens[2])
		}
		if len(tokens) >= 4 {
			ec.OLBitrixUserID = strings.TrimSpace(tokens[3])
		}
	}

	// CHAT_ENTITY_DATA_1: 10 tokens (session state). Missing / short is
	// common (some connectors trail an empty final token); take what we can.
	if d1 := strings.TrimSpace(p.ChatEntityData1); d1 != "" {
		tokens := strings.Split(d1, "|")
		if len(tokens) < 10 {
			slog.Warn("bitrix24 entity: openline DATA_1 has fewer than 10 tokens",
				"data_1", d1,
				"tokens", len(tokens))
		}
		if len(tokens) >= 3 {
			activeType := strings.TrimSpace(tokens[1])
			activeID := strings.TrimSpace(tokens[2])
			// pos 2 = "NONE" when no CRM entity is focused; treat as absent.
			if !strings.EqualFold(activeType, "NONE") && activeType != "" {
				ec.OLActiveCRMType = activeType
				if activeID != "" && activeID != "0" {
					ec.OLActiveCRMID = activeID
				}
			}
		}
		if len(tokens) >= 6 {
			ec.OLLineConfigID = strings.TrimSpace(tokens[5])
		}
		if len(tokens) >= 7 {
			started := strings.TrimSpace(tokens[6])
			// Sanity check — must parse as int64. Anything else is noise.
			if _, err := strconv.ParseInt(started, 10, 64); err == nil && started != "0" {
				ec.OLSessionStarted = started
			}
		}
	}

	// CHAT_ENTITY_DATA_2: 4 slots × 2 tokens = 8 tokens.
	//   "LEAD|<id>|COMPANY|<id>|CONTACT|<id>|DEAL|<id>"
	// Parse as label→id pairs so a future re-ordering by Bitrix would still
	// map correctly. Empty DATA_2 means "no auto-CRM configured" — silent.
	if d2 := strings.TrimSpace(p.ChatEntityData2); d2 != "" {
		tokens := strings.Split(d2, "|")
		if len(tokens)%2 != 0 {
			slog.Warn("bitrix24 entity: openline DATA_2 has odd token count",
				"data_2", d2,
				"tokens", len(tokens))
		}
		for i := 0; i+1 < len(tokens); i += 2 {
			label := strings.ToUpper(strings.TrimSpace(tokens[i]))
			id := strings.TrimSpace(tokens[i+1])
			if id == "" || id == "0" {
				continue
			}
			switch label {
			case "LEAD":
				ec.CRMLeadID = id
			case "COMPANY":
				ec.CRMCompanyID = id
			case "CONTACT":
				ec.CRMContactID = id
			case "DEAL":
				ec.CRMDealID = id
			default:
				slog.Warn("bitrix24 entity: openline DATA_2 unknown label",
					"label", label,
					"id", id,
					"data_2", d2)
			}
		}
	}
}

// fillCRMChat decodes ENTITY_ID for a native CRM-integrated chat. Expected
// shape is 2 tokens "<TYPE>|<id>", e.g. "CONTACT|1780" or "DEAL|2064".
func (ec *EntityContext) fillCRMChat(p *EventParams) {
	id := strings.TrimSpace(p.ChatEntityID)
	if id == "" {
		return
	}
	tokens := strings.Split(id, "|")
	if len(tokens) < 2 {
		slog.Warn("bitrix24 entity: CRM CHAT_ENTITY_ID has fewer than 2 tokens",
			"chat_entity_id", id,
			"tokens", len(tokens))
		return
	}
	ec.CRMType = strings.ToUpper(strings.TrimSpace(tokens[0]))
	ec.CRMID = strings.TrimSpace(tokens[1])
	// Convenience: also populate the per-type CRM* id fields so downstream
	// consumers can key off a single name regardless of chat surface.
	if ec.CRMID != "" && ec.CRMID != "0" {
		switch ec.CRMType {
		case "LEAD":
			ec.CRMLeadID = ec.CRMID
		case "COMPANY":
			ec.CRMCompanyID = ec.CRMID
		case "CONTACT":
			ec.CRMContactID = ec.CRMID
		case "DEAL":
			ec.CRMDealID = ec.CRMID
		}
	}
}

// ToMeta materialises the EntityContext into a flat metadata map. Only
// non-empty values are emitted so callers merging into an existing map do
// not overwrite keys that were never populated.
//
// CHAT_TITLE and CHAT_TYPE are read from the EventParams passed in — they
// belong to every chat, not just those with an ENTITY binding, so they are
// emitted whenever present regardless of ok.
func (ec *EntityContext) ToMeta(p *EventParams) map[string]string {
	out := make(map[string]string, 12)

	if p != nil {
		if v := strings.TrimSpace(p.ChatTitle); v != "" {
			out[MetaKeyChatTitle] = v
		}
		if v := strings.TrimSpace(p.ChatType); v != "" {
			out[MetaKeyChatType] = v
		}
	}

	if ec.OLConnectorCode != "" {
		out[MetaKeyOLConnectorCode] = ec.OLConnectorCode
	}
	if ec.OLLineID != "" {
		out[MetaKeyOLLineID] = ec.OLLineID
	}
	if ec.OLExternalUID != "" {
		out[MetaKeyOLExternalUID] = ec.OLExternalUID
	}
	if ec.OLBitrixUserID != "" {
		out[MetaKeyOLBitrixUserID] = ec.OLBitrixUserID
	}
	if ec.OLLineConfigID != "" {
		out[MetaKeyOLLineConfigID] = ec.OLLineConfigID
	}
	if ec.OLSessionStarted != "" {
		out[MetaKeyOLSessionStarted] = ec.OLSessionStarted
	}
	if ec.OLActiveCRMType != "" {
		out[MetaKeyOLActiveCRMType] = ec.OLActiveCRMType
	}
	if ec.OLActiveCRMID != "" {
		out[MetaKeyOLActiveCRMID] = ec.OLActiveCRMID
	}

	if ec.CRMLeadID != "" {
		out[MetaKeyCRMLeadID] = ec.CRMLeadID
	}
	if ec.CRMCompanyID != "" {
		out[MetaKeyCRMCompanyID] = ec.CRMCompanyID
	}
	if ec.CRMContactID != "" {
		out[MetaKeyCRMContactID] = ec.CRMContactID
	}
	if ec.CRMDealID != "" {
		out[MetaKeyCRMDealID] = ec.CRMDealID
	}

	if ec.CRMType != "" {
		out[MetaKeyCRMEntityType] = ec.CRMType
	}
	if ec.CRMID != "" {
		out[MetaKeyCRMEntityID] = ec.CRMID
	}

	if ec.TaskID != "" {
		out[MetaKeyTaskID] = ec.TaskID
	}
	if ec.WorkgroupID != "" {
		out[MetaKeyWorkgroupID] = ec.WorkgroupID
	}
	if ec.MailID != "" {
		out[MetaKeyMailID] = ec.MailID
	}

	return out
}
