// Package systemmessages renders configurable messages that GoClaw sends
// directly, outside normal LLM output.
package systemmessages

import (
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
)

const (
	KeyPairingAccountRequired       = "pairing.account_required"
	KeyPairingAccountSimpleRequired = "pairing.account_simple_required"
	KeyPairingGroupRequired         = "pairing.group_required"
	KeyPairingGroupPrivateRequired  = "pairing.group_private_required"
	KeyPairingApproved              = "pairing.approved"
)

const defaultAppName = "GoClaw"

// Vars are {{name}} template variables used when rendering a system message.
type Vars map[string]string

// Definition describes a system message key for UIs and validation.
type Definition struct {
	Key          string            `json:"key"`
	Template     string            `json:"template"`
	Description  string            `json:"description,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Descriptions map[string]string `json:"descriptions,omitempty"`
	Variables    []string          `json:"variables,omitempty"`
}

var defaults = map[string]Definition{
	KeyPairingAccountRequired: {
		Key: KeyPairingAccountRequired,
		Template: `{{app_name}}: access not configured.

Your {{platform}} ID: {{sender_id}}

Pairing code: {{code}}

Ask the bot owner to approve with:
  {{approve_command}}`,
		Description: "Sent to an unpaired direct-message user.",
		Labels: map[string]string{
			i18n.LocaleEN: "Account pairing required",
			i18n.LocaleVI: "Yêu cầu ghép nối tài khoản",
			i18n.LocaleZH: "需要配对账号",
			i18n.LocaleKO: "계정 연결 필요",
		},
		Descriptions: map[string]string{
			i18n.LocaleEN: "Sent to an unpaired direct-message user.",
			i18n.LocaleVI: "Gửi khi người dùng nhắn riêng chưa được ghép nối.",
			i18n.LocaleZH: "发送给尚未配对的私聊用户。",
			i18n.LocaleKO: "아직 연결되지 않은 1:1 메시지 사용자에게 보냅니다.",
		},
		Variables: []string{"app_name", "platform", "sender_id", "code", "approve_command"},
	},
	KeyPairingAccountSimpleRequired: {
		Key: KeyPairingAccountSimpleRequired,
		Template: `🔗 This account hasn't been paired yet.

Pairing code: {{code}}

Share this code with the bot owner to get access.`,
		Description: "Sent to an unpaired account when the platform does not need to show the sender ID.",
		Labels: map[string]string{
			i18n.LocaleEN: "Simple account pairing required",
			i18n.LocaleVI: "Yêu cầu ghép nối tài khoản đơn giản",
			i18n.LocaleZH: "需要配对账号（简版）",
			i18n.LocaleKO: "간단 계정 연결 필요",
		},
		Descriptions: map[string]string{
			i18n.LocaleEN: "Sent to an unpaired account when the platform does not need to show the sender ID.",
			i18n.LocaleVI: "Gửi khi tài khoản chưa được ghép nối và nền tảng không cần hiển thị ID người gửi.",
			i18n.LocaleZH: "当平台不需要显示发送者 ID 时，发送给尚未配对的账号。",
			i18n.LocaleKO: "플랫폼에서 보낸 사람 ID를 표시할 필요가 없을 때 연결되지 않은 계정에 보냅니다.",
		},
		Variables: []string{"app_name", "platform", "code", "approve_command"},
	},
	KeyPairingGroupRequired: {
		Key: KeyPairingGroupRequired,
		Template: `🔗 This group hasn't been paired yet.

Pairing code: {{code}}

Share this code with the bot owner to get access.`,
		Description: "Sent to an unpaired group chat.",
		Labels: map[string]string{
			i18n.LocaleEN: "Group pairing required",
			i18n.LocaleVI: "Yêu cầu ghép nối nhóm",
			i18n.LocaleZH: "需要配对群组",
			i18n.LocaleKO: "그룹 연결 필요",
		},
		Descriptions: map[string]string{
			i18n.LocaleEN: "Sent to an unpaired group chat.",
			i18n.LocaleVI: "Gửi khi nhóm chat chưa được ghép nối.",
			i18n.LocaleZH: "发送到尚未配对的群聊。",
			i18n.LocaleKO: "아직 연결되지 않은 그룹 채팅에 보냅니다.",
		},
		Variables: []string{"app_name", "platform", "code", "approve_command"},
	},
	KeyPairingGroupPrivateRequired: {
		Key: KeyPairingGroupPrivateRequired,
		Template: `This channel is not authorized to use this bot.

An admin can approve via CLI:
  {{approve_command}}

Or approve via the {{app_name}} web UI (Pairing section).`,
		Description: "Sent to an unpaired group/private channel where the pairing code should not be exposed directly.",
		Labels: map[string]string{
			i18n.LocaleEN: "Channel approval required",
			i18n.LocaleVI: "Kênh cần được cấp quyền",
			i18n.LocaleZH: "频道需要授权",
			i18n.LocaleKO: "채널 승인 필요",
		},
		Descriptions: map[string]string{
			i18n.LocaleEN: "Sent to an unpaired group/private channel where the pairing code should not be exposed directly.",
			i18n.LocaleVI: "Gửi khi kênh hoặc nhóm chưa được cấp quyền và không nên hiển thị mã ghép nối trực tiếp.",
			i18n.LocaleZH: "发送到尚未配对且不应直接暴露配对码的群组或私有频道。",
			i18n.LocaleKO: "연결되지 않았고 연결 코드를 직접 노출하면 안 되는 그룹 또는 비공개 채널에 보냅니다.",
		},
		Variables: []string{"app_name", "platform", "code", "approve_command"},
	},
	KeyPairingApproved: {
		Key:         KeyPairingApproved,
		Template:    "✅ {{app_name}} access approved. Send a message to start chatting.",
		Description: "Sent after a pairing request is approved.",
		Labels: map[string]string{
			i18n.LocaleEN: "Pairing approved",
			i18n.LocaleVI: "Đã phê duyệt ghép nối",
			i18n.LocaleZH: "配对已批准",
			i18n.LocaleKO: "연결 승인됨",
		},
		Descriptions: map[string]string{
			i18n.LocaleEN: "Sent after a pairing request is approved.",
			i18n.LocaleVI: "Gửi sau khi yêu cầu ghép nối được phê duyệt.",
			i18n.LocaleZH: "配对请求获批后发送。",
			i18n.LocaleKO: "연결 요청이 승인된 뒤 보냅니다.",
		},
		Variables: []string{"app_name"},
	},
}

// Defaults returns a defensive copy of built-in system message definitions.
func Defaults() map[string]Definition {
	out := make(map[string]Definition, len(defaults))
	for key, def := range defaults {
		if len(def.Labels) > 0 {
			def.Labels = copyStringMap(def.Labels)
		}
		if len(def.Descriptions) > 0 {
			def.Descriptions = copyStringMap(def.Descriptions)
		}
		if len(def.Variables) > 0 {
			def.Variables = append([]string(nil), def.Variables...)
		}
		out[key] = def
	}
	return out
}

func copyStringMap(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

// Resolver renders messages using live config snapshots.
type Resolver struct {
	cfg *config.Config
}

func NewResolver(cfg *config.Config) *Resolver {
	return &Resolver{cfg: cfg}
}

// Render resolves a message template by key and locale, applies variables, and
// falls back to built-in English defaults when no override exists.
func (r *Resolver) Render(locale, key string, vars Vars) string {
	template := r.template(locale, key)
	return renderTemplate(template, r.withDefaults(vars))
}

func (r *Resolver) template(locale, key string) string {
	normalized := r.locale(locale)
	if r != nil && r.cfg != nil {
		if messages := r.cfg.SystemMessagesSnapshot().Messages; len(messages) > 0 {
			if byLocale := messages[key]; len(byLocale) > 0 {
				if template := strings.TrimSpace(byLocale[normalized]); template != "" {
					return template
				}
				if template := strings.TrimSpace(byLocale[i18n.LocaleEN]); template != "" {
					return template
				}
			}
		}
	}
	if def, ok := defaults[key]; ok {
		return def.Template
	}
	return key
}

func (r *Resolver) locale(locale string) string {
	if strings.TrimSpace(locale) != "" {
		return i18n.Normalize(locale)
	}
	if r != nil && r.cfg != nil {
		if configured := strings.TrimSpace(r.cfg.SystemMessagesSnapshot().DefaultLocale); configured != "" {
			return i18n.Normalize(configured)
		}
	}
	return i18n.LocaleEN
}

func (r *Resolver) withDefaults(vars Vars) Vars {
	out := make(Vars, len(vars)+2)
	for key, value := range vars {
		out[key] = value
	}
	if strings.TrimSpace(out["app_name"]) == "" {
		out["app_name"] = r.appName()
	}
	if strings.TrimSpace(out["approve_command"]) == "" && strings.TrimSpace(out["code"]) != "" {
		out["approve_command"] = "goclaw pairing approve " + out["code"]
	}
	return out
}

func (r *Resolver) appName() string {
	return defaultAppName
}

// Render renders a default resolver with no custom config. It is useful for
// callers that do not yet have config wiring but should share the same defaults.
func Render(locale, key string, vars Vars) string {
	return NewResolver(nil).Render(locale, key, vars)
}

func renderTemplate(template string, vars Vars) string {
	rendered := template
	for key, value := range vars {
		rendered = strings.ReplaceAll(rendered, "{{"+key+"}}", value)
	}
	return rendered
}
