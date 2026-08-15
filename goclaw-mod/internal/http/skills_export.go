package http

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

	exportReq, err := parseSkillExportRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	skills, err := pg.ExportSkills(r.Context(), h.db, pg.SkillExportSelection{
		IDs:           exportReq.IDs,
		IncludeSystem: exportReq.IncludeSystem,
	})
	if err != nil {
		slog.Error("skills.export.query", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError)})
		return
	}

	stream := r.URL.Query().Get("stream") == "true"
	fileName := skillExportFileName(skills, exportReq.Format, time.Now().UTC())

	if stream {
		flusher := initSSE(w)
		if flusher == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
			return
		}

		tmpFile, err := os.CreateTemp("", "goclaw-skills-export-*"+exportReq.Format.Extension)
		if err != nil {
			sendSSE(w, flusher, "error", ProgressEvent{Phase: "init", Status: "error", Detail: "failed to create temp file"})
			return
		}
		tmpPath := tmpFile.Name()

		progressFn := func(ev ProgressEvent) { sendSSE(w, flusher, "progress", ev) }
		buildErr := h.writeSkillsExportArchive(r.Context(), tmpFile, progressFn, exportReq, skills)
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
			"file_name":    fileName,
		})
		return
	}

	w.Header().Set("Content-Type", exportReq.Format.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	if err := h.writeSkillsExportArchive(r.Context(), w, nil, exportReq, skills); err != nil {
		slog.Error("skills.export.direct", "error", err)
	}
}

// writeSkillsExportArchive builds the skills archive.
func (h *SkillsHandler) writeSkillsExportArchive(ctx context.Context, w io.Writer, progressFn func(ProgressEvent), req skillExportRequest, skills []pg.CustomSkillExport) error {
	lw := &limitedWriter{w: w, limit: maxExportSize}
	archive, err := newSkillArchiveWriter(lw, req.Format)
	if err != nil {
		return err
	}
	defer archive.Close()

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
		if err := archive.AddFile(prefix+"metadata.json", metaJSON); err != nil {
			return fmt.Errorf("write %smetadata.json: %w", prefix, err)
		}

		for _, root := range h.skillExportRoots(sk) {
			if err := addSkillDirectoryToArchive(archive, root, prefix); err != nil {
				slog.Warn("skills.export: write skill directory", "slug", sk.Slug, "root", root, "error", err)
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
			} else if err := archive.AddFile(prefix+"grants.jsonl", data); err != nil {
				return fmt.Errorf("write %sgrants.jsonl: %w", prefix, err)
			}
		}
	}

	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "skills", Status: "done", Current: len(skills), Total: len(skills), Detail: fmt.Sprintf("%d skills exported", len(skills))})
	}

	return archive.Close()
}

func (h *SkillsHandler) skillExportRoots(sk pg.CustomSkillExport) []string {
	if sk.FilePath == "" {
		return nil
	}
	root := config.ExpandHome(store.SkillBaseDir(sk.FilePath))
	return readableSkillRoots(root, sk.Slug, sk.IsSystem, h.bundledDir)
}

type skillExportFormat struct {
	Canonical   string
	Extension   string
	ContentType string
}

type skillExportRequest struct {
	Format        skillExportFormat
	IDs           []uuid.UUID
	IncludeSystem bool
}

func parseSkillExportRequest(r *http.Request) (skillExportRequest, error) {
	format, err := parseSkillExportFormat(r.URL.Query().Get("format"))
	if err != nil {
		return skillExportRequest{}, err
	}
	ids, err := parseSkillExportIDs(r)
	if err != nil {
		return skillExportRequest{}, err
	}
	return skillExportRequest{
		Format:        format,
		IDs:           ids,
		IncludeSystem: strings.EqualFold(r.URL.Query().Get("include_system"), "true"),
	}, nil
}

func parseSkillExportFormat(raw string) (skillExportFormat, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "tar.gz", "tgz":
		return skillExportFormat{Canonical: "tar.gz", Extension: ".tar.gz", ContentType: "application/gzip"}, nil
	case "zip":
		return skillExportFormat{Canonical: "zip", Extension: ".zip", ContentType: "application/zip"}, nil
	default:
		return skillExportFormat{}, fmt.Errorf("unsupported skills export format %q", raw)
	}
}

