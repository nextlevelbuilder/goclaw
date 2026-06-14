package meow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// DraftBundle is the offline-prepped content for one channel-day, written by the
// draft-prep workflow as <project>/marketing/meow/drafts/<date>.json next to its
// <date>.webp image. Ingest reads it server-side, validates, transports the
// image into the meow-assets root, and upserts an mp_content_posts draft.
type DraftBundle struct {
	Handle        string   `json:"handle"`         // "@kingboardgamesofficial"
	ScheduledDate string   `json:"scheduled_date"` // "YYYY-MM-DD" (channel-local)
	KoText        string   `json:"ko_text"`
	EnText        string   `json:"en_text"`
	Image         string   `json:"image"`   // bare WebP filename within the bundle dir (no path)
	Buttons       []Button `json:"buttons"` // label/url; exact-URL allowlist enforced at ingest
}

const draftDateLayout = "2006-01-02"

// ParseDraftBundle decodes a bundle JSON (strict: unknown fields rejected so a
// typo'd key never silently drops content).
func ParseDraftBundle(data []byte) (*DraftBundle, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var b DraftBundle
	if err := dec.Decode(&b); err != nil {
		return nil, fmt.Errorf("parse draft bundle: %w", err)
	}
	return &b, nil
}

// Date parses ScheduledDate.
func (b *DraftBundle) Date() (time.Time, error) {
	return time.Parse(draftDateLayout, b.ScheduledDate)
}

// Validate checks structural validity only (no channel, DB, or filesystem). A
// failure here is a "hold" reason at ingest — the bundle never becomes a draft.
// The exact button-URL allowlist (per channel) and the image's presence/path are
// enforced at ingest, not here.
func (b *DraftBundle) Validate() error {
	if !strings.HasPrefix(strings.TrimSpace(b.Handle), "@") {
		return fmt.Errorf("handle must start with @ (got %q)", b.Handle)
	}
	if _, err := b.Date(); err != nil {
		return fmt.Errorf("scheduled_date must be YYYY-MM-DD (got %q)", b.ScheduledDate)
	}
	if strings.TrimSpace(b.KoText) == "" && strings.TrimSpace(b.EnText) == "" {
		return fmt.Errorf("at least one of ko_text/en_text is required")
	}
	img := strings.TrimSpace(b.Image)
	if img == "" {
		return fmt.Errorf("image filename is required")
	}
	// Image must be a bare filename within the bundle dir — never a path. The
	// real on-disk path is resolved + containment-checked at ingest.
	if strings.ContainsAny(img, `/\`) || strings.Contains(img, "..") {
		return fmt.Errorf("image must be a bare filename, not a path (got %q)", b.Image)
	}
	for i, btn := range b.Buttons {
		if strings.TrimSpace(btn.Label) == "" {
			return fmt.Errorf("button[%d] label is empty", i)
		}
		if !strings.HasPrefix(strings.TrimSpace(btn.URL), "https://") {
			return fmt.Errorf("button[%d] url must be https (got %q)", i, btn.URL)
		}
	}
	return nil
}
