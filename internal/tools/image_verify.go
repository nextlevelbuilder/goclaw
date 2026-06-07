package tools

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // decoder registration
	_ "image/jpeg" // decoder registration
	_ "image/png"  // decoder registration
	"math/bits"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// imageVerifyDefaultThreshold is the default Hamming-distance threshold for
// the 64-bit aHash comparison used by verifyOutputDiffersFromInputs.
//
// Distance interpretation:
//
//	0–2  → near-identical (model echoed input or did nothing)
//	3–5  → very similar (minor restoration / subtle edit, still suspicious)
//	6–15 → moderate edit (light restyle, recolour)
//	16+  → substantial change (composite, regenerate, heavy edit)
//
// 4 catches the silent-failure case (model returns the input image unchanged
// or with imperceptible deltas) without flagging legitimate light edits like
// scratch removal on an old photo, which usually score 6+ after the model
// rebuilds detail.
const imageVerifyDefaultThreshold = 4

// imageAHash computes a 64-bit average-hash perceptual fingerprint of an image.
//
// Algorithm (classic aHash):
//  1. Decode image bytes (png/jpeg/webp/gif).
//  2. Downsample to 8×8 by point sampling with luminance conversion.
//  3. Compute mean luminance; emit 1 bit per pixel (≥mean → 1, else 0).
//
// Returns (hash, true) on success; (0, false) on decode failure or zero-area
// image. Callers should treat the false case as "skip the check" rather than
// as an error — we cannot verify what we cannot decode.
func imageAHash(data []byte) (uint64, bool) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, false
	}
	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW == 0 || srcH == 0 {
		return 0, false
	}

	const N = 8
	var pixels [N * N]uint8
	var sum uint32
	for y := 0; y < N; y++ {
		for x := 0; x < N; x++ {
			sx := bounds.Min.X + (x*srcW)/N
			sy := bounds.Min.Y + (y*srcH)/N
			r, g, b, _ := img.At(sx, sy).RGBA()
			// RGBA() returns 16-bit channels; shift to 8-bit and use the
			// integer luminance approximation Rec. 601.
			lum := uint8((299*(r>>8) + 587*(g>>8) + 114*(b>>8)) / 1000)
			pixels[y*N+x] = lum
			sum += uint32(lum)
		}
	}

	mean := uint8(sum / (N * N))
	var hash uint64
	for i := 0; i < N*N; i++ {
		if pixels[i] >= mean {
			hash |= 1 << uint(i)
		}
	}
	return hash, true
}

// hammingDistance counts differing bits between two 64-bit hashes.
func hammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// verifyOutputDiffersFromInputs checks that the freshly generated image is
// perceptually distinct from every reference image passed to the model.
//
// Why this matters: gpt-image-2 over the Codex /responses image_generation
// tool sometimes returns the (or one of the) input images unchanged when it
// cannot satisfy the edit instruction — refusal-by-echo rather than an HTTP
// error. Without this check the agent reports success and the customer sees
// their own damaged / unedited image back, with a confident "Đây anh." reply.
//
// Behaviour:
//   - len(inputs) == 0 → returns nil (text-to-image; nothing to compare against).
//   - Any aHash that fails to compute is skipped (cannot prove similarity).
//   - On any input whose hash is within `threshold` bits of the output hash,
//     returns an error that names the offending input index and the distance
//     so the LLM-side error message is actionable.
//
// threshold = 0 disables the check entirely.
func verifyOutputDiffersFromInputs(output []byte, inputs []providers.NativeImageInput, threshold int) error {
	if threshold <= 0 || len(inputs) == 0 {
		return nil
	}
	outHash, ok := imageAHash(output)
	if !ok {
		return nil
	}
	for i, in := range inputs {
		inHash, ok := imageAHash(in.Data)
		if !ok {
			continue
		}
		dist := hammingDistance(outHash, inHash)
		if dist < threshold {
			return fmt.Errorf("image generation appears to have failed: output is only %d/64 bits different from input_images[%d] (threshold %d). The model likely returned the input unchanged instead of applying the edit. Try rephrasing the prompt to be more explicit about the change required, or simplify the edit into smaller steps", dist, i, threshold)
		}
	}
	return nil
}
