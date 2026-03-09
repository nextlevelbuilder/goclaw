package channels

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DocMaxChars is the max characters to extract from text documents (matching TS: 200K).
	DocMaxChars = 200_000
)

// TextExtensions maps file extensions to MIME types for text files we can extract.
var TextExtensions = map[string]string{
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
// For text files: extracts content, truncates at DocMaxChars, wraps in <file> block.
// For binary files: returns a placeholder message.
func ExtractDocumentContent(filePath, fileName string) (string, error) {
	if filePath == "" {
		return fmt.Sprintf("[File: %s — download failed]", fileName), nil
	}

	ext := strings.ToLower(filepath.Ext(fileName))
	mime, isText := TextExtensions[ext]
	if !isText {
		return fmt.Sprintf("[File: %s — binary format not supported, only text files can be processed]", fileName), nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file %s: %w", fileName, err)
	}

	content := string(data)

	if len(content) > DocMaxChars {
		content = content[:DocMaxChars] + "\n... [truncated]"
	}

	escaped := html.EscapeString(content)

	return fmt.Sprintf("<file name=%q mime=%q>\n%s\n</file>", fileName, mime, escaped), nil
}
