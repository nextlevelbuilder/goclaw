package decisionmodel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/security"
)

const testKey = "sk-test-SECRETKEY-1234567890"

// newTestServer starts an httptest server and relaxes the https + loopback
// guards for its lifetime. Tests using it must not run in parallel with
// tests asserting those guards.
func newTestServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	security.SetAllowLoopbackForTest(true)
	requireHTTPS = false
	t.Cleanup(func() {
		srv.Close()
		security.SetAllowLoopbackForTest(false)
		requireHTTPS = true
	})
	return srv
}

func newTestJev(t *testing.T, endpoint string, mod func(*Config)) Decider {
	t.Helper()
	cfg := Config{Endpoint: endpoint, APIKey: testKey}
	if mod != nil {
		mod(&cfg)
	}
	d, err := NewJev(cfg)
	if err != nil {
		t.Fatalf("NewJev: %v", err)
	}
	return d
}

func writeChoice(w http.ResponseWriter, choice string, confidence float64) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"model":"jev-1.13.0","answers":{"target":{"type":"choice","choice":"`+choice+
		`","probabilities":{"coder":0.8,"self":0.2},"confidence":`+jsonFloat(confidence)+`}},"usage":{"input_tokens":312,"output_tokens":20}}`)
}

func jsonFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func TestJevRequestMatchesVendorShape(t *testing.T) {
	var got map[string]any
	var auth string
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		writeChoice(w, "coder", 0.9)
	})

	d := newTestJev(t, srv.URL, func(c *Config) { c.MaxStateChars = 5 })
	_, err := d.Decide(context.Background(), State{
		Message: "chào thế giới", // multi-byte: truncation must cut runes, not bytes
		Options: map[string]string{"coder": "Writes Go code"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if auth != "Bearer "+testKey {
		t.Fatalf("Authorization = %q", auth)
	}
	if got["model"] != DefaultModel {
		t.Fatalf("model = %v, want pinned %s", got["model"], DefaultModel)
	}
	if got["state"] != "chào " {
		t.Fatalf("state = %q, want rune-truncated %q", got["state"], "chào ")
	}
	if _, singular := got["question"]; singular {
		t.Fatal(`request uses singular "question"; vendor requires "questions" map`)
	}
	q := got["questions"].(map[string]any)["target"].(map[string]any)
	if q["type"] != "choice" || q["instructions"] != Instructions {
		t.Fatalf("question = %+v", q)
	}
	crit := q["criteria"].(map[string]any)
	if crit["coder"] != "Writes Go code" || crit[SelfOption] == nil || len(crit) != 2 {
		t.Fatalf("criteria must hold offered options plus self, got %+v", crit)
	}
}

func TestJevStateExtraCannotOverrideMessage(t *testing.T) {
	var got map[string]any
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		writeChoice(w, "coder", 0.9)
	})
	d := newTestJev(t, srv.URL, nil)
	_, err := d.Decide(context.Background(), State{
		Message: "real message",
		Options: map[string]string{"coder": "x"},
		Extra:   map[string]string{"team_role": "lead", "request": "spoofed"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	state := got["state"].(map[string]any)
	if state["request"] != "real message" || state["team_role"] != "lead" {
		t.Fatalf("state = %+v", state)
	}
}

func TestJevDecodesAnswerUnderQuestionID(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) { writeChoice(w, "coder", 0.81) })
	dec, err := newTestJev(t, srv.URL, nil).Decide(context.Background(), State{Message: "fix main.go", Options: map[string]string{"coder": "x"}})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Option != "coder" || dec.Confidence != 0.81 || dec.Model != "jev-1.13.0" || dec.InputTokens != 312 || dec.Probabilities["coder"] != 0.8 {
		t.Fatalf("decision = %+v", dec)
	}
}

func TestJevFailuresAreUnavailableAndNeverLeakKey(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"401 echoing key", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"bad key `+r.Header.Get("Authorization")+`"}`)
		}},
		{"429", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
		}},
		{"500", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }},
		{"malformed body", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "{not json "+testKey) }},
		// The rejected design read choice off the response root; that shape must not decode.
		{"choice at root", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"model":"jev-1.13.0","choice":"coder","confidence":0.9}`)
		}},
		{"wrong answer type", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"answers":{"target":{"type":"noul","noul":0.9}}}`)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t, tc.handler)
			dec, err := newTestJev(t, srv.URL, nil).Decide(context.Background(), State{Message: "m", Options: map[string]string{"coder": "x"}})
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("err = %v, want ErrUnavailable", err)
			}
			if strings.Contains(err.Error(), "SECRETKEY") || dec.Option != "" {
				t.Fatalf("leak or verdict on failure: err=%q dec=%+v", err, dec)
			}
		})
	}
}

func TestJevTimeoutIsUnavailable(t *testing.T) {
	release := make(chan struct{})
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) { <-release })
	defer close(release)
	d := newTestJev(t, srv.URL, func(c *Config) { c.Timeout = 50 * time.Millisecond })

	start := time.Now()
	_, err := d.Decide(context.Background(), State{Message: "m", Options: map[string]string{"coder": "x"}})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("single attempt took %v; timeout not enforced", elapsed)
	}
}

func TestJevSingleAttempt(t *testing.T) {
	calls := 0
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, _ = newTestJev(t, srv.URL, nil).Decide(context.Background(), State{Message: "m", Options: map[string]string{"coder": "x"}})
	if calls != 1 {
		t.Fatalf("calls = %d, want exactly 1 (no retry on a latency-bound gate)", calls)
	}
}

func TestNewJevRejectsUnsafeConfig(t *testing.T) {
	cases := map[string]Config{
		"plain http":      {Endpoint: "http://api.typesafe.ai/v1/systemone", APIKey: testKey},
		"non-http scheme": {Endpoint: "file:///etc/passwd", APIKey: testKey},
		"embedded creds":  {Endpoint: "https://user:pw@api.typesafe.ai/v1/systemone", APIKey: testKey},
		"missing host":    {Endpoint: "https:///v1/systemone", APIKey: testKey},
		"empty api key":   {Endpoint: DefaultEndpoint, APIKey: "  "},
	}
	for name, cfg := range cases {
		if _, err := NewJev(cfg); err == nil {
			t.Errorf("%s: NewJev accepted %+v", name, cfg.Endpoint)
		} else if strings.Contains(err.Error(), "SECRETKEY") {
			t.Errorf("%s: error leaks key: %v", name, err)
		}
	}
}

// SSRF is enforced per call (DNS can move after construction), with the
// loopback test bypass OFF — this is the production path.
func TestJevDecideBlocksPrivateDestinations(t *testing.T) {
	for _, endpoint := range []string{
		"https://127.0.0.1/v1/systemone",
		"https://169.254.169.254/latest/meta-data",
		"https://10.0.0.1/v1/systemone",
		"https://[::1]/v1/systemone",
	} {
		d, err := NewJev(Config{Endpoint: endpoint, APIKey: testKey})
		if err != nil {
			t.Fatalf("NewJev(%s): %v", endpoint, err)
		}
		_, err = d.Decide(context.Background(), State{Message: "m", Options: map[string]string{"coder": "x"}})
		if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "rejected") {
			t.Errorf("%s: err = %v, want SSRF rejection", endpoint, err)
		}
	}
}

func TestJevRejectsTooManyOptions(t *testing.T) {
	d, err := NewJev(Config{APIKey: testKey})
	if err != nil {
		t.Fatal(err)
	}
	opts := make(map[string]string, maxOptions)
	for i := range maxOptions {
		opts[strings.Repeat("a", i+1)] = "x"
	}
	if _, err := d.Decide(context.Background(), State{Message: "m", Options: opts}); err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want local validation error before egress", err)
	}
}
