package http

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	providerUsageDefaultLimit = 20
	providerUsageMaxLimit     = 100
	providerUsageMaxRange     = 90 * 24 * time.Hour
	// providerUsageErrorScan bounds how many of the newest failed spans are read
	// to build the grouped recent-error list.
	providerUsageErrorScan = 500
)

// providerUsageDetail is the GET /v1/providers/{id}/usage-detail response.
// Raw span error text is never included, only its failover class.
type providerUsageDetail struct {
	Provider     string                   `json:"provider"`
	Calls        int64                    `json:"calls"`
	Errors       int64                    `json:"errors"`
	LatencyMS    providerUsageLatency     `json:"latency_ms"`
	RecentErrors []providerUsageErrorItem `json:"recent_errors"`
}

type providerUsageLatency struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
}

type providerUsageErrorItem struct {
	At         time.Time `json:"at"`
	ErrorClass string    `json:"error_class"`
	Count      int       `json:"count"`
}

// handleProviderUsageDetail serves latency percentiles and recent failed LLM
// calls of one provider for a time range, scoped to the provider's tenant.
func (h *ProvidersHandler) handleProviderUsageDetail(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	statsStore, ok := h.tracingStore.(store.ProviderCallStatsStore)
	if !ok || statsStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": i18n.T(locale, i18n.MsgInvalidRequest, "tracing store unavailable"),
		})
		return
	}

	from, to, limit, msg := parseProviderUsageParams(r)
	if msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, msg)})
		return
	}

	p, ok := h.providerForHealth(w, r)
	if !ok {
		return
	}

	stats, err := statsStore.GetProviderCallStats(r.Context(), store.ProviderCallStatsQuery{
		TenantID:   p.TenantID,
		Provider:   p.Name,
		From:       from,
		To:         to,
		ErrorLimit: providerUsageErrorScan,
	})
	if err != nil {
		slog.Error("providers.usage_detail_failed", "provider", p.Name, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": i18n.T(locale, i18n.MsgInternalError, "provider usage detail"),
		})
		return
	}

	writeJSON(w, http.StatusOK, providerUsageDetail{
		Provider:     p.Name,
		Calls:        stats.Calls,
		Errors:       stats.Errors,
		LatencyMS:    providerUsageLatency{P50: stats.LatencyP50MS, P95: stats.LatencyP95MS},
		RecentErrors: groupProviderErrors(stats.RecentErrors, limit),
	})
}

// parseProviderUsageParams validates from/to (RFC3339, required, to after from,
// at most 90 days apart) and clamps limit the way other list endpoints do.
// A non-empty msg describes the first invalid parameter.
func parseProviderUsageParams(r *http.Request) (from, to time.Time, limit int, msg string) {
	q := r.URL.Query()
	if q.Get("from") == "" || q.Get("to") == "" {
		return from, to, 0, "from and to are required"
	}
	var err error
	if from, err = time.Parse(time.RFC3339, q.Get("from")); err != nil {
		return from, to, 0, "from must be RFC3339"
	}
	if to, err = time.Parse(time.RFC3339, q.Get("to")); err != nil {
		return from, to, 0, "to must be RFC3339"
	}
	if !to.After(from) {
		return from, to, 0, "to must be after from"
	}
	if to.Sub(from) > providerUsageMaxRange {
		return from, to, 0, "range must be at most 90 days"
	}

	limit = providerUsageDefaultLimit
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		limit = min(n, providerUsageMaxLimit)
	}
	return from.UTC(), to.UTC(), limit, ""
}

// groupProviderErrors folds failed calls into one item per (hour, error class),
// newest first, at most limit items. Each item's At is its newest call.
func groupProviderErrors(spans []store.ProviderErrorSpan, limit int) []providerUsageErrorItem {
	type key struct {
		hour  time.Time
		class string
	}
	index := map[key]int{}
	items := []providerUsageErrorItem{}
	for _, s := range spans {
		at := s.At.UTC()
		k := key{hour: at.Truncate(time.Hour), class: classifySpanError(s.Error)}
		if i, ok := index[k]; ok {
			items[i].Count++
			if at.After(items[i].At) {
				items[i].At = at
			}
			continue
		}
		index[k] = len(items)
		items = append(items, providerUsageErrorItem{At: at, ErrorClass: k.class, Count: 1})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].At.After(items[j].At) })
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

// spanHTTPStatus matches the "HTTP <status>: <body>" text providers.HTTPError
// produces; wrapping errors may put a prefix before it.
var spanHTTPStatus = regexp.MustCompile(`HTTP (\d{3}): `)

var spanErrorClassifier = providers.NewDefaultClassifier()

// classifySpanError maps stored span error text to the runtime failover class
// (rate_limit, timeout, overloaded, auth, billing, ..., context_overflow).
func classifySpanError(text string) string {
	var err error = errors.New(text)
	if m := spanHTTPStatus.FindStringSubmatchIndex(text); m != nil {
		status, _ := strconv.Atoi(text[m[2]:m[3]])
		err = &providers.HTTPError{Status: status, Body: text[m[1]:]}
	}
	c := providers.ClassifyHTTPError(spanErrorClassifier, err)
	if c.Kind == "context_overflow" {
		return "context_overflow"
	}
	// Live context.DeadlineExceeded satisfies net.Error and classifies as timeout;
	// the stored text has lost that type, so match its wording instead.
	if c.Reason == providers.FailoverUnknown && containsAnyFold(text, "deadline exceeded", "timed out") {
		return string(providers.FailoverTimeout)
	}
	return string(c.Reason)
}

func containsAnyFold(s string, subs ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}
