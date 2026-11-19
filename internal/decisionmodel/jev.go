package decisionmodel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/security"
)

// questionID is the single question id sent to the vendor. The vendor never
// shows ids to the model; it only keys the answer.
const questionID = "target"

// Instructions is versioned in code so accuracy changes are attributable to a
// commit, not to drift in operator config.
const Instructions = `Which team member should handle this request? Choose the option whose described expertise best matches the request. If no member clearly matches, choose "self".`

// maxResponseBytes bounds the decoded body; a one-question answer is tiny.
const maxResponseBytes = 1 << 20

type jevClient struct {
	endpoint      string
	host          string // redacted form for logs
	apiKey        string
	model         string
	timeout       time.Duration
	maxStateChars int
	http          *http.Client
}

// NewJev validates cfg and returns a Decider for TypeSafe Jev.
func NewJev(cfg Config) (Decider, error) {
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	u, err := validateEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("decision model api key is empty")
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxStateChars <= 0 {
		cfg.MaxStateChars = DefaultMaxStateChars
	}
	return &jevClient{
		endpoint:      cfg.Endpoint,
		host:          u.Host,
		apiKey:        cfg.APIKey,
		model:         cfg.Model,
		timeout:       cfg.Timeout,
		maxStateChars: cfg.MaxStateChars,
		http:          security.NewSafeClient(cfg.Timeout),
	}, nil
}

type jevQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type jevRequest struct {
	State     any                    `json:"state"`
	Model     string                 `json:"model"`
	Questions map[string]jevQuestion `json:"questions"`
}

type jevAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

type jevResponse struct {
	Model   string               `json:"model"`
	Answers map[string]jevAnswer `json:"answers"`
	Usage   struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
}

// buildRequest maps State onto the vendor's questions-map shape.
func (c *jevClient) buildRequest(state State) (jevRequest, error) {
	options := withSelf(state.Options)
	if len(options) > maxOptions {
		return jevRequest{}, fmt.Errorf("decision model: %d options exceeds limit %d", len(options), maxOptions)
	}
	for id := range options {
		if id == "" {
			return jevRequest{}, errors.New("decision model: empty option id")
		}
	}
	msg := truncateRunes(state.Message, c.maxStateChars)
	var payload any = msg
	if len(state.Extra) > 0 {
		obj := make(map[string]string, len(state.Extra)+1)
		maps.Copy(obj, state.Extra)
		obj["request"] = msg // set last: Extra cannot override the message
		payload = obj
	}
	return jevRequest{
		State: payload,
		Model: c.model,
		Questions: map[string]jevQuestion{
			questionID: {Type: "choice", Instructions: Instructions, Criteria: options},
		},
	}, nil
}

// Decide makes exactly one attempt. Every failure is ErrUnavailable-wrapped;
// error strings carry the status code and redacted host, never the key, body,
// or full endpoint (which may hold a token in its query string).
func (c *jevClient) Decide(ctx context.Context, state State) (Decision, error) {
	body, err := c.buildRequest(state)
	if err != nil {
		return Decision{}, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Decision{}, fmt.Errorf("%w: encode request", ErrUnavailable)
	}

	// Validate per call, not once: re-resolving pins the IP we actually dial
	// and catches DNS that moved to a private range after construction.
	_, pinnedIP, err := security.Validate(c.endpoint)
	if err != nil {
		return Decision{}, fmt.Errorf("%w: endpoint %s rejected", ErrUnavailable, c.host)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(security.WithPinnedIP(ctx, pinnedIP), http.MethodPost, c.endpoint, bytes.NewReader(raw))
	if err != nil {
		return Decision{}, fmt.Errorf("%w: build request", ErrUnavailable)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	slog.Info("security.decision_model.egress",
		"host", c.host, "model", c.model,
		"chars", utf8.RuneCountInString(truncateRunes(state.Message, c.maxStateChars)),
		"options", len(body.Questions[questionID].Criteria))

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return Decision{}, fmt.Errorf("%w: %s unreachable", ErrUnavailable, c.host)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		if resp.StatusCode == http.StatusTooManyRequests {
			return Decision{}, fmt.Errorf("%w: rate limited (retry-after %q)", ErrUnavailable, resp.Header.Get("Retry-After"))
		}
		return Decision{}, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}

	var parsed jevResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&parsed); err != nil {
		return Decision{}, fmt.Errorf("%w: malformed response", ErrUnavailable)
	}
	ans, ok := parsed.Answers[questionID]
	if !ok || ans.Type != "choice" || ans.Choice == "" {
		return Decision{}, fmt.Errorf("%w: response has no choice answer", ErrUnavailable)
	}
	slog.Debug("decision_model.answer", "host", c.host, "model", parsed.Model,
		"choice", ans.Choice, "confidence", ans.Confidence, "latency_ms", time.Since(start).Milliseconds())
	return Decision{
		Option:        ans.Choice,
		Probabilities: ans.Probabilities,
		Confidence:    ans.Confidence,
		Model:         parsed.Model,
		InputTokens:   parsed.Usage.InputTokens,
	}, nil
}
