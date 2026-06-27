package bitrix24

import (
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

// mediaFilesToInfos converts the Bitrix24 channel's bus.MediaFile slice into
// the shared media.MediaInfo values so the channel can reuse the cross-channel
// media.BuildMediaTags helper. Telegram / Slack / Discord all use the same
// helper — having Bitrix follow suit keeps the <media:*> tag shape uniform
// across channels so the agent loop's enrichInputMedia (which REPLACES tags
// it finds, not inserts new ones) can match and inject persisted media IDs
// + file paths the same way it does for the other channels.
//
// MIME classification follows the inline routing comment in download.go
// ("image / document / audio / video") so the tag the LLM sees matches the
// read_* tool routing the agent loop performs downstream.
func mediaFilesToInfos(files []bus.MediaFile) []media.MediaInfo {
	if len(files) == 0 {
		return nil
	}
	out := make([]media.MediaInfo, 0, len(files))
	for _, f := range files {
		out = append(out, media.MediaInfo{
			Type:        classifyMediaType(f.MimeType),
			FilePath:    f.Path,
			ContentType: f.MimeType,
			FileName:    f.Filename,
		})
	}
	return out
}

// classifyMediaType maps a MIME type prefix to a media.Type* constant.
//
// Unknown MIMEs fall back to TypeDocument because the read_document tool is
// the safest catch-all: its local parser handles PDF / DOCX / archives and
// delegates to a vision-capable provider for anything else. A bogus
// "audio" tag on a non-audio file would mislead the LLM into calling
// read_audio and getting a useless transcript; "document" stays neutral.
//
// Bitrix webhook does not distinguish voice notes from generic audio in
// the MIME (both ship as audio/ogg or audio/mpeg). We classify all of them
// as TypeAudio — TypeVoice's only behavioral difference is the tag name
// (<media:voice> vs <media:audio>) and read_audio handles both identically.
func classifyMediaType(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return media.TypeImage
	case strings.HasPrefix(mime, "video/"):
		return media.TypeVideo
	case strings.HasPrefix(mime, "audio/"):
		return media.TypeAudio
	default:
		return media.TypeDocument
	}
}
