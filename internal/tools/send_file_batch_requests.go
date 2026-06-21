package tools

import (
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

type sendFileRequest struct {
	Path    string
	Caption string
}

func parseSendFileRequests(args map[string]any) ([]sendFileRequest, error) {
	if raw, ok := args["attachments"]; ok && raw != nil {
		items, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("attachments must be an array")
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("attachments must include at least one file")
		}
		requests := make([]sendFileRequest, 0, len(items))
		for i, item := range items {
			obj, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("attachments[%d] must be an object", i)
			}
			path := argString(obj, "path")
			if path == "" {
				return nil, fmt.Errorf("attachments[%d].path is required", i)
			}
			requests = append(requests, sendFileRequest{Path: path, Caption: argString(obj, "caption")})
		}
		return requests, nil
	}

	path := argString(args, "path")
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	return []sendFileRequest{{Path: path, Caption: argString(args, "caption")}}, nil
}

func sendFileResultMessage(media []bus.MediaFile) string {
	if len(media) == 1 {
		if media[0].Caption != "" {
			return media[0].Caption
		}
		return fmt.Sprintf("Sent file: %s", media[0].Filename)
	}
	names := make([]string, 0, len(media))
	for _, mf := range media {
		names = append(names, mf.Filename)
	}
	return fmt.Sprintf("Sent %d files: %s", len(media), strings.Join(names, ", "))
}
