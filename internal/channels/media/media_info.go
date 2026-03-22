// Package media provides shared media utilities for all channel implementations.
// Includes media info types, media tag generation, document text extraction,
// STT transcription client, and MIME type detection.
package media

// Media type constants used across all channels.
const (
	TypeImage     = "image"
	TypeVideo     = "video"
	TypeAudio     = "audio"
	TypeVoice     = "voice"
	TypeDocument  = "document"
	TypeAnimation = "animation"
)

// MediaInfo contains information about a downloaded media file.
// Used by all channel implementations (Telegram, Feishu, Discord, etc.).
type MediaInfo struct {
	Type        string // TypeImage, TypeVideo, TypeAudio, TypeVoice, TypeDocument, TypeAnimation
	FilePath    string // local file path after download
	FileID      string // platform-specific file ID (optional)
	ContentType string // MIME type (e.g. "image/jpeg", "audio/ogg")
	FileName    string // original filename
	FileSize    int64
	Transcript  string // STT transcript for audio/voice (empty if not transcribed)
	FromReply   bool   // true if media came from a replied-to/quoted message
}