func parseSkillExportIDs(r *http.Request) ([]uuid.UUID, error) {
	raw := append([]string{}, r.URL.Query()["id"]...)
	raw = append(raw, r.URL.Query()["ids"]...)
	var ids []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, group := range raw {
		for part := range strings.SplitSeq(group, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := uuid.Parse(part)
			if err != nil {
				return nil, fmt.Errorf("invalid skill id %q", part)
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}

type skillArchiveWriter interface {
	AddFile(name string, data []byte) error
	AddFileReader(name string, size int64, modTime time.Time, r io.Reader) error
	Close() error
	ContentType() string
	Extension() string
}

func newSkillArchiveWriter(w io.Writer, format skillExportFormat) (skillArchiveWriter, error) {
	switch format.Canonical {
	case "zip":
		return &skillZipArchiveWriter{zw: zip.NewWriter(w)}, nil
	case "tar.gz":
		gw := gzip.NewWriter(w)
		return &skillTarGzArchiveWriter{gw: gw, tw: tar.NewWriter(gw)}, nil
	default:
		return nil, fmt.Errorf("unsupported skills export format %q", format.Canonical)
	}
}

type skillTarGzArchiveWriter struct {
	gw     *gzip.Writer
	tw     *tar.Writer
	closed bool
}

func (w *skillTarGzArchiveWriter) AddFile(name string, data []byte) error {
	return w.AddFileReader(name, int64(len(data)), time.Now(), bytes.NewReader(data))
}

func (w *skillTarGzArchiveWriter) AddFileReader(name string, size int64, modTime time.Time, r io.Reader) error {
	if err := validateArchivePath(name); err != nil {
		return err
	}
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o644,
		Size:    size,
		ModTime: modTime,
	}
	if err := w.tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := io.Copy(w.tw, r)
	return err
}

func (w *skillTarGzArchiveWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if err := w.tw.Close(); err != nil {
		w.gw.Close()
		return err
	}
	return w.gw.Close()
}

func (w *skillTarGzArchiveWriter) ContentType() string { return "application/gzip" }
func (w *skillTarGzArchiveWriter) Extension() string   { return ".tar.gz" }

type skillZipArchiveWriter struct {
	zw     *zip.Writer
	closed bool
}

func (w *skillZipArchiveWriter) AddFile(name string, data []byte) error {
	return w.AddFileReader(name, int64(len(data)), time.Now(), bytes.NewReader(data))
}

func (w *skillZipArchiveWriter) AddFileReader(name string, size int64, modTime time.Time, r io.Reader) error {
	if err := validateArchivePath(name); err != nil {
		return err
	}
	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
	hdr.SetModTime(modTime)
	hdr.UncompressedSize64 = uint64(size)
	fw, err := w.zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(fw, r)
	return err
}

func (w *skillZipArchiveWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.zw.Close()
}

func (w *skillZipArchiveWriter) ContentType() string { return "application/zip" }
func (w *skillZipArchiveWriter) Extension() string   { return ".zip" }

func addSkillDirectoryToArchive(archive skillArchiveWriter, root, prefix string) error {
	root = filepath.Clean(root)
	if root == "." || root == "" {
		return nil
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil
	}
	rootReal = filepath.Clean(rootReal)
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if skillsExportArtifact(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		archivePath := prefix + sanitizeRelPath(rel)
		if archivePath == prefix {
			return nil
		}
		return addValidatedSkillFileToArchive(archive, rootReal, path, archivePath)
	})
}

func skillsExportArtifact(rel string) bool {
	name := filepath.Base(rel)
	return name == ".DS_Store" || name == "Thumbs.db" || rel == "metadata.json" || rel == "grants.jsonl"
}

func addValidatedSkillFileToArchive(archive skillArchiveWriter, rootReal, path, archivePath string) error {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		slog.Warn("security.skills_export_path_unresolved", "path", path, "error", err)
		return nil
	}
	realPath = filepath.Clean(realPath)
	if !pathWithinDir(realPath, rootReal) || hasDeniedFilePrefix(realPath) {
		slog.Warn("security.skills_export_path_escape", "path", path, "resolved", realPath, "root", rootReal)
		return nil
	}

	realInfo, err := os.Stat(realPath)
	if err != nil {
		return nil
	}
	fileInfo, err := file.Stat()
	if err != nil {
		slog.Warn("security.skills_export_open_race", "path", realPath, "error", err)
		return nil
	}
	if fileInfo.IsDir() || realInfo.IsDir() || !fileInfo.Mode().IsRegular() || !os.SameFile(realInfo, fileInfo) {
		slog.Warn("security.skills_export_open_race", "path", realPath)
		return nil
	}

	return archive.AddFileReader(archivePath, fileInfo.Size(), fileInfo.ModTime(), file)
}

func validateArchivePath(name string) error {
	if name == "" || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") || filepath.IsAbs(name) {
		return errors.New("invalid archive path")
	}
	name = filepath.ToSlash(name)
	if strings.Contains(name, "\\") || strings.Contains(name, ":") {
		return errors.New("invalid archive path")
	}
	for part := range strings.SplitSeq(name, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid archive path")
		}
	}
	return nil
}

func skillExportFileName(skills []pg.CustomSkillExport, format skillExportFormat, now time.Time) string {
	if len(skills) == 1 {
		sk := skills[0]
		return fmt.Sprintf("goclaw-skill-%s-v%d%s", sanitizeName(sk.Slug), sk.Version, format.Extension)
	}
	return fmt.Sprintf("goclaw-skills-export-%s%s", now.Format("20060102-1504"), format.Extension)
}
