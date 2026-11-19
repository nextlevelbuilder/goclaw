// Package teamconfig is a leaf package parsing the free-form team settings
// JSON block into typed routing and member-request configuration. It imports
// only the standard library so it can be consumed by internal/tools,
// internal/teamworkclassify, and internal/gateway/methods without cycles.
package teamconfig

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// MemberRequest controls whether team members can create "request" tasks.
type MemberRequest struct {
	Enabled      bool `json:"enabled"`
	AutoDispatch bool `json:"auto_dispatch"`
}

// RoutingRule routes matching messages to one team member.
type RoutingRule struct {
	AgentKey     string   `json:"agent_key"`
	Keywords     []string `json:"keywords"`
	FilePatterns []string `json:"file_patterns"`
	Criteria     string   `json:"criteria"`
	Enabled      *bool    `json:"enabled"` // nil = enabled
}

// Routing is the team-settings routing block. Absent block = feature off.
type Routing struct {
	Enabled             *bool         `json:"enabled"`
	// FallbackToSelf and ConfidenceThreshold are reserved for the Step 3 decision
	// model (confidence gating of its output). The Step 2 deterministic rule path
	// ignores them: a keyword/file match routes with Confidence=1 regardless.
	FallbackToSelf      *bool         `json:"fallback_to_self"`     // nil = true
	ConfidenceThreshold *float64      `json:"confidence_threshold"` // nil = 0.65
	Rules               []RoutingRule `json:"rules"`
}

// ParseMemberRequest extracts the member_requests block. Canonical
// implementation — replaces the two prior duplicates.
func ParseMemberRequest(settings json.RawMessage) MemberRequest {
	var cfg MemberRequest
	if len(settings) == 0 {
		return cfg
	}
	var raw struct {
		MemberRequests *struct {
			Enabled      *bool `json:"enabled"`
			AutoDispatch *bool `json:"auto_dispatch"`
		} `json:"member_requests"`
	}
	if json.Unmarshal(settings, &raw) != nil || raw.MemberRequests == nil {
		return cfg
	}
	if raw.MemberRequests.Enabled != nil {
		cfg.Enabled = *raw.MemberRequests.Enabled
	}
	if raw.MemberRequests.AutoDispatch != nil {
		cfg.AutoDispatch = *raw.MemberRequests.AutoDispatch
	}
	return cfg
}

// ParseRouting extracts the routing block from raw team settings JSON. An
// absent or malformed block yields a zero Routing — settings are free-form
// and a bad block must never be fatal to classification.
func ParseRouting(settings json.RawMessage) Routing {
	var raw struct {
		Routing *Routing `json:"routing"`
	}
	if len(settings) == 0 || json.Unmarshal(settings, &raw) != nil || raw.Routing == nil {
		return Routing{}
	}
	return *raw.Routing
}

// EffectiveRules drops disabled rules and rules whose AgentKey is not a
// current team member. This is the ONLY place membership is validated — a
// stale rule can never route work to a non-member.
func EffectiveRules(r Routing, memberKeys []string) []RoutingRule {
	if len(r.Rules) == 0 || (r.Enabled != nil && !*r.Enabled) {
		return nil
	}
	members := make(map[string]bool, len(memberKeys))
	for _, k := range memberKeys {
		members[k] = true
	}
	out := make([]RoutingRule, 0, len(r.Rules))
	for _, rule := range r.Rules {
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}
		if !members[rule.AgentKey] {
			continue
		}
		out = append(out, rule)
	}
	return out
}

// RuleMatchesMessage reports whether a rule's keywords or file patterns match
// the message. Matching is word-boundary-anchored and case-insensitive so
// "go" does not match "google" and ".go" does not match "go.mod".
func RuleMatchesMessage(rule RoutingRule, message string) bool {
	msg := strings.ToLower(message)
	for _, kw := range rule.Keywords {
		if wordBoundaryMatch(strings.ToLower(strings.TrimSpace(kw)), msg) {
			return true
		}
	}
	for _, ext := range rule.FilePatterns {
		if wordBoundaryMatch(strings.ToLower(strings.TrimSpace(ext)), msg) {
			return true
		}
	}
	return false
}

// wordBoundaryMatch is substring match constrained to non-word boundaries.
// Precompiled regexps would be ideal but the rule list is built per-classify
// call; a bounded LRU/cache keyed on the normalized term keeps compilation
// out of the hot path without leaking patterns.
func wordBoundaryMatch(term, msg string) bool {
	re := cachedWordRe(term)
	if re == nil {
		return false
	}
	return re.MatchString(msg)
}

const maxCachedRegexps = 512

var (
	wordReMu    sync.RWMutex
	wordReCache = make(map[string]*regexp.Regexp)
)

// cachedWordRe compiles (once) the boundary-anchored pattern for a term.
//
// Word-start terms (`go`, `dockerfile`) anchor on BOTH sides so `go` does
// not match `google` and `dockerfile` does not match `mydockerfile`.
// Terms starting with a non-word char (`.go`, `*.ts`) anchor only on the
// right: the leading punctuation already separates the token, so `.go`
// and `*.ts` correctly match `internal/foo.go` and `app.ts` but not
// `foo.gopher` or `go.mod`. Leading wildcards (e.g. `*.ts`) are stripped
// so that glob syntax behaves as file-extension matching.
// Boundaries are unicode-aware (`\p{L}`/`\p{N}`) so Vietnamese and Chinese
// keywords behave correctly — Go's ASCII-only `\b` does not.
func cachedWordRe(term string) *regexp.Regexp {
	term = strings.TrimSpace(term)
	if strings.HasPrefix(term, "*") {
		term = strings.TrimPrefix(term, "*")
	}
	if term == "" {
		return nil
	}

	wordReMu.RLock()
	re, ok := wordReCache[term]
	wordReMu.RUnlock()
	if ok {
		return re
	}

	q := regexp.QuoteMeta(term)
	var pattern string
	if r := firstRune(term); isWordRune(r) {
		pattern = `(?:^|[^\p{L}\p{N}])` + q + `(?:[^\p{L}\p{N}]|$)`
	} else {
		pattern = q + `(?:[^\p{L}\p{N}]|$)`
	}
	compiled, err := regexp.Compile(`(?i)` + pattern)
	if err != nil {
		return nil
	}

	wordReMu.Lock()
	if len(wordReCache) >= maxCachedRegexps {
		wordReCache = make(map[string]*regexp.Regexp)
	}
	wordReCache[term] = compiled
	wordReMu.Unlock()
	return compiled
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
