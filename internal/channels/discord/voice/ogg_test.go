package voice

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// opusSilenceFrame is 3.5 bytes (toc + frame-count) of a real Opus silence
// frame — enough to satisfy pion's codecs.OpusPacket parser in oggwriter.
// Generated via `opusenc --bitrate 16 silence.wav silence.opus` and
// extracting the smallest frame.
var opusSilenceFrame = []byte{0xf8, 0xff, 0xfe}

func Test_writeUtteranceOgg_empty_returns_err(t *testing.T) {
	dir := t.TempDir()
	_, err := writeUtteranceOgg(dir, utterance{ssrc: 1})
	if err == nil {
		t.Fatal("expected error for empty utterance, got nil")
	}
	if !strings.Contains(err.Error(), "empty utterance") {
		t.Fatalf("expected 'empty utterance' in err, got: %v", err)
	}
}

func Test_writeUtteranceOgg_mismatched_timestamps_returns_err(t *testing.T) {
	dir := t.TempDir()
	_, err := writeUtteranceOgg(dir, utterance{
		ssrc:         1,
		opusFrames:   [][]byte{opusSilenceFrame, opusSilenceFrame},
		rtpTimestamp: []uint32{100}, // only one timestamp for two frames
	})
	if err == nil {
		t.Fatal("expected error for mismatched lengths, got nil")
	}
	if !strings.Contains(err.Error(), "length mismatch") {
		t.Fatalf("expected 'length mismatch' in err, got: %v", err)
	}
}

func Test_writeUtteranceOgg_happy_creates_file(t *testing.T) {
	dir := t.TempDir()
	frames := [][]byte{opusSilenceFrame, opusSilenceFrame, opusSilenceFrame}
	ts := []uint32{48000, 48960, 49920} // 20ms apart at 48kHz
	path, err := writeUtteranceOgg(dir, utterance{
		ssrc:         42,
		opusFrames:   frames,
		rtpTimestamp: ts,
		startedAt:    time.Now(),
	})
	if err != nil {
		t.Fatalf("writeUtteranceOgg: %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Validate Ogg container: first four bytes MUST be "OggS" per RFC 3533.
	if !bytes.HasPrefix(data, []byte("OggS")) {
		t.Fatalf("written file is not a valid Ogg container (first bytes: %q)", data[:min(8, len(data))])
	}
	// A valid Ogg file with two header pages + data has >1 "OggS" sync pattern.
	if bytes.Count(data, []byte("OggS")) < 2 {
		t.Fatalf("expected >=2 Ogg page headers (OpusHead + OpusTags + data), got %d occurrences",
			bytes.Count(data, []byte("OggS")))
	}
	if !bytes.Contains(data, []byte("OpusHead")) {
		t.Fatal("OpusHead signature missing")
	}
	if !bytes.Contains(data, []byte("OpusTags")) {
		t.Fatal("OpusTags signature missing")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
