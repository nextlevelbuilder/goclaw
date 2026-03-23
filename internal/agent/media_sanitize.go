package agent

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"

	"github.com/disintegration/imaging"
)

// Image sanitization constants (moved from telegram/image_sanitize.go).
const (
	// imageMaxSide is the maximum pixels per side before resize.
	imageMaxSide = 1200
	// imageSanitizeMaxBytes is the max file size after compression (5MB, Anthropic API limit).
	imageSanitizeMaxBytes = 5 * 1024 * 1024
)

// jpegQualities is the grid of quality levels to try during sanitization.
var jpegQualities = []int{85, 75, 65, 55, 45, 35}

// Ensure standard image decoders are registered.
func init() {
	image.RegisterFormat("jpeg", "\xff\xd8", jpeg.Decode, jpeg.DecodeConfig)
	image.RegisterFormat("png", "\x89PNG", png.Decode, png.DecodeConfig)
}

// SanitizeImage resizes and compresses an image for LLM vision input.
// Applied uniformly to all channels at the agent loop level.
// Pipeline: decode → auto-orient EXIF → resize if >1200px → JPEG compress until <5MB.
func SanitizeImage(inputPath string) (string, error) {
	img, err := imaging.Open(inputPath, imaging.AutoOrientation(true))
	if err != nil {
		return "", fmt.Errorf("open image: %w", err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	if w > imageMaxSide || h > imageMaxSide {
		img = imaging.Fit(img, imageMaxSide, imageMaxSide, imaging.Lanczos)
	}

	for _, quality := range jpegQualities {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return "", fmt.Errorf("encode jpeg (q=%d): %w", quality, err)
		}
		if buf.Len() <= imageSanitizeMaxBytes {
			f, err := os.CreateTemp("", "goclaw_sanitized_*.jpg")
			if err != nil {
				return "", fmt.Errorf("create temp file: %w", err)
			}
			outPath := f.Name()
			if _, err := f.Write(buf.Bytes()); err != nil {
				f.Close()
				os.Remove(outPath)
				return "", fmt.Errorf("write sanitized image: %w", err)
			}
			f.Close()
			return outPath, nil
		}
	}

	return "", fmt.Errorf("image too large even at lowest quality (dimensions: %dx%d)", w, h)
}
