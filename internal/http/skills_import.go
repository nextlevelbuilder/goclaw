package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// handleSkillsImport imports a skills archive (POST /v1/skills/import).
func (h *SkillsHandler) handleSkillsImport(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())

	if h.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "db not configured")})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImportBodySize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "multipart parse: "+err.Error())})
		return
	}

	f, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "missing 'file' field")})
		return
	}
	defer f.Close()

	stream := r.URL.Query().Get("stream") == "true"
	if stream {
		flusher := initSSE(w)
		if flusher == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
			return
		}
		progressFn := func(ev ProgressEvent) { sendSSE(w, flusher, "progress", ev) }
		summary, importErr := h.doSkillsImport(r.Context(), f, userID, progressFn)
		if importErr != nil {
			sendSSE(w, flusher, "error", ProgressEvent{Phase: "import", Status: "error", Detail: importErr.Error()})
			return
		}
		// Import affects the importer's tenant scope — invalidate that
		// tenant's cached agents so they pick up the new skill set. If
		// imported under master, tid is the master tenant UUID, which
		// still yields a correct per-tenant router wipe.
		h.emitCacheInvalidate(bus.CacheKindSkills, "", store.TenantIDFromContext(r.Context()))
		sendSSE(w, flusher, "complete", summary)
		return
	}

	summary, err := h.doSkillsImport(r.Context(), f, userID, nil)
	if err != nil {
		slog.Error("skills.import", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	h.emitCacheInvalidate(bus.CacheKindSkills, "", store.TenantIDFromContext(r.Context()))
	writeJSON(w, http.StatusCreated, summary)
}

// SkillsImportSummary is returned after a successful skills import.
type SkillsImportSummary struct {
	SkillsImported int `json:"skills_imported"`
	SkillsSkipped  int `json:"skills_skipped"`
	GrantsApplied  int `json:"grants_applied"`
}

// doSkillsImport parses the skills tar.gz and creates skills + writes files + applies grants.
func (h *SkillsHandler) doSkillsImport(ctx context.Context, r io.Reader, userID string, progressFn func(ProgressEvent)) (*SkillsImportSummary, error) {
	entries, err := readTarGzEntries(r)
	if err != nil {
		return nil, err
	}

	// Group entries by skill slug: skills/{slug}/...
	type skillEntry struct {
		metadata []byte
		skillMD  []byte
		grants   []byte
	}
	bySlug := make(map[string]*skillEntry)

	for name, data := range entries {
		if !strings.HasPrefix(name, "skills/") {
			continue
		}
		rest := strings.TrimPrefix(name, "skills/")
		before, after, ok := strings.Cut(rest, "/")
		if !ok {
			continue
		}
		slug := before
		file := after
		if slug == "" || file == "" {
			continue
		}
		if bySlug[slug] == nil {
			bySlug[slug] = &skillEntry{}
		}
		switch file {
		case "metadata.json":
			bySlug[slug].metadata = data
		case "SKILL.md":
			bySlug[slug].skillMD = data
		case "grants.jsonl":
			bySlug[slug].grants = data
		}
	}

	summary := &SkillsImportSummary{}
	skillsDir := h.tenantSkillsDirForImport(ctx)

	for slug, entry := range bySlug {
		if entry.metadata == nil {
			slog.Warn("skills.import: missing metadata.json", "slug", slug)
			continue
		}

		// Security guard: scan SKILL.md for malicious content BEFORE any disk/DB write
		if entry.skillMD != nil {
			violations, safe := skills.GuardSkillContent(string(entry.skillMD))
			if !safe {
				slog.Warn("security.skills.import_rejected",
					"slug", slug,
					"violations", len(violations),
					"first_rule", violations[0].Reason)
				summary.SkillsSkipped++
				continue
			}
		}

		var meta struct {
			Name        string   `json:"name"`
			Slug        string   `json:"slug"`
			Description *string  `json:"description,omitempty"`
			Visibility  string   `json:"visibility"`
			Version     int      `json:"version"`
			Tags        []string `json:"tags,omitempty"`
		}
		if err := json.Unmarshal(entry.metadata, &meta); err != nil {
			slog.Warn("skills.import: parse metadata", "slug", slug, "error", err)
			continue
		}

		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "skill", Status: "running", Detail: slug})
		}

		skillDir := filepath.Join(skillsDir, sanitizeName(slug))
		skillFilePath := filepath.Join(skillDir, "SKILL.md")

		// Check if slug already exists in this tenant before writing files
		tid := tenantIDForSkillImport(ctx)
		var existing bool
		_ = h.db.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM skills WHERE slug = $1 AND tenant_id = $2)", slug, tid,
		).Scan(&existing)

		var skillID uuid.UUID
		if existing {
			_ = h.db.QueryRowContext(ctx,
				"SELECT id FROM skills WHERE slug = $1 AND tenant_id = $2", slug, tid,
			).Scan(&skillID)
			summary.SkillsSkipped++
		} else {
			// Write SKILL.md to filesystem only for new skills
			if err := os.MkdirAll(skillDir, 0755); err != nil {
				slog.Warn("skills.import: mkdir skill dir", "slug", slug, "error", err)
				continue
			}
			if entry.skillMD != nil {
				if err := os.WriteFile(skillFilePath, entry.skillMD, 0644); err != nil {
					slog.Warn("skills.import: write SKILL.md", "slug", slug, "error", err)
				}
			}

			skillID = uuid.Must(uuid.NewV7())
			visibility := meta.Visibility
			if visibility == "" {
				visibility = "internal"
			}
			version := meta.Version
			if version <= 0 {
				version = 1
			}
			_, err := h.db.ExecContext(ctx,
				`INSERT INTO skills (id, name, slug, description, owner_id, visibility, version, status,
				 is_system, file_path, file_size, tenant_id, created_at, updated_at)
				 VALUES ($1,$2,$3,$4,$5,$6,$7,'active',false,$8,0,$9,NOW(),NOW())`,
				skillID, meta.Name, slug, meta.Description,
				userID, visibility, version, skillFilePath, tid,
			)
			if err != nil {
				slog.Warn("skills.import: insert skill", "slug", slug, "error", err)
				continue
			}
			summary.SkillsImported++
		}

		// Apply grants
		if entry.grants != nil && skillID != uuid.Nil {
			grants, err := parseJSONL[pg.SkillGrantWithKey](entry.grants)
			if err != nil {
				slog.Warn("skills.import: parse grants", "slug", slug, "error", err)
			}
			for _, g := range grants {
				var agentID uuid.UUID
				if err := h.db.QueryRowContext(ctx,
					"SELECT id FROM agents WHERE agent_key = $1 AND tenant_id = $2", g.AgentKey, tid,
				).Scan(&agentID); err != nil {
					slog.Warn("skills.import: agent not found for grant", "key", g.AgentKey)
					continue
				}
				if err := pg.ImportSkillGrant(ctx, h.db, skillID, agentID, g.PinnedVersion, userID); err != nil {
					slog.Warn("skills.import: apply grant", "slug", slug, "agent", g.AgentKey, "error", err)
					continue
				}
				summary.GrantsApplied++
			}
		}

		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "skill", Status: "done", Detail: slug})
		}
	}

	// Bump skills version cache
	h.skills.BumpVersion()
	return summary, nil
}

// tenantSkillsDirForImport returns the skills filesystem directory for import, scoped to tenant.
func (h *SkillsHandler) tenantSkillsDirForImport(ctx context.Context) string {
	tid := store.TenantIDFromContext(ctx)
	slug := store.TenantSlugFromContext(ctx)
	return config.TenantSkillsStoreDir(h.dataDir, tid, slug)
}

// tenantIDForSkillImport returns tenant UUID for skill INSERT, falling back to MasterTenantID.
func tenantIDForSkillImport(ctx context.Context) uuid.UUID {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return store.MasterTenantID
	}
	return tid
}
