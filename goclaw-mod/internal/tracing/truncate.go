package tracing

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const truncateMarker = "\n\n... [truncated %d chars] ...\n\n"

// TruncateMid truncates s by removing the middle portion, keeping head + tail.
// Returns s unchanged if it fits within maxLen.
func TruncateMid(s string, maxLen int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= maxLen {
		return s
	}

	marker := fmt.Sprintf(truncateMarker, len(s)-maxLen)
	usable := maxLen - len(marker)
	if usable <= 0 {
		return s[len(s)-maxLen:]
	}

	head := usable * 2 / 3 // 2/3 head, 1/3 tail
	tail := usable - head

	// Align to rune boundaries.
	for head > 0 && !utf8.RuneStart(s[head]) {
		head--
	}
	tailStart := len(s) - tail
	for tailStart < len(s) && !utf8.RuneStart(s[tailStart]) {
		tailStart++
	}

	return s[:head] + marker + s[tailStart:]
}

// TruncateJSON truncates a JSON string by removing middle array elements,
// preserving valid JSON structure. Falls back to TruncateMid if the input
// is not a JSON array.
func TruncateJSON(s string, maxLen int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= maxLen {
		return s
	}

	// Only handle top-level JSON arrays (the common case: messages array).
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(s), &arr); err != nil || len(arr) < 3 {
		return TruncateMid(s, maxLen)
	}

	// Pre-compute each element's serialized length to avoid repeated string builds.
	elemLens := make([]int, len(arr))
	for i, raw := range arr {
		elemLens[i] = len(raw)
	}

	// Find max elements to keep via binary search on total size.
	// We keep `keep` elements split as ceil(keep/2) head + floor(keep/2) tail.
	maxKeep := len(arr) - 1 // must drop at least 1 to have a placeholder
	bestKeep := 0

	lo, hi := 2, maxKeep // minimum 2 (1 head + 1 tail)
	if lo > hi {
		lo = hi
	}
	for lo <= hi {
		mid := (lo + hi) / 2
		size := estimateArraySize(arr, elemLens, mid)
		if size <= maxLen {
			bestKeep = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}

	// Even 2 elements too big — truncate individual elements.
	if bestKeep < 2 {
		return truncateArrayElements(arr, maxLen)
	}

	keepHead := (bestKeep + 1) / 2
	keepTail := bestKeep - keepHead
	return buildTruncatedArray(arr, keepHead, keepTail)
}

// estimateArraySize returns the byte length of a JSON array keeping `keep` elements
// (split head/tail) plus a placeholder, without building the actual string.
func estimateArraySize(arr []json.RawMessage, elemLens []int, keep int) int {
	keepHead := (keep + 1) / 2
	keepTail := keep - keepHead
	dropped := len(arr) - keep

	// placeholder: {"__truncated__":"N elements omitted"}
	placeholderLen := 22 + digitCount(dropped) + 17 // key + number + " elements omitted"}

	total := 2 // [ and ]
	for i := range keepHead {
		total += elemLens[i]
	}
	total += placeholderLen
	for i := len(arr) - keepTail; i < len(arr); i++ {
		total += elemLens[i]
	}
	total += keep // commas between elements (keep elements + 1 placeholder - 1)
	return total
}

// digitCount returns the number of decimal digits in n.
func digitCount(n int) int {
	if n <= 0 {
		return 1
	}
	d := 0
	for n > 0 {
		d++
		n /= 10
	}
	return d
}

// buildTruncatedArray constructs a JSON array string keeping head and tail elements,
// with a placeholder object in the middle showing how many elements were omitted.
func buildTruncatedArray(arr []json.RawMessage, keepHead, keepTail int) string {
	dropped := len(arr) - keepHead - keepTail
	placeholder := fmt.Sprintf(`{"__truncated__":"%d elements omitted"}`, dropped)

	parts := make([]string, 0, keepHead+1+keepTail)
	for i := range keepHead {
		parts = append(parts, string(arr[i]))
	}
	parts = append(parts, placeholder)
	for i := len(arr) - keepTail; i < len(arr); i++ {
		parts = append(parts, string(arr[i]))
	}

	return "[" + strings.Join(parts, ",") + "]"
}

// truncateArrayElements keeps first + last element but truncates their content.
func truncateArrayElements(arr []json.RawMessage, maxLen int) string {
	dropped := len(arr) - 2
	placeholder := fmt.Sprintf(`{"__truncated__":"%d elements omitted"}`, dropped)
	overhead := len("[,,]") + len(placeholder)
	perElem := max((maxLen-overhead)/2, 50)

	first := TruncateMid(string(arr[0]), perElem)
	last := TruncateMid(string(arr[len(arr)-1]), perElem)
	return "[" + first + "," + placeholder + "," + last + "]"
}
