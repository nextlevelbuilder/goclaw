package line

import (
	_ "embed"
	"fmt"
)

// FlexTemplatesVersion is bumped whenever the postback data schema changes.
// Embedded so the build artifact carries provenance for production debugging.
//
//go:embed flex/version.txt
var FlexTemplatesVersion string

// FlexTextBox builds a LINE Flex `text` component. Additional key/value pairs
// are merged into the component map (e.g. "size"/"md", "weight"/"bold",
// "wrap"/true). Exported so plugin packages can reuse the primitive without
// importing an internal helper.
func FlexTextBox(text string, kv ...any) map[string]any {
	m := map[string]any{
		"type": "text",
		"text": text,
	}
	for i := 0; i+1 < len(kv); i += 2 {
		key, _ := kv[i].(string)
		m[key] = kv[i+1]
	}
	return m
}

// FlexButton builds a LINE Flex `button` component whose action is a postback
// with the given `data` payload. The displayText mirrors the label so the
// sending user sees what they tapped in their chat history.
func FlexButton(label, postbackData string) map[string]any {
	return map[string]any{
		"type":   "button",
		"style":  "secondary",
		"height": "sm",
		"action": map[string]any{
			"type":        "postback",
			"label":       label,
			"data":        postbackData,
			"displayText": label,
		},
	}
}

// FlexSeparator builds a separator component with pixel margin.
func FlexSeparator(margin int) map[string]any {
	return map[string]any{
		"type":   "separator",
		"margin": fmt.Sprintf("%dpx", margin),
	}
}

// FlexKVRow builds a horizontal key/value row — a common pattern for summary
// bubbles. Key column is narrower and grey; value wraps.
func FlexKVRow(key, value string) map[string]any {
	return map[string]any{
		"type":    "box",
		"layout":  "horizontal",
		"spacing": "md",
		"contents": []map[string]any{
			{
				"type":  "text",
				"text":  key,
				"size":  "sm",
				"color": "#868e96",
				"flex":  2,
			},
			{
				"type": "text",
				"text": value,
				"size": "sm",
				"flex": 5,
				"wrap": true,
			},
		},
	}
}

// Truncate shortens s to at most `max` runes, appending an ellipsis when
// shortening. Unicode-aware — a Traditional Chinese string of 80 characters
// truncated to 10 yields 9 characters plus ellipsis, not raw-byte truncation.
func Truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// CountSelected returns the number of map entries whose value is true.
func CountSelected(m map[int]bool) int {
	n := 0
	for _, v := range m {
		if v {
			n++
		}
	}
	return n
}
