// Package decisionmodel is a client for "System One" typed-decision models
// (TypeSafe Jev). A decision model takes a state plus typed questions and
// returns a choice with a calibrated probability distribution instead of prose.
//
// The package is a leaf: it imports only internal/security for SSRF-safe
// egress. Callers adapt their own types into State; this package never
// imports classifier or team code.
//
// Egress: Decide sends State.Message (truncated to MaxStateChars) to a
// third-party endpoint. Nothing in this package enables itself — a caller
// must construct a Decider explicitly.
package decisionmodel

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// DefaultEndpoint is TypeSafe's official System One endpoint.
	DefaultEndpoint = "https://api.typesafe.ai/v1/systemone"
	// DefaultModel is a pinned versioned id. The vendor alias "jev-latest"
	// moves on release, which silently shifts calibrated thresholds; pin by
	// default and let operators opt into the alias.
	DefaultModel         = "jev-1.13.0"
	DefaultTimeout       = time.Second
	DefaultMaxStateChars = 4000

	// SelfOption is always offered so "no member matches" is an explicit
	// answer rather than an artifact of the probability distribution.
	SelfOption = "self"

	// maxOptions is the vendor limit for a Choice question.
	maxOptions = 255
)

// ErrUnavailable reports that the decision model could not answer (network,
// timeout, non-2xx, rate limit, malformed body). Callers must treat it as
// "no opinion" — never as a SelfOption verdict.
var ErrUnavailable = errors.New("decision model unavailable")

// Decider resolves a state to one option id.
type Decider interface {
	Decide(ctx context.Context, state State) (Decision, error)
}

// State is the content shown to the decision model.
type State struct {
	Message string            // raw user message; truncated to MaxStateChars
	Options map[string]string // option id -> rubric; SelfOption is added if absent
	Extra   map[string]string // optional structured context (e.g. team role)
}

// Decision is the model's raw answer. Use Accept to turn it into a routing
// verdict; Option is not validated against anything here.
type Decision struct {
	Option        string
	Probabilities map[string]float64
	Confidence    float64
	Model         string // versioned id that answered, for logging/pinning
	InputTokens   int    // billed tokens, for cost attribution
}

// Accept applies the routing semantics to a Decision. It returns the option to
// act on, or "" when the caller should keep its own fallback verdict:
//
//   - SelfOption, or an option not in offered (the model invented an id) -> ""
//   - confidence < threshold and fallbackToSelf                          -> ""
//   - confidence < threshold and !fallbackToSelf                         -> option
//   - otherwise                                                          -> option
//
// offered must be the option set actually sent; membership is checked here so
// a hallucinated id can never be routed on.
func (d Decision) Accept(offered map[string]string, threshold float64, fallbackToSelf bool) string {
	if d.Option == "" || d.Option == SelfOption {
		return ""
	}
	if _, ok := offered[d.Option]; !ok {
		return ""
	}
	if d.Confidence < threshold && fallbackToSelf {
		return ""
	}
	return d.Option
}

// Config configures a Jev client.
type Config struct {
	Endpoint      string        // default DefaultEndpoint; must be https and public
	APIKey        string        // required; never logged or included in errors
	Model         string        // default DefaultModel
	Timeout       time.Duration // default DefaultTimeout; the only retry policy
	MaxStateChars int           // default DefaultMaxStateChars; caps egressed text
}

// requireHTTPS is false only in tests, which talk to httptest servers.
var requireHTTPS = true

// validateEndpoint rejects malformed, non-https, and credential-bearing URLs.
// SSRF (private/loopback/metadata) is checked separately per request so DNS
// changes after construction cannot redirect egress inward.
func validateEndpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, errors.New("decision model endpoint is not a valid URL")
	}
	if requireHTTPS && u.Scheme != "https" {
		return nil, fmt.Errorf("decision model endpoint must use https, got %q", u.Scheme)
	}
	if u.User != nil {
		return nil, errors.New("decision model endpoint must not embed credentials")
	}
	return u, nil
}

// truncateRunes cuts s to at most n runes without splitting a UTF-8 sequence.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}

// withSelf returns options plus SelfOption, without mutating the input.
func withSelf(options map[string]string) map[string]string {
	out := make(map[string]string, len(options)+1)
	for k, v := range options {
		out[strings.TrimSpace(k)] = v
	}
	if _, ok := out[SelfOption]; !ok {
		out[SelfOption] = "No listed member clearly matches; the current agent handles it."
	}
	return out
}
