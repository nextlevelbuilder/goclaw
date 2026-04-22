package audio

import "regexp"

// stripMarkdown removes common markdown formatting so TTS reads prose, not
// syntax characters. Preserves inner text of bold/italic/inline code/links.
func stripMarkdown(text string) string {
	text = regexp.MustCompile("(?s)```[^`]*```").ReplaceAllString(text, "")
	text = regexp.MustCompile("`([^`]+)`").ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`\*\*([^*]+)\*\*`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`\*([^*]+)\*`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`__([^_]+)__`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`_([^_]+)_`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`(?m)^#+\s+`).ReplaceAllString(text, "")
	return text
}

// stripTtsDirectives removes [[tts...]] markup from text.
// `[[tts:text]]...[[/tts:text]]` blocks keep their inner content.
// Bare `[[tts]]` and `[[tts:something]]` tags are removed entirely.
func stripTtsDirectives(text string) string {
	text = regexp.MustCompile(`(?s)\[\[tts:text\]\](.*?)\[\[/tts:text\]\]`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`\[\[tts(?::[^\]]*)?\]\]`).ReplaceAllString(text, "")
	return text
}
