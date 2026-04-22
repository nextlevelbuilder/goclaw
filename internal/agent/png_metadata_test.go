package agent

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// TestEmbedPNGPrompt_RoundTrip embeds a prompt into a real PNG and verifies
// the tEXt chunk can be read back by parsing raw chunk bytes.
func TestEmbedPNGPrompt_RoundTrip(t *testing.T) {
	wantPrompt := "A vibrant sunset over the ocean"

	out, err := EmbedPNGPrompt(minimalPNG, wantPrompt)
	if err != nil {
		t.Fatalf("EmbedPNGPrompt: %v", err)
	}
	if len(out) <= len(minimalPNG) {
		t.Errorf("output (%d bytes) must be larger than input (%d bytes)", len(out), len(minimalPNG))
	}

	// Parse tEXt chunks from the output PNG.
	texts := parsePNGTextChunks(out)

	// "Description" chunk must carry the prompt.
	got, ok := texts["Description"]
	if !ok {
		t.Fatalf("no tEXt 'Description' chunk found; chunks = %v", texts)
	}
	if got != wantPrompt {
		t.Errorf("Description = %q, want %q", got, wantPrompt)
	}

	// "Software" chunk must carry "goclaw".
	if sw := texts["Software"]; sw != "goclaw" {
		t.Errorf("Software = %q, want %q", sw, "goclaw")
	}
}

// TestEmbedPNGPrompt_EmptyPrompt verifies that an empty prompt is a no-op
// (output identical to input).
func TestEmbedPNGPrompt_EmptyPrompt(t *testing.T) {
	out, err := EmbedPNGPrompt(minimalPNG, "")
	if err != nil {
		t.Fatalf("EmbedPNGPrompt with empty prompt: %v", err)
	}
	if !bytes.Equal(out, minimalPNG) {
		t.Error("expected output identical to input for empty prompt")
	}
}

// TestEmbedPNGPrompt_NonPNGPassthrough verifies that non-PNG bytes are returned
// unchanged (no error).
func TestEmbedPNGPrompt_NonPNGPassthrough(t *testing.T) {
	notPNG := []byte("this is not a png file at all")
	out, err := EmbedPNGPrompt(notPNG, "some prompt")
	if err != nil {
		t.Fatalf("EmbedPNGPrompt on non-PNG: %v", err)
	}
	if !bytes.Equal(out, notPNG) {
		t.Error("expected non-PNG bytes returned unchanged")
	}
}

// TestEmbedPNGPrompt_LongPrompt verifies that a prompt longer than 1 KB round-trips
// correctly (tEXt chunks have no length limit).
func TestEmbedPNGPrompt_LongPrompt(t *testing.T) {
	longPrompt := strings.Repeat("detailed landscape with mountains, ", 40)

	out, err := EmbedPNGPrompt(minimalPNG, longPrompt)
	if err != nil {
		t.Fatalf("EmbedPNGPrompt with long prompt: %v", err)
	}
	texts := parsePNGTextChunks(out)
	if got := texts["Description"]; got != longPrompt {
		t.Errorf("long prompt round-trip failed: len(got)=%d len(want)=%d",
			len(got), len(longPrompt))
	}
}

// TestEmbedPNGPrompt_IENDStillLast verifies the structural invariant that
// IEND remains the last chunk in the output PNG after embedding.
func TestEmbedPNGPrompt_IENDStillLast(t *testing.T) {
	out, err := EmbedPNGPrompt(minimalPNG, "test prompt")
	if err != nil {
		t.Fatalf("EmbedPNGPrompt: %v", err)
	}

	// Walk chunks and record the last one we see.
	pos := len(pngSignature)
	lastType := ""
	for pos+12 <= len(out) {
		chunkLen := int(binary.BigEndian.Uint32(out[pos : pos+4]))
		if chunkLen < 0 {
			break
		}
		lastType = string(out[pos+4 : pos+8])
		next := pos + 8 + chunkLen + 4
		if next <= pos {
			break
		}
		pos = next
	}
	if lastType != "IEND" {
		t.Errorf("last chunk type = %q, want IEND", lastType)
	}
}

// parsePNGTextChunks walks a PNG byte stream and extracts all tEXt chunks as
// a map of keyword → text. Used only by tests to verify round-trip correctness.
func parsePNGTextChunks(data []byte) map[string]string {
	result := make(map[string]string)
	pos := len(pngSignature)
	for pos+12 <= len(data) {
		chunkLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		if chunkLen < 0 {
			break
		}
		chunkType := string(data[pos+4 : pos+8])
		chunkData := data[pos+8 : pos+8+chunkLen]
		if chunkType == "tEXt" {
			// tEXt format: keyword\0text
			if nul := bytes.IndexByte(chunkData, 0x00); nul >= 0 {
				keyword := string(chunkData[:nul])
				text := string(chunkData[nul+1:])
				result[keyword] = text
			}
		}
		next := pos + 8 + chunkLen + 4
		if next <= pos {
			break
		}
		pos = next
	}
	return result
}
