package voice

const (
	ciphertextTOCMinFrames = 40
	ciphertextTOCDistinct  = 32
)

// likelyCiphertextOpus catches the failure mode where inner-DAVE ciphertext
// leaks into the Opus pipeline. Real speech tends to reuse a small set of Opus
// TOC bytes across an utterance; encrypted bytes make the first byte look close
// to uniformly random.
func likelyCiphertextOpus(frames [][]byte) (bool, int) {
	if len(frames) < ciphertextTOCMinFrames {
		return false, 0
	}

	seen := make(map[byte]struct{}, min(len(frames), 256))
	for _, frame := range frames {
		if len(frame) == 0 {
			continue
		}
		seen[frame[0]] = struct{}{}
	}
	distinct := len(seen)
	return distinct > ciphertextTOCDistinct && distinct*2 > len(frames), distinct
}
