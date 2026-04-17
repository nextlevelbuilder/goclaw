package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

// ---- Event name constants ----

// Package update event names used by the WS event filter and subscribers.
const (
	eventPackageUpdateChecked   = "package.update.checked"
	eventPackageUpdateStarted   = "package.update.started"
	eventPackageUpdateSucceeded = "package.update.succeeded"
	eventPackageUpdateFailed    = "package.update.failed"
)

// ---- Event payload types ----

// PackageUpdateCheckedPayload is broadcast after a refresh completes.
type PackageUpdateCheckedPayload struct {
	Count     int       `json:"count"`
	CheckedAt time.Time `json:"checked_at"`
}

// PackageUpdateStartedPayload is broadcast before Apply is called.
type PackageUpdateStartedPayload struct {
	Source      string `json:"source"`
	Name        string `json:"name"`
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
}

// PackageUpdateSucceededPayload is broadcast after a successful Apply.
type PackageUpdateSucceededPayload struct {
	Source      string `json:"source"`
	Name        string `json:"name"`
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	DurationMs  int64  `json:"duration_ms"`
}

// PackageUpdateFailedPayload is broadcast when Apply returns an error.
type PackageUpdateFailedPayload struct {
	Source string `json:"source"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// ---- handleListUpdates ----

// handleListUpdates returns the current update cache.
// If the cache is stale, triggers a background refresh (non-blocking).
// Auth: operator+ (readAuth in RegisterRoutes).
func (h *PackagesHandler) handleListUpdates(w http.ResponseWriter, r *http.Request) {
	if h.Registry == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "update registry not configured"})
		return
	}

	updates, checkedAt := h.Registry.Cache.Snapshot()
	ttl := h.Registry.TTL
	age := time.Duration(0)
	stale := true
	if !checkedAt.IsZero() {
		age = time.Since(checkedAt)
		stale = age > ttl
	}

	// Non-blocking background refresh when stale.
	if stale {
		h.Registry.RefreshInBackground(r.Context(), 30*time.Second)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"updates":      updates,
		"checkedAt":    checkedAt,
		"ageSeconds":   int64(age.Seconds()),
		"ttlSeconds":   int64(ttl.Seconds()),
		"stale":        stale,
		"sources":      h.Registry.Sources(),
		"availability": h.Registry.Availability(),
	})
}

// ---- handleRefreshUpdates ----

// handleRefreshUpdates runs a synchronous CheckAll and returns the fresh cache.
// Auth: admin + master-scope (adminAuth + requireMasterScope).
func (h *PackagesHandler) handleRefreshUpdates(w http.ResponseWriter, r *http.Request) {
	// red-team H5: master-scope guard first, then write limit.
	if !requireMasterScope(w, r) {
		return
	}
	if !enforcePackagesWriteLimit(w, r, "/v1/packages/updates/refresh") {
		return
	}

	if h.Registry == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "update registry not configured"})
		return
	}

	errs := h.Registry.CheckAll(r.Context())
	if len(errs) > 0 {
		// Log per-source errors but still return whatever partial data was cached.
		for _, e := range errs {
			slog.Warn("packages: refresh partial error", "error", e)
		}
	}

	updates, checkedAt := h.Registry.Cache.Snapshot()

	// Publish checked event (TenantID=Nil → only Owner clients receive).
	if h.Publisher != nil {
		h.Publisher.Broadcast(bus.Event{
			Name:     eventPackageUpdateChecked,
			Payload:  PackageUpdateCheckedPayload{Count: len(updates), CheckedAt: checkedAt},
			TenantID: uuid.Nil,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"updates":   updates,
		"checkedAt": checkedAt,
		"sources":   h.Registry.Sources(),
	})
}

// ---- handleUpdatePackage ----

// updatePackageRequest is the body for POST /v1/packages/update.
type updatePackageRequest struct {
	Package   string `json:"package"`   // "github:<name>" form; full spec also accepted
	ToVersion string `json:"toVersion"` // optional; uses cache entry's LatestVersion if empty
}

// handleUpdatePackage applies a single package update.
// Auth: admin + master-scope.
func (h *PackagesHandler) handleUpdatePackage(w http.ResponseWriter, r *http.Request) {
	// red-team H5: master-scope guard first.
	if !requireMasterScope(w, r) {
		return
	}
	if !enforcePackagesWriteLimit(w, r, "/v1/packages/update") {
		return
	}

	if h.Registry == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "update registry not configured"})
		return
	}

	locale := extractLocale(r)
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req updatePackageRequest
	if !bindJSON(w, r, locale, &req) {
		return
	}

	source, name, ok := resolveUpdateSpec(req.Package)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": i18n.T(locale, i18n.MsgInvalidRequest, "package must be github:<name>, pip:<name>, or npm:<name>"),
		})
		return
	}

	// Locate cache entry for meta + fromVersion.
	updates, _ := h.Registry.Cache.Snapshot()
	var entry *skills.UpdateInfo
	for i := range updates {
		if updates[i].Source == source && updates[i].Name == name {
			entry = &updates[i]
			break
		}
	}

	toVersion := req.ToVersion
	fromVersion := ""
	var meta map[string]any

	if entry != nil {
		fromVersion = entry.CurrentVersion
		meta = entry.Meta
		if toVersion == "" {
			toVersion = entry.LatestVersion
		}
	} else if toVersion == "" {
		// Cache stale/empty and no explicit version — can't proceed.
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": i18n.T(locale, i18n.MsgUpdateCacheStale),
		})
		return
	}

	// Publish started event.
	if h.Publisher != nil {
		h.Publisher.Broadcast(bus.Event{
			Name: eventPackageUpdateStarted,
			Payload: PackageUpdateStartedPayload{
				Source: source, Name: name,
				FromVersion: fromVersion, ToVersion: toVersion,
			},
			TenantID: uuid.Nil,
		})
	}

	slog.Info("packages: applying update", "source", source, "name", name, "to", toVersion)
	// Lock key MUST match the installer's key for the same package (CRIT-2).
	// For github source, installer locks on parsed.Repo (repo-portion only,
	// e.g. "lazygit"). Derive the same from entry meta.repo ("owner/repo").
	lockKey := lockKeyForSource(source, name, meta)
	elapsed, err := h.Registry.Apply(r.Context(), source, lockKey, name, toVersion, meta)

	if err != nil {
		if h.Publisher != nil {
			h.Publisher.Broadcast(bus.Event{
				Name:     eventPackageUpdateFailed,
				Payload:  PackageUpdateFailedPayload{Source: source, Name: name, Reason: err.Error()},
				TenantID: uuid.Nil,
			})
		}
		slog.Error("packages: update failed", "source", source, "name", name, "error", err)

		// red-team C4: detect manifest desync and surface it explicitly.
		manifestDesynced := errors.Is(err, skills.ErrUpdateManifestDesync)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"ok":               false,
			"fromVersion":      fromVersion,
			"toVersion":        toVersion,
			"error":            err.Error(),
			"manifestDesynced": manifestDesynced, // red-team C4: manifest retry desync
		})
		return
	}

	if h.Publisher != nil {
		h.Publisher.Broadcast(bus.Event{
			Name: eventPackageUpdateSucceeded,
			Payload: PackageUpdateSucceededPayload{
				Source: source, Name: name,
				FromVersion: fromVersion, ToVersion: toVersion,
				DurationMs: elapsed.Milliseconds(),
			},
			TenantID: uuid.Nil,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"fromVersion": fromVersion,
		"toVersion":   toVersion,
	})
}

// ---- handleApplyAllUpdates ----

// applyAllRequest is the optional body for POST /v1/packages/updates/apply-all.
// Empty packages array or omitted = apply all cache entries.
type applyAllRequest struct {
	Packages []string `json:"packages"` // "github:<name>" specs; empty = all
}

// applyAllResult accumulates per-package outcomes.
type applyAllSucceeded struct {
	Package     string `json:"package"`
	FromVersion string `json:"fromVersion"`
	ToVersion   string `json:"toVersion"`
}
type applyAllFailed struct {
	Package string `json:"package"`
	Reason  string `json:"reason"`
}

// handleApplyAllUpdates applies updates for all (or a subset) of cached entries.
// Always returns HTTP 200; caller inspects failed[] length (red-team M2).
func (h *PackagesHandler) handleApplyAllUpdates(w http.ResponseWriter, r *http.Request) {
	// red-team H5: master-scope guard first.
	if !requireMasterScope(w, r) {
		return
	}
	if !enforcePackagesWriteLimit(w, r, "/v1/packages/updates/apply-all") {
		return
	}

	if h.Registry == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "update registry not configured"})
		return
	}

	locale := extractLocale(r)
	r.Body = http.MaxBytesReader(w, r.Body, 16384)

	// Body is optional. Peek for empty body; if present, bindJSON with strict
	// success (bindJSON writes 400 + returns false on parse failure — must NOT
	// be ignored, or we'd emit double HTTP responses on malformed JSON).
	var req applyAllRequest
	buf, berr := io.ReadAll(r.Body)
	if berr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + berr.Error()})
		return
	}
	if trimmed := strings.TrimSpace(string(buf)); trimmed != "" && trimmed != "{}" {
		if derr := json.Unmarshal(buf, &req); derr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + derr.Error()})
			return
		}
	}
	_ = locale // reserved for future i18n error messages

	updates, _ := h.Registry.Cache.Snapshot()
	start := time.Now()

	// Build index of cache entries by "source:name" for O(1) lookup.
	cacheIndex := make(map[string]skills.UpdateInfo, len(updates))
	for _, u := range updates {
		cacheIndex[u.Source+":"+u.Name] = u
	}

	// Resolve which entries to apply.
	type target struct {
		spec        string // "github:name" for output
		source, name string
		entry        skills.UpdateInfo
	}
	var targets []target

	if len(req.Packages) == 0 {
		// Apply all cached entries.
		for _, u := range updates {
			targets = append(targets, target{
				spec:   u.Source + ":" + u.Name,
				source: u.Source,
				name:   u.Name,
				entry:  u,
			})
		}
	} else {
		// Resolve each caller-supplied spec.
		for _, spec := range req.Packages {
			src, nm, ok := resolveUpdateSpec(spec)
			if !ok {
				// Invalid spec → immediate failed entry, continue.
				targets = append(targets, target{spec: spec})
				continue
			}
			key := src + ":" + nm
			entry, _ := cacheIndex[key] // red-team C6: comma-ok; zero value used if absent
			targets = append(targets, target{
				spec:   spec,
				source: src,
				name:   nm,
				entry:  entry,
			})
		}
	}

	var succeeded []applyAllSucceeded
	var failed []applyAllFailed

	for _, t := range targets {
		if t.source == "" {
			failed = append(failed, applyAllFailed{Package: t.spec, Reason: "invalid package spec"})
			continue
		}

		entry := t.entry
		fromVersion := entry.CurrentVersion
		toVersion := entry.LatestVersion
		if toVersion == "" {
			failed = append(failed, applyAllFailed{Package: t.spec, Reason: "no update available in cache"})
			continue
		}

		// Publish started.
		if h.Publisher != nil {
			h.Publisher.Broadcast(bus.Event{
				Name: eventPackageUpdateStarted,
				Payload: PackageUpdateStartedPayload{
					Source: t.source, Name: t.name,
					FromVersion: fromVersion, ToVersion: toVersion,
				},
				TenantID: uuid.Nil,
			})
		}

		slog.Info("packages: apply-all applying", "source", t.source, "name", t.name, "to", toVersion)
		lockKey := lockKeyForSource(t.source, t.name, entry.Meta)
		elapsed, err := h.Registry.Apply(r.Context(), t.source, lockKey, t.name, toVersion, entry.Meta)
		if err != nil {
			if h.Publisher != nil {
				h.Publisher.Broadcast(bus.Event{
					Name:     eventPackageUpdateFailed,
					Payload:  PackageUpdateFailedPayload{Source: t.source, Name: t.name, Reason: err.Error()},
					TenantID: uuid.Nil,
				})
			}
			slog.Warn("packages: apply-all item failed", "name", t.name, "error", err)
			failed = append(failed, applyAllFailed{Package: t.spec, Reason: err.Error()})
			// red-team M2: no context cancel on item failure — continue with remaining.
			continue
		}

		if h.Publisher != nil {
			h.Publisher.Broadcast(bus.Event{
				Name: eventPackageUpdateSucceeded,
				Payload: PackageUpdateSucceededPayload{
					Source: t.source, Name: t.name,
					FromVersion: fromVersion, ToVersion: toVersion,
					DurationMs: elapsed.Milliseconds(),
				},
				TenantID: uuid.Nil,
			})
		}
		succeeded = append(succeeded, applyAllSucceeded{
			Package: t.spec, FromVersion: fromVersion, ToVersion: toVersion,
		})
	}

	// red-team M2: always 200; caller inspects failed[] for partial failures.
	writeJSON(w, http.StatusOK, map[string]any{
		"succeeded":  nonNilSlice(succeeded),
		"failed":     nonNilSlice(failed),
		"durationMs": time.Since(start).Milliseconds(),
	})
}

// ---- helpers ----

// resolveUpdateSpec parses a package spec and returns (source, name, ok).
// Supported prefixes: "github:<name>", "pip:<name>", "npm:<name>".
//
// github: bare name "github:<name>" or full "github:owner/repo[@tag]".
// Bare github names are validated against validGitHubBareName; full specs
// are resolved via the manifest (repo may differ, e.g. cli/cli → gh).
// pip/npm: name is validated via the strict whitelist validators.
// Bare-name fallback (without colon) is NOT supported — all sources require
// an explicit "source:" prefix.
func resolveUpdateSpec(pkg string) (source, name string, ok bool) {
	prefix, rest, found := strings.Cut(pkg, ":")
	if !found || rest == "" {
		return "", "", false
	}
	switch prefix {
	case "github":
		// Full spec "github:owner/repo[@tag]" — extract bare name = repo component.
		if spec, err := skills.ParseGitHubSpec(pkg); err == nil {
			// Resolve name via manifest (repo may differ from binary name, e.g. cli/cli → gh).
			if installer := skills.DefaultGitHubInstaller(); installer != nil {
				if entries, lerr := installer.List(); lerr == nil {
					for _, e := range entries {
						if strings.EqualFold(e.Repo, spec.Owner+"/"+spec.Repo) {
							return "github", e.Name, true
						}
					}
				}
			}
			// Fallback: use repo name directly.
			return "github", spec.Repo, true
		}
		// Bare name form "github:<name>".
		if validGitHubBareName.MatchString(rest) {
			return "github", rest, true
		}
		return "", "", false
	case "pip":
		if err := skills.ValidatePipPackageName(rest); err != nil {
			return "", "", false
		}
		return "pip", rest, true
	case "npm":
		if err := skills.ValidateNpmPackageName(rest); err != nil {
			return "", "", false
		}
		return "npm", rest, true
	default:
		return "", "", false
	}
}

// nonNilSlice returns an empty non-nil slice when s is nil, so JSON encodes
// [] instead of null (red-team M7: frontend null-check safety).
func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// lockKeyForSource returns the canonical PackageLocker key for a given
// (source, name, meta) tuple. MUST match the key used by the installer for
// the same package (review CRIT-2).
//
// For github source: installer locks on parsed.Repo (repo-portion only,
// e.g. "lazygit"). Meta carries repo as "owner/repo" — extract the portion
// after "/". Fallback to name when meta is nil/missing (stale cache).
//
// For pip/npm: PackageLocker internally prefixes by source, so we return
// name directly (NOT "pip:name" or "npm:name").
func lockKeyForSource(source, name string, meta map[string]any) string {
	switch source {
	case "pip", "npm":
		return name
	case "github":
		if meta != nil {
			if v, ok := meta["repo"].(string); ok && v != "" {
				if i := strings.IndexByte(v, '/'); i > 0 && i < len(v)-1 {
					return v[i+1:]
				}
				return v
			}
		}
		return name
	default:
		return name
	}
}

