package audio

import "regexp"

// Pre-compiled regexes for performance (called per stream chunk).
var (
	mdFencedCodeRe = regexp.MustCompile("(?s)```[^`]*```")
	mdInlineCodeRe = regexp.MustCompile("`([^`]+)`")
	mdBoldStarRe   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdItalicStarRe = regexp.MustCompile(`\*([^*]+)\*`)
	mdBoldUnderRe  = regexp.MustCompile(`__([^_]+)__`)
	mdItalicUnderRe = regexp.MustCompile(`_([^_]+)_`)
	mdLinkRe       = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	mdHeadingRe    = regexp.MustCompile(`(?m)^#+\s+`)

	ttsTextBlockRe  = regexp.MustCompile(`(?s)\[\[tts:text\]\](.*?)\[\[/tts:text\]\]`)
	ttsVoiceBlockRe = regexp.MustCompile(`(?s)\[\[tts\]\].*?\[\[/tts\]\]`)
	ttsBareTagRe    = regexp.MustCompile(`\[\[/?tts(?::[^\]]*)?\]\]`)
)

// stripMarkdown removes common markdown formatting so TTS reads prose, not
// syntax characters. Preserves inner text of bold/italic/inline code/links.
func stripMarkdown(text string) string {
	text = mdFencedCodeRe.ReplaceAllString(text, "")
	text = mdInlineCodeRe.ReplaceAllString(text, "$1")
	text = mdBoldStarRe.ReplaceAllString(text, "$1")
	text = mdItalicStarRe.ReplaceAllString(text, "$1")
	text = mdBoldUnderRe.ReplaceAllString(text, "$1")
	text = mdItalicUnderRe.ReplaceAllString(text, "$1")
	text = mdLinkRe.ReplaceAllString(text, "$1")
	text = mdHeadingRe.ReplaceAllString(text, "")
	return text
}

// StripTTSDirectives removes [[tts...]] markup from text.
// `[[tts:text]]...[[/tts:text]]` blocks keep their inner content (voice + text display).
// `[[tts]]...[[/tts]]` blocks are removed entirely including content (voice only, no text).
// Bare `[[tts:something]]` tags without closing are removed.
// Exported for use by channels TTS auto-apply.
func StripTTSDirectives(text string) string {
	// 1. [[tts:text]]...[[/tts:text]] → keep inner content (transcript mode)
	text = ttsTextBlockRe.ReplaceAllString(text, "$1")
	// 2. [[tts]]...[[/tts]] → remove entirely including content (voice-only mode)
	text = ttsVoiceBlockRe.ReplaceAllString(text, "")
	// 3. Remove any remaining bare/unclosed tags
	text = ttsBareTagRe.ReplaceAllString(text, "")
	return text
}
