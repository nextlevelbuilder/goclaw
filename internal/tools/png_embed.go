package tools

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
)

// pngEmbedPrompt embeds a generation prompt into a PNG byte stream as a tEXt
// "Description" chunk inserted before the IEND chunk.
//
// If data is not a valid PNG (wrong magic bytes or no IEND chunk), the original
// bytes are returned unchanged. An empty prompt is a no-op.
//
// This is the tools-package counterpart of agent.EmbedPNGPrompt. A separate copy
// is required to avoid the tools→agent import cycle.
func pngEmbedPrompt(data []byte, prompt string) ([]byte, error) {
	if len(prompt) == 0 {
		return data, nil
	}

	pngSig := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if len(data) < len(pngSig) || !bytes.Equal(data[:len(pngSig)], pngSig) {
		return data, nil
	}

	iendOffset := pngFindIEND(data)
	if iendOffset < 0 {
		return data, nil
	}

	extra := pngBuildTextChunk("Description", prompt)

	result := make([]byte, 0, len(data)+len(extra))
	result = append(result, data[:iendOffset]...)
	result = append(result, extra...)
	result = append(result, data[iendOffset:]...)
	return result, nil
}

// pngBuildTextChunk encodes a single PNG tEXt chunk for the given keyword/value pair.
func pngBuildTextChunk(keyword, value string) []byte {
	data := make([]byte, 0, len(keyword)+1+len(value))
	data = append(data, []byte(keyword)...)
	data = append(data, 0x00)
	data = append(data, []byte(value)...)

	chunkType := []byte("tEXt")
	crcInput := append(chunkType, data...)
	checksum := crc32.ChecksumIEEE(crcInput)

	var buf bytes.Buffer
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	buf.Write(lenBuf[:])
	buf.Write(chunkType)
	buf.Write(data)
	var crcBuf [4]byte
	binary.BigEndian.PutUint32(crcBuf[:], checksum)
	buf.Write(crcBuf[:])
	return buf.Bytes()
}

// pngFindIEND returns the byte offset of the IEND chunk start, or -1 if not found.
func pngFindIEND(data []byte) int {
	pngSigLen := 8
	pos := pngSigLen
	for pos+12 <= len(data) {
		chunkLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		if chunkLen < 0 {
			break
		}
		chunkType := data[pos+4 : pos+8]
		if bytes.Equal(chunkType, []byte("IEND")) {
			return pos
		}
		next := pos + 8 + chunkLen + 4
		if next <= pos {
			break // overflow guard
		}
		pos = next
	}
	return -1
}
