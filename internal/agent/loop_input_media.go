package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// isTextMime returns true for MIME types representing human-readable text content.
// Covers text/* family plus common application/* types that are text-based.
func isTextMime(mime string) bool {
	if strings.HasPrefix(mime, "text/") {
		return true
	}
	switch mime {
	case "application/json", "application/xml", "application/yaml",
		"application/x-yaml", "application/javascript":
		return true
	}
	return false
}

// collectRefsByKind gathers MediaRefs of a given kind from message history
// in chronological order, then appends current-turn refs. The last ref is the
// newest document for read_document's omitted media_id fallback.
func collectRefsByKind(messages []providers.Message, currentRefs []providers.MediaRef, kind string) []providers.MediaRef {
	var refs []providers.MediaRef
	for i := range messages {
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
		if req.RunKind == "delegate" {
			rehomeDelegatedMediaMessage(messages, req.Media, mediaRefs, tools.ToolWorkspaceFromCtx(ctx))
		}

		// Register persisted text uploads in vault (async, non-blocking).
		if l.onTextUploaded != nil {
			for _, ref := range mediaRefs {
				if ref.Path != "" && isTextMime(ref.MimeType) {
					if content, err := os.ReadFile(ref.Path); err == nil {
						go l.onTextUploaded(context.WithoutCancel(ctx), ref.Path, string(content))
					}
				}
			}
		}

		// Load current-turn images from persisted refs (Path is always set for new uploads).
		var imageFiles []bus.MediaFile
		for _, ref := range mediaRefs {
			if ref.Kind == "image" && ref.Path != "" {
				imageFiles = append(imageFiles, bus.MediaFile{Path: ref.Path, MimeType: ref.MimeType, Filename: filepath.Base(ref.Path)})
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

	// 2b. Collect image MediaRefs (historical + current) for exact media_id
	// resolution by read_image and create_image.
	if imageRefs := collectRefsByKind(messages, mediaRefs, "image"); len(imageRefs) > 0 {
		ctx = tools.WithMediaImageRefs(ctx, imageRefs)
	}

	// 2c. Collect document MediaRefs (historical + current) for read_document tool.
	if docRefs := collectRefsByKind(messages, mediaRefs, "document"); len(docRefs) > 0 {
		ctx = tools.WithMediaDocRefs(ctx, docRefs)
		// Enrich the last user message with exact IDs and logical workspace paths.
		// Only current-turn refs are paired with current-turn tags.
		l.enrichDocumentPaths(messages, mediaRefs, tools.ToolWorkspaceFromCtx(ctx))
	}

	// 2d. Collect audio MediaRefs (historical + current) for read_audio tool.
	if audioRefs := collectRefsByKind(messages, mediaRefs, "audio"); len(audioRefs) > 0 {
		ctx = tools.WithMediaAudioRefs(ctx, audioRefs)
		l.enrichAudioIDs(messages, mediaRefs, tools.ToolWorkspaceFromCtx(ctx))
	}

	// 2e. Collect video MediaRefs (historical + current) for read_video tool.
	if videoRefs := collectRefsByKind(messages, mediaRefs, "video"); len(videoRefs) > 0 {
		ctx = tools.WithMediaVideoRefs(ctx, videoRefs)
		l.enrichVideoIDs(messages, mediaRefs)
	}

	// 2f. Enrich <media:image> tags with persisted media IDs so the LLM
	// knows images were received and stored (consistent with audio/video enrichment).
	l.enrichImageIDs(messages, mediaRefs, tools.ToolWorkspaceFromCtx(ctx))

	// 2f-ii. Enrich ALL user messages' image tags with logical file paths.
	// This upgrades legacy absolute paths and keeps current and historical media
	// references safe regardless of whether images are also attached inline.
	l.enrichImagePaths(messages, tools.ToolWorkspaceFromCtx(ctx))

	// 2g. Collect all media file paths for team workspace auto-collect.
	// When the leader calls team_tasks(create), these paths are copied to the
	// team workspace so members can access attached files.
	if len(mediaRefs) > 0 {
		var mediaPaths []string
		for _, ref := range mediaRefs {
			// Prefer workspace-local path (.uploads/) over canonical .media/ path.
			if ref.Path != "" {
				mediaPaths = append(mediaPaths, ref.Path)
			} else if l.mediaStore != nil {
				p, err := l.mediaStore.LoadPath(ref.ID)
				if err != nil {
					continue
				}
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

// rehomeDelegatedMediaMessage replaces caller-owned attachment paths with the
// delegatee-owned persisted copies and ensures every imported attachment has a
// media tag. The normal enrichment pass then adds the new IDs and local paths.
func rehomeDelegatedMediaMessage(
	messages []providers.Message,
	input []bus.MediaFile,
	refs []providers.MediaRef,
	workspace string,
) {
	if len(messages) == 0 || len(refs) == 0 {
		return
	}
	lastUser := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUser = i
			break
		}
	}
	if lastUser < 0 {
		return
	}

	content := messages[lastUser].Content
	for i := 0; i < len(input) && i < len(refs); i++ {
		if input[i].Path != "" && refs[i].Path != "" {
			replacement := logicalWorkspaceMediaPath(workspace, refs[i].Path)
			if replacement == "" {
				replacement = filepath.Base(refs[i].Path)
			}
			content = strings.ReplaceAll(content, input[i].Path, replacement)
		}
	}

	tagCounts := map[string]int{
		"image":    strings.Count(content, "<media:image"),
		"document": strings.Count(content, "<media:document"),
		"audio":    strings.Count(content, "<media:audio") + strings.Count(content, "<media:voice"),
		"video":    strings.Count(content, "<media:video"),
	}
	for _, ref := range refs {
		tag := ""
		switch ref.Kind {
		case "image":
			tag = "<media:image>"
		case "document":
			tag = "<media:document>"
		case "audio":
			tag = "<media:audio>"
		case "video":
			tag = "<media:video>"
		}
		if tag == "" {
			continue
		}
		if tagCounts[ref.Kind] > 0 {
			tagCounts[ref.Kind]--
			continue
		}
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += tag
	}
	messages[lastUser].Content = content
}
