package meow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// IngestResult reports the outcome of ingesting one draft bundle.
type IngestResult struct {
	Held      bool      // true = a content problem; no draft row was created
	Reason    string    // why it was held (for the operator)
	PostID    uuid.UUID // set when a draft was upserted
	ImagePath string    // container-visible path the draft references
}

// IngestDraft validates a draft bundle, transports its image into the asset root
// (the container-visible meow-assets dir), and upserts an mp_content_posts
// draft. A content problem (bad bundle, unknown channel, missing image,
// off-allowlist button) yields Held=true with a Reason and NO database row — an
// invalid bundle can never become an approvable/publishable post. err is
// returned only for unexpected IO/DB failures.
//
// bundleDir is the on-server directory holding the bundle's image; assetRoot is
// the publish allowed-root (e.g. /app/data/meow-assets). The persisted
// image_path is always the copied path under assetRoot — never a source path.
func IngestDraft(ctx context.Context, st store.MeowStore, tenantID uuid.UUID, b *DraftBundle, bundleDir, assetRoot string) (IngestResult, error) {
	if err := b.Validate(); err != nil {
		return IngestResult{Held: true, Reason: err.Error()}, nil
	}

	ch, err := st.GetChannelByHandle(ctx, tenantID, b.Handle)
	if errors.Is(err, store.ErrMeowChannelNotFound) {
		return IngestResult{Held: true, Reason: "unknown channel " + b.Handle}, nil
	}
	if err != nil {
		return IngestResult{}, err
	}

	// Exact-URL allowlist (per channel) + host allowlist for every button.
	registered, err := registeredURLs(ch.ButtonSet)
	if err != nil {
		return IngestResult{}, fmt.Errorf("channel button set: %w", err)
	}
	hosts := DefaultButtonHostAllowlist()
	for _, btn := range b.Buttons {
		if err := ValidateButtonURL(btn.URL, registered, hosts); err != nil {
			return IngestResult{Held: true, Reason: err.Error()}, nil
		}
	}

	// Source image must exist and resolve inside the bundle dir (no escape).
	src := filepath.Join(bundleDir, b.Image)
	if _, err := ValidateImagePath(src, []string{bundleDir}); err != nil {
		return IngestResult{Held: true, Reason: "image: " + err.Error()}, nil
	}

	date, _ := b.Date() // already validated
	dest := filepath.Join(assetRoot, "drafts", ch.BrandKey, date.Format(draftDateLayout)+".webp")
	if err := copyFile(src, dest); err != nil {
		return IngestResult{}, fmt.Errorf("transport image: %w", err)
	}
	// The persisted path must resolve inside the asset root.
	validated, err := ValidateImagePath(dest, []string{assetRoot})
	if err != nil {
		return IngestResult{}, fmt.Errorf("post-copy validation: %w", err)
	}

	buttons, err := json.Marshal(b.Buttons)
	if err != nil {
		return IngestResult{}, err
	}
	post := &store.MpContentPost{
		TenantID:      tenantID,
		ChannelID:     ch.ID,
		ScheduledDate: date,
		Status:        store.MpPostDraft,
		KoText:        b.KoText,
		EnText:        b.EnText,
		ImagePath:     validated,
		Buttons:       buttons,
	}
	if err := st.UpsertDraftPost(ctx, post); err != nil {
		return IngestResult{}, fmt.Errorf("upsert draft: %w", err)
	}
	return IngestResult{PostID: post.ID, ImagePath: validated}, nil
}

// copyFile copies src → dest, creating dest's parent dir (0755) and the file
// (0644). The deferred close captures a flush error.
func copyFile(src, dest string) (err error) {
	if e := os.MkdirAll(filepath.Dir(dest), 0o755); e != nil {
		return e
	}
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if e != nil {
		return e
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}
