package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// TestPersistMedia_NamingScheme verifies that persistMedia produces:
//   - `{slug}-{8hex}{ext}` when Filename is present and sanitizes to a non-empty stem
//   - `{uuid}{ext}` UUID fallback when Filename is empty or sanitizes to ""
//
// It covers Vietnamese/CJK preservation, image re-encode via SanitizeImage,
// and the voice-note (empty Filename) fallback path.
func TestPersistMedia_NamingScheme(t *testing.T) {
	workspace := t.TempDir()

	writeTmp := func(t *testing.T, name, data string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, []byte(data), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		return p
	}

	uuidPat := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.[a-z0-9]+$`)
	slugPat := regexp.MustCompile(`^(.+)-[0-9a-f]{8}\.[a-z0-9]+$`)

	type tc struct {
		name       string
		filename   string
		content    string
		mime       string
		wantSlug   string // prefix (stem) expected in disk name; empty means UUID fallback
		wantExtPat string // extension regex on disk (lowercase)
	}

	cases := []tc{
		{
			name:       "vietnamese_pdf",
			filename:   "Báo cáo Q4.pdf",
			content:    "%PDF-1.4\n%fake",
			mime:       "application/pdf",
			wantSlug:   "bao-cao-q4",
			wantExtPat: `\.pdf`,
		},
		{
			name:       "cjk_preserved",
			filename:   "猫の写真.png",
			content:    "not a real png",
			mime:       "application/octet-stream", // bypass image sanitize path
			wantSlug:   "猫の写真",
			wantExtPat: `\.bin`,
		},
		{
			name:       "empty_filename_uuid_fallback",
			filename:   "",
			content:    "audio bytes",
			mime:       "audio/ogg",
			wantSlug:   "",
			wantExtPat: `\.ogg`,
		},
		{
			name:       "filename_all_unsafe_fallback",
			filename:   "///",
			content:    "pdf body",
			mime:       "application/pdf",
			wantSlug:   "",
			wantExtPat: `\.pdf`,
		},
		{
			name:       "zip_archive_preserves_archive_extension",
			filename:   "codex.zip",
			content:    "zip bytes",
			mime:       "application/zip",
			wantSlug:   "codex",
			wantExtPat: `\.zip`,
		},
	}

	var loop Loop
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := writeTmp(t, "src.bin", c.content)
			refs := loop.persistMedia("session-key-test", []bus.MediaFile{{
				Path:     src,
				MimeType: c.mime,
				Filename: c.filename,
			}}, workspace)
			if len(refs) != 1 {
				t.Fatalf("got %d refs, want 1", len(refs))
			}
			base := filepath.Base(refs[0].Path)
			if c.wantSlug == "" {
				// UUID fallback
				if !uuidPat.MatchString(base) {
					t.Fatalf("expected UUID-pattern disk name, got %q", base)
				}
				return
			}
			m := slugPat.FindStringSubmatch(base)
			if m == nil {
				t.Fatalf("expected slug+8hex disk name, got %q", base)
			}
			if m[1] != c.wantSlug {
				t.Fatalf("stem = %q, want %q (full: %q)", m[1], c.wantSlug, base)
			}
			extOk := regexp.MustCompile(c.wantExtPat + `$`)
			if !extOk.MatchString(base) {
				t.Fatalf("disk name %q does not match ext pattern %q", base, c.wantExtPat)
			}
		})
	}
}

func TestPersistMedia_BackfilledThreadAttachmentsCreateToolRefs(t *testing.T) {
	workspace := t.TempDir()
	imagePath := filepath.Join(t.TempDir(), "diagram.png")
	if err := os.WriteFile(imagePath, []byte("not a real png"), 0644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	docPath := filepath.Join(t.TempDir(), "brief.pdf")
	if err := os.WriteFile(docPath, []byte("%PDF-1.4\n%fake"), 0644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	var loop Loop
	refs := loop.persistMedia("discord-thread-session", []bus.MediaFile{
		{Path: imagePath, MimeType: "image/png", Filename: "diagram.png"},
		{Path: docPath, MimeType: "application/pdf", Filename: "brief.pdf"},
	}, workspace)

	if len(refs) != 2 {
		t.Fatalf("refs = %d, want 2: %#v", len(refs), refs)
	}
	gotKinds := map[string]bool{}
	for _, ref := range refs {
		gotKinds[ref.Kind] = true
		if ref.Path == "" {
			t.Fatalf("ref missing persisted path: %#v", ref)
		}
		if _, err := os.Stat(ref.Path); err != nil {
			t.Fatalf("persisted file missing for %s: %v", ref.Kind, err)
		}
	}
	if !gotKinds["image"] {
		t.Fatalf("missing image ref: %#v", refs)
	}
	if !gotKinds["document"] {
		t.Fatalf("missing document ref: %#v", refs)
	}
}
