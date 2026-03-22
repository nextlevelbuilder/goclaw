package media

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

// docMaxChars is the max characters to extract from text documents (matching TS: 200K).
const docMaxChars = 200_000

// BuildMediaTags generates content tags for media items (matching TS media placeholder format).
// For audio/voice items that have been transcribed, the transcript is embedded in a <transcript> block.
// Items with FromReply=true are annotated with "(from replied message)" so the LLM can distinguish
// media from the current message vs media from the message being replied to.
func BuildMediaTags(mediaList []MediaInfo) string {
	var tags []string
	for _, m := range mediaList {
		var tag string
		switch m.Type {
		case TypeImage:
			tag = "<media:image>"
		case TypeVideo, TypeAnimation:
			tag = "<media:video>"
		case TypeAudio:
			if m.Transcript != "" {
				tag = fmt.Sprintf("<media:audio>\n<transcript>%s</transcript>", html.EscapeString(m.Transcript))
			} else {
				tag = "<media:audio>"
			}
		case TypeVoice:
			if m.Transcript != "" {
				tag = fmt.Sprintf("<media:voice>\n<transcript>%s</transcript>", html.EscapeString(m.Transcript))
			} else {
				tag = "<media:voice>"
			}
		case TypeDocument:
			if m.FileName != "" {
				tag = fmt.Sprintf("<media:document name=%q>", m.FileName)
			} else {
				tag = "<media:document>"
			}
		}
		if tag != "" {
			if m.FromReply {
				tag += " (from replied message)"
			}
			tags = append(tags, tag)
		}
	}
	return strings.Join(tags, "\n")
}

// textExtensions maps file extensions to MIME types for text files we can extract.
var textExtensions = map[string]string{
	".txt":  "text/plain",
	".md":   "text/markdown",
	".csv":  "text/csv",
	".tsv":  "text/tab-separated-values",
	".json": "application/json",
	".yaml": "text/yaml",
	".yml":  "text/yaml",
	".xml":  "text/xml",
	".log":  "text/plain",
	".ini":  "text/plain",
	".cfg":  "text/plain",
	".env":  "text/plain",
	".sh":   "text/x-shellscript",
	".py":   "text/x-python",
	".go":   "text/x-go",
	".js":   "text/javascript",
	".ts":   "text/typescript",
	".html": "text/html",
	".css":  "text/css",
	".sql":  "text/x-sql",
	".rs":   "text/x-rust",
	".java": "text/x-java",
	".c":    "text/x-c",
	".cpp":  "text/x-c++",
	".h":    "text/x-c",
	".rb":   "text/x-ruby",
	".php":  "text/x-php",
	".toml": "text/x-toml",
}

// ExtractDocumentContent reads a document file and returns its content wrapped in XML tags.
// For text files: extracts content, truncates at docMaxChars, wraps in <file> block.
// For binary files: returns a placeholder hint directing to the read_document tool.
func ExtractDocumentContent(filePath, fileName string) (string, error) {
	if filePath == "" {
		return fmt.Sprintf("[File: %s — download failed]", fileName), nil
	}

	ext := strings.ToLower(filepath.Ext(fileName))
	mime, isText := textExtensions[ext]
	if !isText {
		// Binary files (PDF, DOCX, etc.) are persisted via MediaRef and analyzed
		// by the read_document tool. Return a hint instead of "not supported" placeholder.
		return fmt.Sprintf("[File: %s — use read_document tool to analyze this file]", fileName), nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file %s: %w", fileName, err)
	}

	content := string(data)

	// Truncate if too long
	if len(content) > docMaxChars {
		content = content[:docMaxChars] + "\n... [truncated]"
	}

	// XML escape content to prevent injection
	escaped := html.EscapeString(content)

	return fmt.Sprintf("<file name=%q mime=%q>\n%s\n</file>", fileName, mime, escaped), nil
}
