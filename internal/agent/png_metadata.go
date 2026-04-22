package agent

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
)

// pngSignature is the 8-byte PNG file signature.
var pngSignature = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

// EmbedPNGPrompt rewrites a PNG byte stream to include tEXt metadata chunks
// for "Description" (the generation prompt) and "Software" (goclaw).
//
// The chunks are inserted immediately before the IEND chunk so all image data
// remains valid. If the input is not a PNG (wrong magic bytes), the original
// bytes are returned unchanged without error. An empty prompt is a no-op.
//
// tEXt chunk format (per PNG spec):
//
//	4 bytes  length of data field
//	4 bytes  chunk type "tEXt"
//	N bytes  keyword\0text  (data field)
//	4 bytes  CRC32 of chunk-type + data
func EmbedPNGPrompt(pngBytes []byte, prompt string) ([]byte, error) {
	if len(prompt) == 0 {
		return pngBytes, nil
	}
	// Validate PNG signature.
	if len(pngBytes) < len(pngSignature) || !bytes.Equal(pngBytes[:len(pngSignature)], pngSignature) {
		// Not a PNG — return unchanged.
		return pngBytes, nil
	}

	// Build the tEXt chunks to insert.
	extraChunks := buildTextChunks([]textKV{
		{Key: "Description", Value: prompt},
		{Key: "Software", Value: "goclaw"},
	})

	// Locate the IEND chunk and insert before it.
	iendOffset := findIENDOffset(pngBytes)
	if iendOffset < 0 {
		// Malformed PNG — return unchanged.
		return pngBytes, nil
	}

	result := make([]byte, 0, len(pngBytes)+len(extraChunks))
	result = append(result, pngBytes[:iendOffset]...)
	result = append(result, extraChunks...)
	result = append(result, pngBytes[iendOffset:]...)
	return result, nil
}

// textKV is a keyword/value pair for PNG tEXt chunks.
type textKV struct {
	Key   string
	Value string
}

// buildTextChunks encodes multiple tEXt chunks into raw PNG chunk bytes.
func buildTextChunks(pairs []textKV) []byte {
	var buf bytes.Buffer
	for _, p := range pairs {
		// data = keyword + NUL + text
		data := make([]byte, 0, len(p.Key)+1+len(p.Value))
		data = append(data, []byte(p.Key)...)
		data = append(data, 0x00)
		data = append(data, []byte(p.Value)...)

		chunkType := []byte("tEXt")
		crcInput := append(chunkType, data...)
		checksum := crc32.ChecksumIEEE(crcInput)

		// 4-byte length
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
		buf.Write(lenBuf[:])

		// chunk type
		buf.Write(chunkType)

		// data
		buf.Write(data)

		// CRC32
		var crcBuf [4]byte
		binary.BigEndian.PutUint32(crcBuf[:], checksum)
		buf.Write(crcBuf[:])
	}
	return buf.Bytes()
}

// findIENDOffset returns the byte offset at which the IEND chunk starts.
// Returns -1 if IEND is not found (malformed PNG).
func findIENDOffset(data []byte) int {
	pos := len(pngSignature)
	for pos+12 <= len(data) {
		chunkLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		chunkType := data[pos+4 : pos+8]
		if bytes.Equal(chunkType, []byte("IEND")) {
			return pos
		}
		// Advance: 4 (length) + 4 (type) + chunkLen (data) + 4 (CRC)
		pos += 8 + chunkLen + 4
		if chunkLen < 0 || pos < 0 {
			// Overflow guard.
			break
		}
	}
	return -1
}
