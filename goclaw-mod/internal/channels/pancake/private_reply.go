package pancake

import "strings"

// defaultPrivateReplyMsg is the English fallback when PrivateReplyMessage is
// empty. Not localized by design — sellers set their own wording in config.
const defaultPrivateReplyMsg = "Thanks for your comment! We'll DM you shortly."

// renderPrivateReplyMessage substitutes {{key}} placeholders in tmpl with vars
// values. Pre-sanitizes values (strips "{{" and "}}") so a value cannot inject
// another placeholder. Empty tmpl falls back to defaultPrivateReplyMsg.
// Unknown placeholders are left as-is.
func renderPrivateReplyMessage(tmpl string, vars map[string]string) string {
	if tmpl == "" {
		tmpl = defaultPrivateReplyMsg
	}
	out := tmpl
	for k, v := range vars {
		safe := strings.ReplaceAll(v, "{{", "")
		safe = strings.ReplaceAll(safe, "}}", "")
		out = strings.ReplaceAll(out, "{{"+k+"}}", safe)
	}
	return out
}
