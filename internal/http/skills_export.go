package http

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// SetDB injects the raw DB handle needed for export/import direct queries.
func (h *SkillsHandler) SetDB(db *sql.DB) {
	h.db = db
}

// handleSkillsExportPreview returns skill export counts without building the archive.
func (h *SkillsHandler) handleSkillsExportPreview(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	if h.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "db not configured")})
		return
	}

	preview, err := pg.ExportSkillsPreview(r.Context(), h.db)
	if err != nil {
		slog.Error("skills.export.preview", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError)})
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

// handleSkillsExport builds and streams (or SSE-wraps) a skills tar.gz archive.
func (h *SkillsHandler) handleSkillsExport(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())

	if h.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "db not configured")})
		return
	}

	stream := r.URL.Query().Get("stream") == "true"
	fileName := fmt.Sprintf("skills-%s.tar.gz", time.Now().UTC().Format("20060102"))

	if stream {
		flusher := initSSE(w)
		if flusher == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
			return
		}

		tmpFile, err := os.CreateTemp("", "goclaw-skills-export-*.tar.gz")
		if err != nil {
			sendSSE(w, flusher, "error", ProgressEvent{Phase: "init", Status: "error", Detail: "failed to create temp file"})
			return
		}
		tmpPath := tmpFile.Name()

		progressFn := func(ev ProgressEvent) { sendSSE(w, flusher, "progress", ev) }
		buildErr := h.writeSkillsExportArchive(r.Context(), tmpFile, progressFn)
		tmpFile.Close()

		if buildErr != nil {
			slog.Error("skills.export.sse", "error", buildErr)
			sendSSE(w, flusher, "error", ProgressEvent{Phase: "archive", Status: "error", Detail: buildErr.Error()})
			os.Remove(tmpPath)
			return
		}

		token := storeExportToken("skills", userID, tmpPath, fileName)
		sendSSE(w, flusher, "complete", map[string]string{
			"download_url": "/v1/export/download/" + token,
		})
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	if err := h.writeSkillsExportArchive(r.Context(), w, nil); err != nil {
		slog.Error("skills.export.direct", "error", err)
	}
}

// writeSkillsExportArchive builds the skills tar.gz: skills/{slug}/metadata.json + SKILL.md + grants.jsonl per skill.
func (h *SkillsHandler) writeSkillsExportArchive(ctx context.Context, w io.Writer, progressFn func(ProgressEvent)) error {
	lw := &limitedWriter{w: w, limit: maxExportSize}
	gw := gzip.NewWriter(lw)
	tw := tar.NewWriter(gw)

	skills, err := pg.ExportCustomSkills(ctx, h.db)
	if err != nil {
		tw.Close()
		gw.Close()
		return fmt.Errorf("query custom skills: %w", err)
	}

	for i, sk := range skills {
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "skills", Status: "running", Current: i + 1, Total: len(skills), Detail: sk.Slug})
		}

		prefix := "skills/" + sanitizeName(sk.Slug) + "/"

		// metadata.json — strip FilePath from exported metadata
		type exportMeta struct {
			ID          string   `json:"id"`
			Name        string   `json:"name"`
			Slug        string   `json:"slug"`
			Description *string  `json:"description,omitempty"`
			Visibility  string   `json:"visibility"`
			Version     int      `json:"version"`
			Tags        []string `json:"tags,omitempty"`
		}
		meta := exportMeta{
			ID:          sk.ID,
			Name:        sk.Name,
			Slug:        sk.Slug,
			Description: sk.Description,
			Visibility:  sk.Visibility,
			Version:     sk.Version,
			Tags:        sk.Tags,
		}
		metaJSON, err := jsonIndent(meta)
		if err != nil {
			slog.Warn("skills.export: marshal metadata", "slug", sk.Slug, "error", err)
			continue
		}
		if err := addToTar(tw, prefix+"metadata.json", metaJSON); err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("write %smetadata.json: %w", prefix, err)
		}

		// SKILL.md — read from filesystem
		if sk.FilePath != "" {
			fullPath := config.ExpandHome(sk.FilePath)
			if data, err := os.ReadFile(fullPath); err == nil {
				if err := addToTar(tw, prefix+"SKILL.md", data); err != nil {
					slog.Warn("skills.export: write SKILL.md", "slug", sk.Slug, "error", err)
				}
			} else {
				slog.Warn("skills.export: read SKILL.md", "slug", sk.Slug, "path", fullPath, "error", err)
			}
		}

		// grants.jsonl
		skillID, err := uuid.Parse(sk.ID)
		if err != nil {
			slog.Warn("skills.export: invalid skill id", "id", sk.ID)
			continue
		}
		grants, err := pg.ExportSkillGrantsWithAgentKey(ctx, h.db, skillID)
		if err != nil {
			slog.Warn("skills.export: query grants", "slug", sk.Slug, "error", err)
		}
		if len(grants) > 0 {
			data, err := marshalJSONL(grants)
			if err != nil {
				slog.Warn("skills.export: marshal grants", "slug", sk.Slug, "error", err)
			} else if err := addToTar(tw, prefix+"grants.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write %sgrants.jsonl: %w", prefix, err)
			}
		}
	}

	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "skills", Status: "done", Current: len(skills), Total: len(skills), Detail: fmt.Sprintf("%d skills exported", len(skills))})
	}

	if err := tw.Close(); err != nil {
		gw.Close()
		return fmt.Errorf("close tar: %w", err)
	}
	return gw.Close()
}
