package common

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"log/slog"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // register WebP decoder
)

// CompressImage shrinks oversized images under maxBytes for any Zalo upload
// endpoint that caps payload size (OA /v2.0/oa/upload/image: 1MB jpg/png).
// Bot uploads photos by URL — the URL is typically obtained from the same
// OA upload endpoint, so Bot inherits the cap transitively.
//
// Transparent inputs first try PNG (lossless), then fall back to a
// white-flattened JPEG so a noisy alpha image doesn't fail the send.

var (
	jpegQualityLadder = []int{85, 75, 65, 55, 45, 35}
	maxSideLadder     = []int{1600, 1200, 900, 600}
)

// Bounds the RGBA buffer image.Decode allocates so a small payload with
// huge dimensions can't pin GB of memory.
const maxDecodePixels = 25_000_000

func CompressImage(data []byte, originalMIME string, maxBytes int) ([]byte, string, error) {
	if len(data) <= maxBytes {
		return data, originalMIME, nil
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("zalo: decode image header: %w", err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxDecodePixels {
		return nil, "", fmt.Errorf("zalo: image dimensions %dx%d exceed %d pixel cap",
			cfg.Width, cfg.Height, maxDecodePixels)
	}

	// AutoOrientation applies EXIF rotation so phone photos arrive upright
	// after we strip EXIF on re-encode.
	img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, "", fmt.Errorf("zalo: decode image for compression: %w", err)
	}

	if hasTransparency(img, originalMIME) {
		if out, ok := encodePNGLadder(img, maxBytes); ok {
			slog.Info("zalo.image.compressed",
				"orig_bytes", len(data), "orig_mime", originalMIME,
				"new_bytes", len(out), "out_mime", "image/png", "transparent", true)
			return out, "image/png", nil
		}
		// PNG can't fit — flatten onto white so the message still ships.
		img = flattenOnWhite(img)
	}

	out, side, q, err := encodeJPEGLadder(img, maxBytes)
	if err != nil {
		return nil, "", err
	}
	if out != nil {
		slog.Info("zalo.image.compressed",
			"orig_bytes", len(data), "orig_mime", originalMIME,
			"new_bytes", len(out), "out_mime", "image/jpeg",
			"side", side, "quality", q)
		return out, "image/jpeg", nil
	}
	b := img.Bounds()
	return nil, "", fmt.Errorf("zalo: image cannot fit under %d bytes (%dx%d original %d bytes)",
		maxBytes, b.Dx(), b.Dy(), len(data))
}

func hasTransparency(img image.Image, originalMIME string) bool {
	if originalMIME == "image/jpeg" {
		return false
	}
	switch im := img.(type) {
	case *image.RGBA:
		for i := 3; i < len(im.Pix); i += 4 {
			if im.Pix[i] != 0xff {
				return true
			}
		}
		return false
	case *image.NRGBA:
		for i := 3; i < len(im.Pix); i += 4 {
			if im.Pix[i] != 0xff {
				return true
			}
		}
		return false
	case *image.RGBA64:
		// 16-bit alpha at byte offsets 6..7 of each 8-byte pixel.
		for i := 6; i+1 < len(im.Pix); i += 8 {
			if im.Pix[i] != 0xff || im.Pix[i+1] != 0xff {
				return true
			}
		}
		return false
	case *image.NRGBA64:
		for i := 6; i+1 < len(im.Pix); i += 8 {
			if im.Pix[i] != 0xff || im.Pix[i+1] != 0xff {
				return true
			}
		}
		return false
	case *image.Paletted:
		for _, c := range im.Palette {
			if _, _, _, a := c.RGBA(); a < 0xffff {
				return true
			}
		}
		return false
	}
	switch img.ColorModel() {
	case color.YCbCrModel, color.GrayModel, color.Gray16Model, color.CMYKModel:
		return false
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a < 0xffff {
				return true
			}
		}
	}
	return false
}

// PNG has no quality knob, so only dimensions can shrink the output.
// Returns ok=false when the smallest tried side still overflows.
func encodePNGLadder(img image.Image, maxBytes int) ([]byte, bool) {
	bounds := img.Bounds()
	origW, origH := bounds.Dx(), bounds.Dy()
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	for _, side := range maxSideLadder {
		scaled := img
		if origW > side || origH > side {
			scaled = imaging.Fit(img, side, side, imaging.Lanczos)
		}
		var buf bytes.Buffer
		if err := enc.Encode(&buf, scaled); err != nil {
			continue
		}
		if buf.Len() <= maxBytes {
			return buf.Bytes(), true
		}
	}
	return nil, false
}

// Returns nil bytes with nil error when the ladder is exhausted without
// fitting — so callers can distinguish "didn't fit" from "encode broke".
func encodeJPEGLadder(img image.Image, maxBytes int) ([]byte, int, int, error) {
	bounds := img.Bounds()
	origW, origH := bounds.Dx(), bounds.Dy()
	for _, side := range maxSideLadder {
		scaled := img
		if origW > side || origH > side {
			scaled = imaging.Fit(img, side, side, imaging.Lanczos)
		}
		for _, q := range jpegQualityLadder {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: q}); err != nil {
				return nil, 0, 0, fmt.Errorf("zalo: jpeg encode (side=%d q=%d): %w", side, q, err)
			}
			if buf.Len() <= maxBytes {
				return buf.Bytes(), side, q, nil
			}
		}
	}
	return nil, 0, 0, nil
}

func flattenOnWhite(img image.Image) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(b)
	draw.Draw(out, b, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(out, b, img, b.Min, draw.Over)
	return out
}
