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
			// Include path so the LLM can pass it directly to tools like
			// create_image(input_images=[...]) without guessing which uploaded
			// file belongs to which message turn. Skip /tmp paths because those
			// are ephemeral channel temp files that get cleaned up before the
			// agent loop persists them to .uploads/ — enrichImagePaths fills in
			// the persisted path later from MediaRefs.
			attrs := []string{}
			if m.FilePath != "" && !strings.HasPrefix(m.FilePath, "/tmp/") {
				attrs = append(attrs, fmt.Sprintf("path=%q", m.FilePath))
			}
			if m.SourceURL != "" {
				attrs = append(attrs, fmt.Sprintf("url=%q", m.SourceURL))
			}
			if len(attrs) > 0 {
				tag = fmt.Sprintf("<media:image %s>", strings.Join(attrs, " "))
			} else {
				tag = "<media:image>"
			}
		case TypeVideo, TypeAnimation:
			if m.FilePath != "" && !strings.HasPrefix(m.FilePath, "/tmp/") {
				tag = fmt.Sprintf("<media:video path=%q>", m.FilePath)
			} else {
				tag = "<media:video>"
			}
		case TypeAudio:
			pathAttr := ""
			if m.FilePath != "" && !strings.HasPrefix(m.FilePath, "/tmp/") {
				pathAttr = fmt.Sprintf(" path=%q", m.FilePath)
			}
			if m.Transcript != "" {
				tag = fmt.Sprintf("<media:audio%s>\n<transcript>%s</transcript>", pathAttr, html.EscapeString(m.Transcript))
			} else {
				tag = fmt.Sprintf("<media:audio%s>", pathAttr)
			}
		case TypeVoice:
			pathAttr := ""
			if m.FilePath != "" && !strings.HasPrefix(m.FilePath, "/tmp/") {
				pathAttr = fmt.Sprintf(" path=%q", m.FilePath)
			}
			if m.Transcript != "" {
				tag = fmt.Sprintf("<media:voice%s>\n<transcript>%s</transcript>", pathAttr, html.EscapeString(m.Transcript))
			} else {
				tag = fmt.Sprintf("<media:voice%s>", pathAttr)
			}
		case TypeDocument:
			attrs := []string{}
			if m.FileName != "" {
				attrs = append(attrs, fmt.Sprintf("name=%q", m.FileName))
			}
			if m.FilePath != "" && !strings.HasPrefix(m.FilePath, "/tmp/") {
				attrs = append(attrs, fmt.Sprintf("path=%q", m.FilePath))
			}
			if len(attrs) > 0 {
				tag = fmt.Sprintf("<media:document %s>", strings.Join(attrs, " "))
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
		if isArchiveFileName(fileName) {
			return fmt.Sprintf("[Archive: %s — use exec with the path from the <media:document> tag to inspect or extract this archive]", fileName), nil
		}
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

func isArchiveFileName(fileName string) bool {
	lower := strings.ToLower(fileName)
	return strings.HasSuffix(lower, ".zip") ||
		strings.HasSuffix(lower, ".tar") ||
		strings.HasSuffix(lower, ".tar.gz") ||
		strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".tar.bz2") ||
		strings.HasSuffix(lower, ".tbz2") ||
		strings.HasSuffix(lower, ".tar.xz") ||
		strings.HasSuffix(lower, ".txz") ||
		strings.HasSuffix(lower, ".gz")
}
