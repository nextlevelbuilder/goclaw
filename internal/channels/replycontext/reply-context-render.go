package replycontext

import "strings"

type renderMessage struct {
	sender string
	body   string
}

func NormalizeIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func RenderQuote(quote Quote) string {
	return renderQuote(quote, DefaultOptions())
}

func renderQuote(quote Quote, opts Options) string {
	body := strings.TrimSpace(quote.Body)
	if body == "" {
		return ""
	}
	body = truncateText(body, opts.MaxCharsPerMessage)
	block, ok := renderReplyBlock(strings.TrimSpace(quote.Sender), body, opts.MaxTotalChars)
	if !ok {
		return ""
	}
	return block
}

func Compose(context, body string) string {
	context = strings.TrimSpace(context)
	body = strings.TrimSpace(body)
	switch {
	case context != "" && body != "":
		return context + "\n" + body
	case context != "":
		return context
	default:
		return body
	}
}

func renderChain(chain []renderMessage, opts Options) string {
	remaining := opts.MaxTotalChars
	var b strings.Builder
	for i, msg := range chain {
		if i > 0 {
			if remaining <= 1 {
				break
			}
			b.WriteByte('\n')
			remaining--
		}
		body := truncateText(msg.body, opts.MaxCharsPerMessage)
		block, ok := renderReplyBlock(msg.sender, body, remaining)
		if !ok {
			break
		}
		b.WriteString(block)
		remaining -= len(block)
	}
	return b.String()
}

func renderReplyBlock(sender, body string, maxBytes int) (string, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", false
	}
	prefix := "[Replying to"
	if sender = inlineText(sender); sender != "" {
		prefix += " " + sender
	}
	prefix += "]\n"
	suffix := "\n[/Replying]"
	if maxBytes > 0 {
		bodyBudget := maxBytes - len(prefix) - len(suffix)
		if bodyBudget <= 0 {
			return "", false
		}
		body = truncateText(body, bodyBudget)
	}
	return prefix + body + suffix, true
}

func truncateText(text string, maxBytes int) string {
	text = strings.TrimSpace(text)
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	const marker = "..."
	if maxBytes <= len(marker) {
		return marker[:maxBytes]
	}
	limit := maxBytes - len(marker)
	var b strings.Builder
	for _, r := range text {
		if b.Len()+len(string(r)) > limit {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String()) + marker
}

func inlineText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
