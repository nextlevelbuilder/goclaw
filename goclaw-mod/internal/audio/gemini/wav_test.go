package gemini

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestWrap_RIFFHeader_Bytes verifies every field of the 44-byte RIFF/WAV header
// produced by Wrap() for a 1024-byte PCM payload.
func TestWrap_RIFFHeader_Bytes(t *testing.T) {
	pcm := make([]byte, 1024)
	wav := Wrap(pcm)

	if !bytes.Equal(wav[0:4], []byte("RIFF")) {
		t.Errorf("bytes[0:4] = %q, want RIFF", wav[0:4])
	}
	if got := binary.LittleEndian.Uint32(wav[4:8]); got != 36+1024 {
		t.Errorf("chunk size = %d, want %d", got, 36+1024)
	}
	if !bytes.Equal(wav[8:12], []byte("WAVE")) {
		t.Errorf("bytes[8:12] = %q, want WAVE", wav[8:12])
	}
	if !bytes.Equal(wav[12:16], []byte("fmt ")) {
		t.Errorf("bytes[12:16] = %q, want 'fmt '", wav[12:16])
	}
	if got := binary.LittleEndian.Uint32(wav[16:20]); got != 16 {
		t.Errorf("fmt chunk size = %d, want 16", got)
	}
	if got := binary.LittleEndian.Uint16(wav[20:22]); got != 1 {
		t.Errorf("audio format = %d, want 1 (PCM)", got)
	}
	if got := binary.LittleEndian.Uint16(wav[22:24]); got != 1 {
		t.Errorf("channels = %d, want 1 (mono)", got)
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != 24000 {
		t.Errorf("sample rate = %d, want 24000", got)
	}
	if got := binary.LittleEndian.Uint32(wav[28:32]); got != 48000 {
		t.Errorf("byte rate = %d, want 48000", got)
	}
	if got := binary.LittleEndian.Uint16(wav[32:34]); got != 2 {
		t.Errorf("block align = %d, want 2", got)
	}
	if got := binary.LittleEndian.Uint16(wav[34:36]); got != 16 {
		t.Errorf("bits per sample = %d, want 16", got)
	}
	if !bytes.Equal(wav[36:40], []byte("data")) {
		t.Errorf("bytes[36:40] = %q, want 'data'", wav[36:40])
	}
	if got := binary.LittleEndian.Uint32(wav[40:44]); got != 1024 {
		t.Errorf("data chunk size = %d, want 1024", got)
	}
	if !bytes.Equal(wav[44:], pcm) {
		t.Error("PCM payload mismatch after WAV header")
	}
}

// TestWrap_EmptyPCM verifies a 44-byte header + zero data payload.
func TestWrap_EmptyPCM(t *testing.T) {
	wav := Wrap([]byte{})
	if len(wav) != wavHeaderSize {
		t.Errorf("len(wav) = %d, want %d", len(wav), wavHeaderSize)
	}
	if got := binary.LittleEndian.Uint32(wav[40:44]); got != 0 {
		t.Errorf("data chunk size = %d, want 0", got)
	}
}
