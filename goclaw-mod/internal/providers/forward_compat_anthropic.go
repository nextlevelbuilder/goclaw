package providers

import (
	"regexp"
	"strconv"
)

// anthropicVersionRe matches versioned Claude model IDs like "claude-opus-4-7-20260501".
var anthropicVersionRe = regexp.MustCompile(`^(claude-(?:opus|sonnet|haiku)-\d+)-(\d+)(.*)$`)

// AnthropicForwardCompat resolves unknown Claude models by cloning from prior versions.
type AnthropicForwardCompat struct{}

// ResolveForwardCompat handles models like "claude-opus-4-7" by finding "claude-opus-4-6".
func (r *AnthropicForwardCompat) ResolveForwardCompat(modelID string, registry ModelRegistry) *ModelSpec {
	m := anthropicVersionRe.FindStringSubmatch(modelID)
	if m == nil {
		return nil
	}

	prefix := m[1]  // e.g. "claude-opus-4"
	version := m[2]  // e.g. "7"
	suffix := m[3]   // e.g. "-20260501" or ""

	// Try decrementing the version to find a known template
	ver := 0
	for _, c := range version {
		ver = ver*10 + int(c-'0')
	}
	if ver <= 0 {
		return nil
	}

	// Build template candidates: try with same suffix first, then without
	var templates []string
	for v := ver - 1; v >= ver-3 && v >= 0; v-- {
		vs := strconv.Itoa(v)
		candidate := prefix + "-" + vs + suffix
		templates = append(templates, candidate)
		if suffix != "" {
			templates = append(templates, prefix+"-"+vs)
		}
	}

	return CloneFromTemplate(registry, "anthropic", modelID, templates, nil)
}
