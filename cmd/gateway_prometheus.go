//go:build prometheus

package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

// wirePrometheusMetrics registers a Prometheus text-exposition /metrics
// endpoint on a dedicated listener. It is compiled only with `-tags
// prometheus`; the default binary gets the no-op in gateway_prometheus_noop.go
// (same pattern as gateway_otel*.go). The handler feeds
// reliability.Metrics.Take() (cumulative counters since process start) plus a
// per-tenant spend gauge summed from usage_event_rollups (the billing truth).
//
// The exposition format is hand-rolled Prometheus text format — no external
// dependency, so the default build (without -tags prometheus) stays clean.
// Metrics are named goclaw_* to match the project's OTel naming. The cost
// gauge query is a simple aggregate that works identically on PostgreSQL and
// SQLite, so one build serves both backends.
//
// Returns a stop function that closes the listener. When prometheus is
// disabled (telemetry.prometheus_enabled = false or port <= 0) it returns
// (nil, nil) — no listener, no goroutine.
func wirePrometheusMetrics(ctx context.Context, cfg *config.Config, db *sql.DB) (func(), error) {
	if cfg == nil || !cfg.Telemetry.Prometheus.Enabled {
		slog.Debug("prometheus /metrics available but not enabled (set telemetry.prometheus_enabled + telemetry.prometheus_port)")
		return nil, nil
	}
	port := cfg.Telemetry.Prometheus.EffectivePort()
	if port <= 0 {
		slog.Warn("prometheus /metrics disabled: telemetry.prometheus_port must be > 0")
		return nil, nil
	}

	addr := net.JoinHostPort(cfg.Telemetry.Prometheus.BindHost(), strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("prometheus listen on %s: %w", addr, err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", prometheusMetricsHandler(db))

	var (
		httpSrv = &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		}
		stopOnce sync.Once
	)

	go func() {
		slog.Info("prometheus /metrics listening", "address", ln.Addr().String())
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("prometheus /metrics serve error", "error", err)
		}
	}()

	stop := func() {
		stopOnce.Do(func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(shutdownCtx); err != nil {
				slog.Warn("prometheus /metrics shutdown", "error", err)
			}
		})
	}
	return stop, nil
}

// prometheusTenantCostQuery returns the per-tenant spend total from
// usage_event_rollups. It is a plain aggregate — no window functions, no
// dialect-specific syntax — so the identical query runs on PostgreSQL and
// SQLite.
const prometheusTenantCostQuery = `
SELECT tenant_id, COALESCE(SUM(cost_usd), 0)
FROM usage_event_rollups
GROUP BY tenant_id`

// prometheusTenantCost carries one per-tenant spend total.
type prometheusTenantCost struct {
	TenantID string
	CostUSD  float64
}

// queryPrometheusTenantCosts runs the rollup aggregate and returns the
// per-tenant totals, sorted by tenant for stable output.
func queryPrometheusTenantCosts(ctx context.Context, db *sql.DB) ([]prometheusTenantCost, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, prometheusTenantCostQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []prometheusTenantCost
	for rows.Next() {
		var tc prometheusTenantCost
		if err := rows.Scan(&tc.TenantID, &tc.CostUSD); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TenantID < out[j].TenantID })
	return out, nil
}

// prometheusMetricsHandler builds the /metrics handler. The reliability
// counters are read live on each scrape (Take is lock-free reads of atomics);
// the tenant cost gauges are refreshed per scrape from usage_event_rollups.
func prometheusMetricsHandler(db *sql.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		costRows, err := queryPrometheusTenantCosts(r.Context(), db)
		if err != nil {
			// Best-effort: the core /metrics still serves the in-process
			// reliability counters even when the rollup query fails.
			slog.Warn("prometheus: tenant cost rollup query failed", "error", err)
			costRows = nil
		}
		writePrometheusMetrics(w, costRows)
	})
}

// writePrometheusMetrics renders the Prometheus text exposition format for the
// reliability snapshot and tenant cost gauges. All samples are written through
// a buffer so a partial response is never observed.
func writePrometheusMetrics(w http.ResponseWriter, costRows []prometheusTenantCost) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	snap := reliability.Default().Metrics.Take()
	bf := newPromBuf(w)
	bf.sample("goclaw_llm_requests_total", snap.LLMRequests)
	bf.sample("goclaw_llm_successes_total", snap.LLMSuccesses)
	bf.sample("goclaw_llm_retries_total", snap.LLMRetries)
	bf.sample("goclaw_llm_rate_limited_total", snap.LLMRateLimited)
	bf.sample("goclaw_llm_server_errors_total", snap.LLMServerErrors)
	bf.sample("goclaw_llm_timeouts_total", snap.LLMTimeouts)
	bf.sample("goclaw_llm_stream_stalls_total", snap.LLMStreamStalls)
	bf.sample("goclaw_llm_loop_total", snap.LLMLoop)
	bf.sample("goclaw_llm_repeated_tool_calls_total", snap.LLMRepeatedToolCalls)
	bf.sample("goclaw_llm_empty_outputs_total", snap.LLMEmptyOutputs)
	bf.sample("goclaw_llm_premature_completions_total", snap.LLMPrematureCompletions)
	bf.sample("goclaw_agent_recovered_total", snap.AgentRecovered)
	bf.sample("goclaw_agent_continued_total", snap.AgentContinued)
	if snap.LLMLatencyCount > 0 {
		avgMS := float64(snap.LLMLatencyNanos) / float64(snap.LLMLatencyCount) / 1e6
		bf.gauge("goclaw_llm_latency_avg_ms", avgMS)
	}

	for _, tc := range costRows {
		bf.gaugeLabeled("goclaw_tenant_cost_usd", tc.CostUSD, "tenant", tc.TenantID)
	}
	bf.flush()
}

// promBuf writes Prometheus text samples through a buffered writer so the
// response is assembled before being sent.
type promBuf struct {
	w   http.ResponseWriter
	buf []byte
}

func newPromBuf(w http.ResponseWriter) *promBuf {
	return &promBuf{w: w}
}

func (p *promBuf) sample(name string, v uint64) {
	p.buf = append(p.buf, name...)
	p.buf = append(p.buf, ' ')
	p.buf = strconv.AppendUint(p.buf, v, 10)
	p.buf = append(p.buf, '\n')
}

func (p *promBuf) gauge(name string, v float64) {
	p.buf = append(p.buf, name...)
	p.buf = append(p.buf, ' ')
	p.buf = strconv.AppendFloat(p.buf, v, 'g', -1, 64)
	p.buf = append(p.buf, '\n')
}

func (p *promBuf) gaugeLabeled(name string, v float64, label, labelVal string) {
	p.buf = append(p.buf, name...)
	p.buf = append(p.buf, '{')
	p.buf = append(p.buf, label...)
	p.buf = append(p.buf, `="`...)
	p.buf = append(p.buf, labelVal...)
	p.buf = append(p.buf, `"} `...)
	p.buf = strconv.AppendFloat(p.buf, v, 'g', -1, 64)
	p.buf = append(p.buf, '\n')
}

func (p *promBuf) flush() {
	_, _ = p.w.Write(p.buf)
}
