//go:build prometheus

package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

// TestPrometheusMetricsHandlerSmoke verifies the /metrics handler renders the
// Prometheus text format with reliability counters and tenant cost gauges.
// Runs only in the -tags prometheus build, where the handler is compiled.
func TestPrometheusMetricsHandlerSmoke(t *testing.T) {
	reliability.Configure(reliability.DefaultCircuitOptions(), 0)
	m := reliability.Default().Metrics
	m.RecordLLMRequest()
	m.RecordLLMSuccess()
	m.RecordLLMRetry()
	m.RecordLLMLatency(150 * 1e6) // 150ms

	rec := httptest.NewRecorder()
	writePrometheusMetrics(rec, []prometheusTenantCost{
		{TenantID: "tenant-a", CostUSD: 1.25},
		{TenantID: "tenant-b", CostUSD: 0.0},
	})

	res := rec.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"goclaw_llm_requests_total 1",
		"goclaw_llm_successes_total 1",
		"goclaw_llm_retries_total 1",
		"goclaw_llm_latency_avg_ms 150",
		`goclaw_tenant_cost_usd{tenant="tenant-a"} 1.25`,
		`goclaw_tenant_cost_usd{tenant="tenant-b"} 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\nbody:\n%s", want, body)
		}
	}
}

// TestPrometheusMetricsHandlerNoCostRows verifies a nil cost row slice is
// tolerated: the handler serves the reliability counters and omits cost
// gauges without panicking.
func TestPrometheusMetricsHandlerNoCostRows(t *testing.T) {
	reliability.Configure(reliability.DefaultCircuitOptions(), 0)
	rec := httptest.NewRecorder()
	writePrometheusMetrics(rec, nil)
	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty metrics body")
	}
	if strings.Contains(rec.Body.String(), "goclaw_tenant_cost_usd") {
		t.Fatal("cost gauges must be omitted with no cost rows")
	}
}

// TestPrometheusWireDisabled verifies the disabled path returns no listener.
func TestPrometheusWireDisabled(t *testing.T) {
	stop, err := wirePrometheusMetrics(nil, nil, nil)
	if err != nil {
		t.Fatalf("disabled wire returned error: %v", err)
	}
	if stop != nil {
		t.Fatal("disabled wire must return nil stop function")
	}
}
