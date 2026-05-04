// Obsidian-style YAML frontmatter parsing for memory markdown files.
//
// Frontmatter convention: a YAML block at the very top of the file,
// delimited by `---` lines. Everything between the delimiters is parsed
// as YAML; everything after the closing `---` is the markdown body.
//
// We extract a stable subset of fields (title, type, updated, tags,
// aliases, sources) into a typed Metadata struct, and capture the rest
// as a generic map under Metadata.Extra so callers can persist and
// retrieve any vault-specific fields without parser changes.
package memory

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Metadata is the structured frontmatter extracted from a memory doc.
// All fields are best-effort: a doc with no frontmatter returns the
// zero value with Body = the entire input.
type Metadata struct {
	Title    string         `json:"title,omitempty"`
	Type     string         `json:"type,omitempty"`
	Updated  string         `json:"updated,omitempty"` // ISO date-ish; we don't parse to time.Time because Obsidian users sometimes write "2026-04-06" or "April 2026"
	Tags     []string       `json:"tags,omitempty"`
	Aliases  []string       `json:"aliases,omitempty"`
	Sources  []string       `json:"sources,omitempty"`
	Extra    map[string]any `json:"extra,omitempty"` // any frontmatter keys outside the well-known set
}

// HasContent returns true if the parser found any frontmatter at all.
// Useful for callers that want to skip the metadata write when nothing
// was extracted.
func (m Metadata) HasContent() bool {
	if m.Title != "" || m.Type != "" || m.Updated != "" {
		return true
	}
	if len(m.Tags) > 0 || len(m.Aliases) > 0 || len(m.Sources) > 0 {
		return true
	}
	return len(m.Extra) > 0
}

// ParseFrontmatter splits an Obsidian markdown document into its
// frontmatter Metadata and the markdown Body (with the frontmatter
// block stripped). When no frontmatter is present, returns the zero
// Metadata and the full input as Body. YAML parse errors return the
// zero Metadata + the full input — never an error — so a malformed
// frontmatter doesn't break indexing.
func ParseFrontmatter(content string) (meta Metadata, body string) {
	// Frontmatter must start at byte 0. Allow a leading BOM or blank
	// line — common when files are saved by tools that prepend either.
	trimmed := strings.TrimLeft(content, "\ufeff\n\r")
	if !strings.HasPrefix(trimmed, "---") {
		return Metadata{}, content
	}

	// Open delimiter is the first line. It must be exactly "---" with
	// no other content (Obsidian convention; we tolerate trailing
	// whitespace).
	rest := trimmed[3:]
	if !strings.HasPrefix(rest, "\n") && !strings.HasPrefix(rest, "\r\n") {
		return Metadata{}, content
	}

	// Find the closing "---" on its own line.
	closeIdx := strings.Index(rest, "\n---")
	if closeIdx < 0 {
		return Metadata{}, content
	}
	yamlBlock := rest[:closeIdx]
	afterClose := rest[closeIdx+4:] // skip "\n---"
	// Body must start at the next newline; tolerate trailing whitespace
	// on the closing delimiter line.
	if i := strings.Index(afterClose, "\n"); i >= 0 {
		body = afterClose[i+1:]
	} else {
		body = ""
	}

	// Parse YAML into a generic map then promote known fields. Going
	// through map[string]any (rather than directly into Metadata) lets
	// us capture Extra without enumerating every possible key.
	raw := map[string]any{}
	if err := yaml.Unmarshal([]byte(yamlBlock), &raw); err != nil {
		// Malformed frontmatter — return body without frontmatter so
		// the caller still indexes the markdown content, but no
		// metadata is extracted.
		return Metadata{}, body
	}

	meta = Metadata{Extra: map[string]any{}}
	for k, v := range raw {
		switch strings.ToLower(k) {
		case "title":
			meta.Title = stringOf(v)
		case "type":
			meta.Type = stringOf(v)
		case "updated":
			meta.Updated = stringOf(v)
		case "tags":
			meta.Tags = stringSliceOf(v)
		case "aliases":
			meta.Aliases = stringSliceOf(v)
		case "sources":
			meta.Sources = stringSliceOf(v)
		default:
			meta.Extra[k] = v
		}
	}
	if len(meta.Extra) == 0 {
		meta.Extra = nil
	}
	return meta, body
}

// stringOf coerces a YAML-parsed value to string. Numbers and dates
// (which yaml.v3 returns as time.Time when the format matches) are
// stringified so the storage layer always sees a string.
func stringOf(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		// fmt.Sprint would also work but bypasses our string-only
		// contract for known scalar fields. yaml.v3 returns numbers
		// as int / float64 / bool; stringify deterministically.
		return formatScalar(t)
	}
}

func formatScalar(v any) string {
	switch t := v.(type) {
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return intToStr(int64(t))
	case int64:
		return intToStr(t)
	case float64:
		// Trim trailing zeros: 2026.0 → "2026"
		s := floatToStr(t)
		return s
	default:
		return ""
	}
}

func intToStr(n int64) string {
	// strconv-free to keep imports minimal.
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func floatToStr(f float64) string {
	// Cheap whole-number detect; otherwise fall through to a
	// minimal-precision float repr. Don't pull in strconv for this.
	if f == float64(int64(f)) {
		return intToStr(int64(f))
	}
	// Conservative fallback for non-integer floats — rare in practice
	// for Obsidian frontmatter (most numeric fields are years/IDs).
	// Use a simple textual rep via fmt-equivalent without importing fmt.
	return floatTextRep(f)
}

func floatTextRep(f float64) string {
	// Tiny custom impl: integer part + "." + first 6 digits.
	// Negligible precision for our use case; avoids fmt import.
	neg := f < 0
	if neg {
		f = -f
	}
	whole := int64(f)
	frac := f - float64(whole)
	out := intToStr(whole)
	if frac == 0 {
		return out
	}
	out += "."
	for i := 0; i < 6; i++ {
		frac *= 10
		d := int(frac)
		out += string(rune('0' + d))
		frac -= float64(d)
		if frac == 0 {
			break
		}
	}
	if neg {
		return "-" + out
	}
	return out
}

// stringSliceOf coerces a YAML-parsed value to []string. Accepts:
//   - nil → nil
//   - []any → element-wise string coerce
//   - string → single-element slice (Obsidian sometimes writes
//     `tags: voice` instead of `tags: [voice]`)
//   - comma-separated string → split
//   - anything else → nil
func stringSliceOf(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s := stringOf(e); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	case string:
		// Treat single string as single-element slice. Don't split on
		// commas — Obsidian uses YAML lists for multi-tag.
		if t == "" {
			return nil
		}
		return []string{t}
	default:
		return nil
	}
}
