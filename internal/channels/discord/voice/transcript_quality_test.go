package voice

import (
	"strings"
	"testing"
)

func Test_assessTranscriptQuality_allows_normal_meeting_snippet(t *testing.T) {
	text := "I have some work that I can do on my side for an SMS optimized onboarding flow."

	decision := assessTranscriptQuality(text, utterance{durationMs: 10_000}, 10)

	if decision.Drop {
		t.Fatalf("normal transcript dropped: %+v", decision)
	}
}

func Test_assessTranscriptQuality_drops_implausibly_dense_transcript(t *testing.T) {
	text := strings.Repeat("thanks for watching this episode and subscribe for more ", 18)

	decision := assessTranscriptQuality(text, utterance{durationMs: 10_000}, 10)

	if !decision.Drop || decision.Reason == "" {
		t.Fatalf("dense transcript not dropped: %+v", decision)
	}
}

func Test_assessTranscriptQuality_drops_timestamp_hallucination(t *testing.T) {
	text := "00:02:39,000 -- 00:03:01,300 " + strings.Repeat("goodbye everyone enjoy yourself ", 6)

	decision := assessTranscriptQuality(text, utterance{durationMs: 10_000}, 10)

	if !decision.Drop || decision.Reason != "timestamped_hallucination" {
		t.Fatalf("timestamp hallucination not dropped: %+v", decision)
	}
}
