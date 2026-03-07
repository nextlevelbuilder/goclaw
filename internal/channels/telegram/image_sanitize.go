package telegram

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"

	"github.com/disintegration/imaging"
)

const (
	// imageMaxSide is the maximum pixels per side before resize (matching TS imageMaxSide: 1200).
	imageMaxSide = 1200

	// imageMaxBytes is the max file size after compression (matching TS: 5MB).
	imageMaxBytes = 5 * 1024 * 1024
)

// jpegQualities is the grid of quality levels to try (matching TS jpegQualities).
var jpegQualities = []int{85, 75, 65, 55, 45, 35}

// sanitizeImage resizes and compresses an image for LLM vision input.
// Returns the path to the sanitized image (JPEG), or the original path if no processing needed.
// Pipeline (port from TS resizeToJpeg + sanitizeImageForVision):
//  1. Decode image (JPEG/PNG)
//  2. Auto-orient via EXIF
//  3. Resize if larger than imageMaxSide
//  4. Encode as JPEG, iterate quality until under imageMaxBytes
func sanitizeImage(inputPath string) (string, error) {
	img, err := imaging.Open(inputPath, imaging.AutoOrientation(true))
	if err != nil {
		return "", fmt.Errorf("open image: %w", err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Resize if either dimension exceeds max
	if w > imageMaxSide || h > imageMaxSide {
		img = imaging.Fit(img, imageMaxSide, imageMaxSide, imaging.Lanczos)
	}

	// Try encoding at decreasing quality until under size limit
	for _, quality := range jpegQualities {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return "", fmt.Errorf("encode jpeg (q=%d): %w", quality, err)
		}

		if buf.Len() <= imageMaxBytes {
			outPath := filepath.Join(os.TempDir(), fmt.Sprintf("goclaw_sanitized_%d.jpg", os.Getpid()))
			if err := os.WriteFile(outPath, buf.Bytes(), 0644); err != nil {
				return "", fmt.Errorf("write sanitized image: %w", err)
			}
			return outPath, nil
		}
	}

	return "", fmt.Errorf("image too large even at lowest quality (dimensions: %dx%d)", w, h)
}

// Ensure standard image decoders are registered.
func init() {
	image.RegisterFormat("jpeg", "\xff\xd8", jpeg.Decode, jpeg.DecodeConfig)
	image.RegisterFormat("png", "\x89PNG", png.Decode, png.DecodeConfig)
}
