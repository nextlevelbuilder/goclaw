package tools

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// makeSolidPNG returns a 32×32 PNG filled with one colour. Used to build
// deterministic perceptual-hash fixtures: two identical images hash the
// same; images with very different luminance hash differently.
func makeSolidPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// makeCheckerPNG returns a 32×32 black-and-white checkerboard PNG.
// Hashes very differently from a solid image (~32 bits flipped).
func makeCheckerPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			if (x/4+y/4)%2 == 0 {
				img.Set(x, y, color.RGBA{0, 0, 0, 255})
			} else {
				img.Set(x, y, color.RGBA{255, 255, 255, 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestImageAHash_IdenticalImagesHashEqually(t *testing.T) {
	pngBytes := makeSolidPNG(t, color.RGBA{128, 128, 128, 255})
	h1, ok1 := imageAHash(pngBytes)
	h2, ok2 := imageAHash(pngBytes)
	if !ok1 || !ok2 {
		t.Fatalf("expected ok=true for both decodes, got %v / %v", ok1, ok2)
	}
	if h1 != h2 {
		t.Errorf("expected identical hashes, got %x vs %x", h1, h2)
	}
}

func TestImageAHash_DifferentImagesHashDifferently(t *testing.T) {
	solid := makeSolidPNG(t, color.RGBA{128, 128, 128, 255})
	checker := makeCheckerPNG(t)
	hs, _ := imageAHash(solid)
	hc, _ := imageAHash(checker)
	dist := hammingDistance(hs, hc)
	if dist < 16 {
		t.Errorf("expected solid vs checker to differ by ≥16 bits, got %d", dist)
	}
}

func TestImageAHash_UndecodableReturnsFalse(t *testing.T) {
	_, ok := imageAHash([]byte("not an image"))
	if ok {
		t.Errorf("expected ok=false for garbage bytes")
	}
}

func TestVerifyOutputDiffers_FailsWhenOutputEqualsInput(t *testing.T) {
	pngBytes := makeSolidPNG(t, color.RGBA{200, 100, 50, 255})
	inputs := []providers.NativeImageInput{{MimeType: "image/png", Data: pngBytes}}

	err := verifyOutputDiffersFromInputs(pngBytes, inputs, imageVerifyDefaultThreshold)
	if err == nil {
		t.Fatal("expected verification to fail when output is identical to input")
	}
	if !strings.Contains(err.Error(), "input_images[0]") {
		t.Errorf("error should name the offending input index, got: %v", err)
	}
}

func TestVerifyOutputDiffers_PassesWhenOutputDiffers(t *testing.T) {
	input := makeSolidPNG(t, color.RGBA{30, 30, 30, 255})
	output := makeCheckerPNG(t)
	inputs := []providers.NativeImageInput{{MimeType: "image/png", Data: input}}

	if err := verifyOutputDiffersFromInputs(output, inputs, imageVerifyDefaultThreshold); err != nil {
		t.Errorf("expected verification to pass when images differ, got: %v", err)
	}
}

func TestVerifyOutputDiffers_PassesWithNoInputs(t *testing.T) {
	output := makeSolidPNG(t, color.RGBA{0, 0, 0, 255})
	if err := verifyOutputDiffersFromInputs(output, nil, imageVerifyDefaultThreshold); err != nil {
		t.Errorf("expected pass for text-to-image (no inputs), got: %v", err)
	}
}

func TestVerifyOutputDiffers_ThresholdZeroDisables(t *testing.T) {
	pngBytes := makeSolidPNG(t, color.RGBA{200, 100, 50, 255})
	inputs := []providers.NativeImageInput{{MimeType: "image/png", Data: pngBytes}}
	if err := verifyOutputDiffersFromInputs(pngBytes, inputs, 0); err != nil {
		t.Errorf("expected threshold=0 to disable the check, got: %v", err)
	}
}
