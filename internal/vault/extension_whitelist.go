package vault

// extensionDocType maps allowed file extensions (with leading dot, lowercased)
// to their canonical vault docType. Extensions not in this map are rejected
// by isIncludedExtension and skipped by safe_walk / rescan.
//
// docType values: "note" | "media" | "document"
// Other docTypes (context, memory, skill, episodic) are derived from path
// prefix in InferDocType, not from extension.
var extensionDocType = map[string]string{
	// Text / code → note
	".md": "note", ".txt": "note", ".json": "note", ".yaml": "note", ".yml": "note",
	".toml": "note", ".csv": "note", ".go": "note", ".js": "note", ".ts": "note",
	".tsx": "note", ".jsx": "note", ".py": "note", ".rs": "note", ".java": "note",
	".sh": "note", ".sql": "note", ".html": "note", ".css": "note",

	// Image → media
	".png": "media", ".jpg": "media", ".jpeg": "media", ".gif": "media",
	".webp": "media", ".svg": "media", ".bmp": "media",

	// Video → media
	".mp4": "media", ".webm": "media", ".mov": "media", ".avi": "media", ".mkv": "media",

	// Audio → media
	".mp3": "media", ".wav": "media", ".ogg": "media", ".flac": "media",
	".aac": "media", ".m4a": "media",

	// Office documents → document (new docType)
	".pdf": "document", ".docx": "document", ".xlsx": "document", ".pptx": "document",
	".doc": "document", ".xls": "document", ".ppt": "document",
	".odt": "document", ".ods": "document", ".odp": "document", ".rtf": "document",
}

// isIncludedExtension reports whether ext is whitelisted for vault registration
// and returns its canonical docType. ext MUST be lowercased with a leading dot
// (e.g. ".md"). Empty extension → (false, "").
func isIncludedExtension(ext string) (bool, string) {
	if ext == "" {
		return false, ""
	}
	dt, ok := extensionDocType[ext]
	return ok, dt
}
