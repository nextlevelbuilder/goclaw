// Package gemini implements Google Gemini TTS via generateContent API.
// Gemini returns base64 PCM 16-bit signed little-endian 24kHz mono; this
// package wraps it in a 44-byte RIFF/WAV header so browsers can play it.
package gemini

import "encoding/binary"

// WAV header constants for 24kHz mono 16-bit PCM.
const (
	sampleRate24k  uint32 = 24000
	channelsMono   uint16 = 1
	bitsPerSample  uint16 = 16
	audioFormatPCM uint16 = 1
	fmtChunkSize   uint32 = 16
	wavHeaderSize         = 44
)

// Wrap prepends a RIFF/WAV header to raw PCM bytes and returns playable WAV.
// PCM must be 16-bit signed little-endian at 24kHz mono.
func Wrap(pcm []byte) []byte {
	dataSize := uint32(len(pcm))
	byteRate := uint32(sampleRate24k) * uint32(channelsMono) * uint32(bitsPerSample) / 8
	blockAlign := channelsMono * bitsPerSample / 8

	out := make([]byte, wavHeaderSize+dataSize)
	pos := 0

	// RIFF chunk descriptor
	copy(out[pos:], "RIFF")
	pos += 4
	binary.LittleEndian.PutUint32(out[pos:], 36+dataSize) // chunk size = 36 + data
	pos += 4
	copy(out[pos:], "WAVE")
	pos += 4

	// fmt sub-chunk
	copy(out[pos:], "fmt ")
	pos += 4
	binary.LittleEndian.PutUint32(out[pos:], fmtChunkSize)
	pos += 4
	binary.LittleEndian.PutUint16(out[pos:], audioFormatPCM)
	pos += 2
	binary.LittleEndian.PutUint16(out[pos:], channelsMono)
	pos += 2
	binary.LittleEndian.PutUint32(out[pos:], sampleRate24k)
	pos += 4
	binary.LittleEndian.PutUint32(out[pos:], byteRate)
	pos += 4
	binary.LittleEndian.PutUint16(out[pos:], blockAlign)
	pos += 2
	binary.LittleEndian.PutUint16(out[pos:], bitsPerSample)
	pos += 2

	// data sub-chunk
	copy(out[pos:], "data")
	pos += 4
	binary.LittleEndian.PutUint32(out[pos:], dataSize)
	pos += 4

	copy(out[pos:], pcm)
	return out
}
