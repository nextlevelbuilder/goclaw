package voice

const (
	ciphertextTOCMinFrames = 40
	ciphertextTOCDistinct  = 32

	lowInformationMinFrames       = 25
	lowInformationSmallFramePct   = 85
	lowInformationAverageFrameMax = 8
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

// likelyLowInformationOpus catches long silence/comfort-noise buffers that are
// valid enough to package as Ogg but not useful speech. Sending these to Scribe
// is expensive and can produce long hallucinated "podcast outro" transcripts.
func likelyLowInformationOpus(frames [][]byte) (bool, int, int) {
	if len(frames) < lowInformationMinFrames {
		return false, 0, 0
	}

	smallFrames := 0
	totalBytes := 0
	for _, frame := range frames {
		totalBytes += len(frame)
		if len(frame) <= 3 {
			smallFrames++
		}
	}

	avgBytes := totalBytes / len(frames)
	if smallFrames*100 >= lowInformationSmallFramePct*len(frames) {
		return true, smallFrames, avgBytes
	}
	return avgBytes <= lowInformationAverageFrameMax, smallFrames, avgBytes
}
