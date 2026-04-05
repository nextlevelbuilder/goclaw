package agent

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// collectRefsByKind gathers MediaRefs of a given kind from message history
// (reverse order) and current-turn refs. Historical first, current last.
func collectRefsByKind(messages []providers.Message, currentRefs []providers.MediaRef, kind string) []providers.MediaRef {
	var refs []providers.MediaRef
	for i := len(messages) - 1; i >= 0; i-- {
		for _, ref := range messages[i].MediaRefs {
			if ref.Kind == kind {
				refs = append(refs, ref)
			}
		}
	}
	for _, ref := range currentRefs {
		if ref.Kind == kind {
			refs = append(refs, ref)
		}
	}
	return refs
}

// enrichInputMedia processes incoming media (images, documents, audio, video),
// persists them, enriches messages with media tags, and populates context
// with refs for tool access. Returns updated context, modified messages, and current-turn media refs.
func (l *Loop) enrichInputMedia(ctx context.Context, req *RunRequest, messages []providers.Message) (context.Context, []providers.Message, []providers.MediaRef) {
	// 1b. Determine image routing strategy.
	// If read_image tool has a dedicated vision provider, images are NOT attached inline
	// to the main LLM — the agent calls read_image tool instead. This avoids sending
	// images to providers that don't support vision or have strict content filters.
	deferToReadImageTool := l.hasReadImageProvider()

	if !deferToReadImageTool {
		// Inline mode: reload historical images directly into messages for main provider.
		l.reloadMediaForMessages(messages, maxMediaReloadMessages)
	}

	// 2. Process media: sanitize images, persist to media store.
	var mediaRefs []providers.MediaRef
	if len(req.Media) > 0 {
		mediaRefs = l.persistMedia(req.SessionKey, req.Media, tools.ToolWorkspaceFromCtx(ctx))
		// Load current-turn images from persisted refs (Path is always set for new uploads).
		var imageFiles []bus.MediaFile
		for _, ref := range mediaRefs {
			if ref.Kind == "image" && ref.Path != "" {
				imageFiles = append(imageFiles, bus.MediaFile{Path: ref.Path, MimeType: ref.MimeType})
			}
		}
		if deferToReadImageTool {
			// File-ref mode: images primarily accessed via read_image(path=...).
			// Still load into context as fallback — if LLM omits the path param,
			// read_image can fall back to context images. This costs Go memory
			// but NOT LLM tokens (base64 is in Go context, not sent to provider).
			if images := loadImages(imageFiles); len(images) > 0 {
				ctx = tools.WithMediaImages(ctx, images)
			}
			slog.Info("vision: file-ref mode, images accessible via read_image tool",
				"count", len(imageFiles), "agent", l.id)
		} else if images := loadImages(imageFiles); len(images) > 0 {
			// Inline mode: read files, base64 encode, attach to message + context.
			messages[len(messages)-1].Images = images
			ctx = tools.WithMediaImages(ctx, images)
			slog.Info("vision: attached images inline to main provider", "count", len(images), "agent", l.id)
		}
	}

	// 2a. Load historical images into context for read_image tool.
	// Both modes need this: inline mode for main LLM, file-ref mode as fallback
	// when LLM calls read_image without the path param.
	if l.mediaStore != nil {
		ctx = l.loadHistoricalImagesForTool(ctx, mediaRefs, messages)
	}

	// 2b. Collect document MediaRefs (historical + current) for read_document tool.
	if docRefs := collectRefsByKind(messages, mediaRefs, "document"); len(docRefs) > 0 {
		ctx = tools.WithMediaDocRefs(ctx, docRefs)
		// Enrich the last user message with persisted file paths so skills can access
		// documents via exec (e.g. pypdf). Only for current-turn refs (just persisted).
		l.enrichDocumentPaths(messages, mediaRefs)
	}

	// 2c. Collect audio MediaRefs (historical + current) for read_audio tool.
	if audioRefs := collectRefsByKind(messages, mediaRefs, "audio"); len(audioRefs) > 0 {
		ctx = tools.WithMediaAudioRefs(ctx, audioRefs)
		l.enrichAudioIDs(messages, mediaRefs)
	}

	// 2d. Collect video MediaRefs (historical + current) for read_video tool.
	if videoRefs := collectRefsByKind(messages, mediaRefs, "video"); len(videoRefs) > 0 {
		ctx = tools.WithMediaVideoRefs(ctx, videoRefs)
		l.enrichVideoIDs(messages, mediaRefs)
	}

	// 2e. Enrich <media:image> tags with persisted media IDs so the LLM
	// knows images were received and stored (consistent with audio/video enrichment).
	l.enrichImageIDs(messages, mediaRefs)

	// 2e-ii. In file-ref mode, enrich ALL user messages' image tags with file paths.
	// This enables read_image(path=...) for both current and historical images.
	if deferToReadImageTool {
		l.enrichImagePaths(messages)
	}

	// 2f. Collect all media file paths for team workspace auto-collect.
	// When the leader calls team_tasks(create), these paths are copied to the
	// team workspace so members can access attached files.
	if len(mediaRefs) > 0 && l.mediaStore != nil {
		var mediaPaths []string
		for _, ref := range mediaRefs {
			// Prefer workspace-local path (.uploads/) over canonical .media/ path.
			if ref.Path != "" {
				mediaPaths = append(mediaPaths, ref.Path)
			} else if p, err := l.mediaStore.LoadPath(ref.ID); err == nil {
				mediaPaths = append(mediaPaths, p)
			}
		}
		if len(mediaPaths) > 0 {
			ctx = tools.WithRunMediaPaths(ctx, mediaPaths)
			// Extract original filenames from <media:document name="X" path="Y"> tags
			// in the last user message (enriched in step 2b above).
			if lastMsg := messages[len(messages)-1]; lastMsg.Role == "user" {
				if nameMap := tools.ExtractMediaNameMap(lastMsg.Content); len(nameMap) > 0 {
					ctx = tools.WithRunMediaNames(ctx, nameMap)
				}
			}
		}
	}

	return ctx, messages, mediaRefs
}
