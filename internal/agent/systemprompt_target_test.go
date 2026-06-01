package agent

import (
	"strings"
	"testing"
)

// Test 8: ChatID present → prompt contains <current_reply_target> block.
func TestSystemPromptCurrentReplyTargetInjected(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Channel = "telegram"
	cfg.ChannelType = "telegram"
	cfg.ChatID = "123"
	cfg.PeerKind = "direct"

	prompt := BuildSystemPrompt(cfg)

	for _, want := range []string{
		"<current_reply_target>",
		"chat_id: 123",
		"kind: direct",
		"</current_reply_target>",
		"omit `target` to reply here",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

// Test 8b: group peer → kind: group.
func TestSystemPromptCurrentReplyTargetGroup(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Channel = "telegram"
	cfg.ChannelType = "telegram"
	cfg.ChatID = "-100G"
	cfg.PeerKind = "group"

	prompt := BuildSystemPrompt(cfg)

	if !strings.Contains(prompt, "chat_id: -100G") {
		t.Error("prompt missing group chat_id")
	}
	if !strings.Contains(prompt, "kind: group") {
		t.Error("prompt missing kind: group")
	}
}

// Bitrix24 entity link section — when channel is bitrix24 + domain present,
// the prompt teaches the LLM the correct portal domain so it stops producing
// hallucinated `bitrix24.example.com` URLs in replies.
func TestSystemPromptBitrix24EntityLinkSection(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Channel = "bitrix-sales"
	cfg.ChannelType = "bitrix24"
	cfg.BitrixPortalDomain = "tamgiac.bitrix24.com"
	cfg.SenderID = "614" // numeric Bitrix24 user id from FROM_USER_ID

	prompt := BuildSystemPrompt(cfg)

	for _, want := range []string{
		"## Bitrix24 Entity URLs",
		"Portal domain: `tamgiac.bitrix24.com`",
		// Task URL must substitute the sender's user_id directly so the LLM
		// doesn't fall back to the placeholder path (which 404s).
		"https://tamgiac.bitrix24.com/company/personal/user/614/tasks/task/view/{task_id}/",
		"https://tamgiac.bitrix24.com/crm/deal/details/{deal_id}/",
		"https://tamgiac.bitrix24.com/crm/lead/details/{lead_id}/",
		"https://tamgiac.bitrix24.com/crm/contact/details/{contact_id}/",
		"https://tamgiac.bitrix24.com/crm/company/details/{company_id}/",
		"https://tamgiac.bitrix24.com/shop/orders/details/{order_id}/",
		"https://tamgiac.bitrix24.com/shop/orders/payment/details/{payment_id}/",
		"https://tamgiac.bitrix24.com/shop/orders/shipment/details/{shipment_id}/",
		"https://tamgiac.bitrix24.com/calendar/?EVENT_ID={event_id}",
		"never use `example.com`",
		"trailing `/`",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	// Path-based entity URLs must end with `/` — Bitrix24 path semantics.
	for _, slash := range []string{
		"/crm/deal/details/{deal_id}/`",
		"/shop/orders/details/{order_id}/`",
		"/shop/orders/payment/details/{payment_id}/`",
		"/shop/orders/shipment/details/{shipment_id}/`",
	} {
		if !strings.Contains(prompt, slash) {
			t.Errorf("path URL missing trailing slash: %q", slash)
		}
	}
	// Placeholder domain must not appear as a constructed URL — the warning
	// string itself is allowed to reference `example.com` to instruct against
	// using it.
	if strings.Contains(prompt, "https://bitrix24.example.com") || strings.Contains(prompt, "https://example.com/crm") {
		t.Error("prompt constructed a placeholder URL")
	}
}

// Non-bitrix24 channels must not see the entity link section even if the
// domain field is accidentally populated (defensive — caller should not, but
// scoping is enforced at the gate, not the value).
func TestSystemPromptBitrix24EntityLinkSection_SkippedForOtherChannel(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Channel = "telegram"
	cfg.ChannelType = "telegram"
	cfg.BitrixPortalDomain = "tamgiac.bitrix24.com" // ignored

	prompt := BuildSystemPrompt(cfg)
	if strings.Contains(prompt, "## Bitrix24 Entity URLs") {
		t.Error("Bitrix24 entity link section must not appear for non-bitrix24 channel")
	}
}

// Empty domain → section skipped, even on bitrix24 channel (legacy install
// without a portal row should not produce a useless "Portal domain: ``").
func TestSystemPromptBitrix24EntityLinkSection_SkippedWhenDomainEmpty(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Channel = "bitrix-sales"
	cfg.ChannelType = "bitrix24"
	cfg.BitrixPortalDomain = ""

	prompt := BuildSystemPrompt(cfg)
	if strings.Contains(prompt, "## Bitrix24 Entity URLs") {
		t.Error("section must be omitted when portal domain is empty")
	}
}

// When senderID is non-numeric or empty (cron, synthetic dispatch, system
// runs), the Task URL falls back to a placeholder pattern instead of putting
// junk into the path. Numeric substitution is gated by isNumericID.
func TestBuildBitrix24EntityLinkSection_SenderIDGate(t *testing.T) {
	cases := []struct {
		name      string
		sender    string
		wantSubst bool // expect numeric user id baked into Task URL
	}{
		{"numeric_sender_substitutes", "614", true},
		{"empty_sender_uses_placeholder", "", false},
		{"non_numeric_sender_uses_placeholder", "ticker:system", false},
		{"telegram_username_form_rejected", "12345|alice", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := buildBitrix24EntityLinkSection("tamgiac.bitrix24.com", tc.sender)
			joined := strings.Join(lines, "\n")
			if tc.wantSubst {
				want := "/company/personal/user/" + tc.sender + "/tasks/task/view/"
				if !strings.Contains(joined, want) {
					t.Errorf("Task URL did not substitute sender %q; got: %s", tc.sender, joined)
				}
			} else {
				if !strings.Contains(joined, "{viewer_user_id}") {
					t.Errorf("Task URL should fall back to placeholder for sender %q; got: %s", tc.sender, joined)
				}
				if tc.sender != "" && strings.Contains(joined, "/user/"+tc.sender+"/") {
					t.Errorf("non-numeric sender %q leaked into URL path: %s", tc.sender, joined)
				}
			}
		})
	}
}

// Tolerates accidental scheme/path in channel config (some installers pasted
// the full client_endpoint URL); helper must extract just the host.
func TestBuildBitrix24EntityLinkSection_NormalizesInput(t *testing.T) {
	cases := []string{
		"tamgiac.bitrix24.com",
		"https://tamgiac.bitrix24.com",
		"https://tamgiac.bitrix24.com/rest/",
		"  tamgiac.bitrix24.com  ",
	}
	for _, in := range cases {
		lines := buildBitrix24EntityLinkSection(in, "614")
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, "Portal domain: `tamgiac.bitrix24.com`") {
			t.Errorf("input %q did not normalize to bare domain; got: %s", in, joined)
		}
		if strings.Contains(joined, "https://https://") {
			t.Errorf("input %q produced double scheme: %s", in, joined)
		}
		if strings.Contains(joined, "/rest/") {
			t.Errorf("input %q leaked rest path into URLs: %s", in, joined)
		}
	}
}

// Test 9: ChatID empty → no <current_reply_target> block.
func TestSystemPromptCurrentReplyTargetOmittedWhenNoChat(t *testing.T) {
	cfg := fullTestConfig()
	cfg.ChatID = ""
	prompt := BuildSystemPrompt(cfg)
	if strings.Contains(prompt, "<current_reply_target>") {
		t.Error("prompt should NOT include <current_reply_target> when ChatID is empty")
	}
}
