package voice

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	hallucinationMinCharsForRate = 320
	hallucinationMaxCharsPerSec  = 38.0
	hallucinationMaxWordsPerSec  = 6.5
	hallucinationLongTextChars   = 600
	hallucinationTimestampChars  = 120
)

var transcriptTimestampPattern = regexp.MustCompile(`\b\d{1,2}:\d{2}(?::\d{2})?(?:[,.]\d{3})?\s*(?:-->|--|[-–—])\s*\d{1,2}:\d{2}`)

type transcriptQualityDecision struct {
	Drop        bool
	Reason      string
	Chars       int
	Words       int
	DurationSec float64
	CharsPerSec float64
	WordsPerSec float64
}

func assessTranscriptQuality(text string, u utterance, providerDurationSec float64) transcriptQualityDecision {
	text = strings.TrimSpace(text)
	durationSec := float64(u.durationMs) / 1000
	if durationSec <= 0 {
		durationSec = providerDurationSec
	}
	if durationSec < 0.4 {
		durationSec = 0.4
	}

	chars := utf8.RuneCountInString(text)
	words := len(strings.Fields(text))
	decision := transcriptQualityDecision{
		Chars:       chars,
		Words:       words,
		DurationSec: durationSec,
		CharsPerSec: float64(chars) / durationSec,
		WordsPerSec: float64(words) / durationSec,
	}

	switch {
	case chars >= hallucinationTimestampChars && transcriptTimestampPattern.MatchString(text):
		decision.Drop = true
		decision.Reason = "timestamped_hallucination"
	case chars >= hallucinationLongTextChars && durationSec <= 20:
		decision.Drop = true
		decision.Reason = "implausibly_long_transcript"
	case chars >= hallucinationMinCharsForRate && decision.CharsPerSec > hallucinationMaxCharsPerSec:
		decision.Drop = true
		decision.Reason = "implausible_char_rate"
	case chars >= hallucinationMinCharsForRate && decision.WordsPerSec > hallucinationMaxWordsPerSec:
		decision.Drop = true
		decision.Reason = "implausible_word_rate"
	}

	return decision
}
