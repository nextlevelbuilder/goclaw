package orchestration

import (
	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// MediaResultToBusFiles converts agent.MediaResult slice to bus.MediaFile slice.
func MediaResultToBusFiles(results []agent.MediaResult) []bus.MediaFile {
	if len(results) == 0 {
		return nil
	}
	files := make([]bus.MediaFile, len(results))
	for i, r := range results {
		files[i] = bus.MediaFile{
			Path:     r.Path,
			MimeType: r.ContentType,
		}
	}
	return files
}

// BusFilesToMediaResult converts bus.MediaFile slice to agent.MediaResult slice.
func BusFilesToMediaResult(files []bus.MediaFile) []agent.MediaResult {
	if len(files) == 0 {
		return nil
	}
	results := make([]agent.MediaResult, len(files))
	for i, f := range files {
		results[i] = agent.MediaResult{
			Path:        f.Path,
			ContentType: f.MimeType,
		}
	}
	return results
}
