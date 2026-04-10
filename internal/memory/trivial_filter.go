package memory

import "strings"

// trivialStopwords are common filler words that don't carry search intent.
var trivialStopwords = map[string]bool{
	"hi": true, "hello": true, "hey": true, "ok": true, "okay": true,
	"yes": true, "no": true, "thanks": true, "thank": true, "you": true,
	"sure": true, "right": true, "got": true, "it": true, "the": true,
	"a": true, "an": true, "is": true, "are": true, "was": true, "i": true,
	"me": true, "my": true, "we": true, "do": true, "did": true, "please": true,
	"good": true, "great": true, "nice": true, "hmm": true, "ah": true,
	"oh": true, "um": true, "well": true, "so": true, "and": true,
	"but": true, "or": true, "that": true, "this": true,
}

// isTrivialMessage returns true if the message has fewer than 3 meaningful words.
// Skips memory injection for greetings, acknowledgments, and single-word responses.
func isTrivialMessage(msg string) bool {
	words := strings.Fields(strings.ToLower(msg))
	meaningful := 0
	for _, w := range words {
		w = strings.Trim(w, ".,!?;:'\"()-")
		if len(w) > 0 && !trivialStopwords[w] {
			meaningful++
			if meaningful >= 3 {
				return false
			}
		}
	}
	return true
}
