package nodehost

import (
	"regexp"
	"strings"
)

// PosixInlineCommandFlags are the flags that introduce an inline command in POSIX shells.
var PosixInlineCommandFlags = newSet("-lc", "-c", "--command")

// InlineCommandMatch holds the result of searching for an inline command flag.
type InlineCommandMatch struct {
	Command         *string // the inline command text, or nil
	ValueTokenIndex *int    // index of the value token, or nil
}

var combinedCRe = regexp.MustCompile(`(?i)^-[^-]*c[^-]*$`)

// ResolveInlineCommandMatch scans argv for a shell inline command flag.
// If allowCombinedC is true, handles combined short flags like "-lc".
func ResolveInlineCommandMatch(argv []string, flags map[string]bool, allowCombinedC bool) InlineCommandMatch {
	for i := 1; i < len(argv); i++ {
		token := strings.TrimSpace(safeIndex(argv, i))
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		if lower == "--" {
			break
		}
		if flags[lower] {
			var cmd *string
			var vidx *int
			if i+1 < len(argv) {
				idx := i + 1
				vidx = &idx
				v := strings.TrimSpace(argv[i+1])
				if v != "" {
					cmd = &v
				}
			}
			return InlineCommandMatch{Command: cmd, ValueTokenIndex: vidx}
		}
		if allowCombinedC && combinedCRe.MatchString(token) {
			cIdx := strings.Index(strings.ToLower(token), "c")
			inline := strings.TrimSpace(token[cIdx+1:])
			if inline != "" {
				idx := i
				return InlineCommandMatch{Command: &inline, ValueTokenIndex: &idx}
			}
			var cmd *string
			var vidx *int
			if i+1 < len(argv) {
				idx := i + 1
				vidx = &idx
				v := strings.TrimSpace(argv[i+1])
				if v != "" {
					cmd = &v
				}
			}
			return InlineCommandMatch{Command: cmd, ValueTokenIndex: vidx}
		}
	}
	return InlineCommandMatch{}
}
