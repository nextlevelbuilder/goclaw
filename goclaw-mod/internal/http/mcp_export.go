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

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// SetDB injects the raw DB handle needed for MCP export/import direct queries.
func (h *MCPHandler) SetDB(db *sql.DB) {
	h.db = db
}

// handleMCPExportPreview returns MCP export counts without building the archive.
func (h *MCPHandler) handleMCPExportPreview(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	if h.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "db not configured")})
		return
	}

	preview, err := pg.ExportMCPPreview(r.Context(), h.db)
	if err != nil {
		slog.Error("mcp.export.preview", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError)})
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

// handleMCPExport builds and streams (or SSE-wraps) a MCP servers tar.gz archive.
func (h *MCPHandler) handleMCPExport(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())

	if h.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "db not configured")})
		return
	}

	stream := r.URL.Query().Get("stream") == "true"
	fileName := fmt.Sprintf("mcp-servers-%s.tar.gz", time.Now().UTC().Format("20060102"))

	if stream {
		flusher := initSSE(w)
		if flusher == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
			return
		}

		tmpFile, err := os.CreateTemp("", "goclaw-mcp-export-*.tar.gz")
		if err != nil {
			sendSSE(w, flusher, "error", ProgressEvent{Phase: "init", Status: "error", Detail: "failed to create temp file"})
			return
		}
		tmpPath := tmpFile.Name()

		progressFn := func(ev ProgressEvent) { sendSSE(w, flusher, "progress", ev) }
		buildErr := h.writeMCPExportArchive(r.Context(), tmpFile, progressFn)
		tmpFile.Close()

		if buildErr != nil {
			slog.Error("mcp.export.sse", "error", buildErr)
			sendSSE(w, flusher, "error", ProgressEvent{Phase: "archive", Status: "error", Detail: buildErr.Error()})
			os.Remove(tmpPath)
			return
		}

		token := storeExportToken("mcp", userID, tmpPath, fileName)
		sendSSE(w, flusher, "complete", map[string]string{
			"download_url": "/v1/export/download/" + token,
		})
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	if err := h.writeMCPExportArchive(r.Context(), w, nil); err != nil {
		slog.Error("mcp.export.direct", "error", err)
	}
}

// writeMCPExportArchive builds the MCP tar.gz: servers.jsonl + grants.jsonl.
func (h *MCPHandler) writeMCPExportArchive(ctx context.Context, w io.Writer, progressFn func(ProgressEvent)) error {
	lw := &limitedWriter{w: w, limit: maxExportSize}
	gw := gzip.NewWriter(lw)
	tw := tar.NewWriter(gw)

	servers, err := pg.ExportMCPServers(ctx, h.db)
	if err != nil {
		tw.Close()
		gw.Close()
		return fmt.Errorf("query mcp servers: %w", err)
	}

	if len(servers) > 0 {
		data, err := marshalJSONL(servers)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("marshal servers: %w", err)
		}
		if err := addToTar(tw, "servers.jsonl", data); err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("write servers.jsonl: %w", err)
		}
	}

	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "servers", Status: "done", Current: len(servers), Total: len(servers), Detail: fmt.Sprintf("%d servers exported", len(servers))})
	}

	grants, err := pg.ExportMCPGrantsWithKeys(ctx, h.db)
	if err != nil {
		slog.Warn("mcp.export: query grants failed", "error", err)
	}
	if len(grants) > 0 {
		data, err := marshalJSONL(grants)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("marshal grants: %w", err)
		}
		if err := addToTar(tw, "grants.jsonl", data); err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("write grants.jsonl: %w", err)
		}
	}

	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "grants", Status: "done", Current: len(grants), Total: len(grants), Detail: fmt.Sprintf("%d agent grants", len(grants))})
	}

	if err := tw.Close(); err != nil {
		gw.Close()
		return fmt.Errorf("close tar: %w", err)
	}
	return gw.Close()
}
