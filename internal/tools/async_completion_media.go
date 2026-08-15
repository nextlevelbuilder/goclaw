package tools

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

const asyncCompletionMediaKey = "completion_media"

type persistedCompletionMedia struct {
	Path     string `json:"path"`
	MimeType string `json:"mime_type,omitempty"`
	Filename string `json:"filename,omitempty"`
	Caption  string `json:"caption,omitempty"`
}

// completionMediaDescriptors converts runtime host paths into workspace-safe
// logical paths. Outside-workspace media is intentionally omitted.
func completionMediaDescriptors(
	media []bus.MediaFile,
	workspace, logicalPrefix string,
) []persistedCompletionMedia {
	if len(media) == 0 || workspace == "" {
		return nil
	}
	descriptors := make([]persistedCompletionMedia, 0, len(media))
	for _, item := range media {
		rawPath := item.Path
		if !filepath.IsAbs(rawPath) {
			rawPath = filepath.Join(workspace, rawPath)
		}
		absolutePath, err := filepath.Abs(rawPath)
		if err != nil {
			continue
		}
		relativePath, err := filepath.Rel(workspace, absolutePath)
		if err != nil {
			continue
		}
		logicalPath := filepath.ToSlash(relativePath)
		if logicalPrefix != "" {
			logicalPath = strings.TrimSuffix(logicalPrefix, "/") + "/" + logicalPath
		}
		normalized, err := validateArtifactRelativePath(logicalPath)
		if err != nil {
			continue
		}
		descriptors = append(descriptors, persistedCompletionMedia{
			Path:     normalized,
			MimeType: item.MimeType,
			Filename: item.Filename,
			Caption:  item.Caption,
		})
	}
	return descriptors
}

func persistedCompletionMediaPayload(raw any) []persistedCompletionMedia {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var decoded []persistedCompletionMedia
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil
	}
	safe := decoded[:0]
	for _, item := range decoded {
		normalized, err := validateArtifactRelativePath(item.Path)
		if err != nil {
			continue
		}
		item.Path = normalized
		safe = append(safe, item)
	}
	return safe
}
