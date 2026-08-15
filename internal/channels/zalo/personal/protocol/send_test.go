package protocol

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"testing"
)

func TestImageDimensions(t *testing.T) {
	tests := []struct {
		name  string
		data  []byte
		wantW int
		wantH int
	}{
		{
			name:  "valid PNG 200x100",
			data:  makePNG(200, 100),
			wantW: 200,
			wantH: 100,
		},
		{
			name:  "valid PNG 1x1",
			data:  makePNG(1, 1),
			wantW: 1,
			wantH: 1,
		},
		{
			name:  "valid JPEG 50x30",
			data:  makeJPEG(50, 30),
			wantW: 50,
			wantH: 30,
		},
		{
			name:  "not an image",
			data:  []byte("hello world"),
			wantW: 0,
			wantH: 0,
		},
		{
			name:  "empty",
			data:  nil,
			wantW: 0,
			wantH: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h := imageDimensions(tt.data)
			if w != tt.wantW || h != tt.wantH {
				t.Errorf("imageDimensions() = (%d, %d), want (%d, %d)", w, h, tt.wantW, tt.wantH)
			}
		})
	}
}

func makePNG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func makeJPEG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255})
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	return buf.Bytes()
}

func TestIsImageFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"photo.jpg", true},
		{"photo.JPG", true},
		{"photo.jpeg", true},
		{"photo.png", true},
		{"photo.webp", true},
		{"photo.PNG", true},
		{"file.md", false},
		{"file.pdf", false},
		{"file.gif", false},
		{"file.mp4", false},
		{"file.txt", false},
		{"noext", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsImageFile(tt.path); got != tt.want {
				t.Errorf("IsImageFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestBuildMultipartBody(t *testing.T) {
	data := []byte("hello world")
	reader, contentType, err := buildMultipartBody("chunkContent", "test.png", data)
	if err != nil {
		t.Fatalf("buildMultipartBody() error: %v", err)
	}

	if contentType == "" {
		t.Fatal("content type is empty")
	}
	if !bytes.Contains([]byte(contentType), []byte("multipart/form-data")) {
		t.Errorf("content type %q does not contain multipart/form-data", contentType)
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read body error: %v", err)
	}

	if !bytes.Contains(body, data) {
		t.Error("body does not contain file data")
	}
	if !bytes.Contains(body, []byte("chunkContent")) {
		t.Error("body does not contain field name")
	}
	if !bytes.Contains(body, []byte("test.png")) {
		t.Error("body does not contain filename")
	}
}
