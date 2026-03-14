package validation

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
)

// Image-related errors
var (
	ErrInvalidImage     = errors.New("invalid or corrupted image file")
	ErrImageTooLarge    = errors.New("image dimensions exceed maximum allowed")
	ErrUnsupportedImage = errors.New("unsupported image format")
)

// ImageInfo holds extracted image metadata
type ImageInfo struct {
	Width     int
	Height    int
	Format    FileType
	ColorType string
	BitDepth  int
}

// ImageValidator validates and extracts info from images
type ImageValidator struct {
	maxWidth  int
	maxHeight int
	logger    *slog.Logger
}

// NewImageValidator creates a new image validator
func NewImageValidator(maxWidth, maxHeight int, logger *slog.Logger) *ImageValidator {
	if maxWidth <= 0 {
		maxWidth = 16384
	}
	if maxHeight <= 0 {
		maxHeight = 16384
	}
	return &ImageValidator{
		maxWidth:  maxWidth,
		maxHeight: maxHeight,
		logger:    logger.With("component", "image-validator"),
	}
}

// ValidateAndExtract validates image and extracts metadata
func (v *ImageValidator) ValidateAndExtract(data []byte) (*ImageInfo, error) {
	if len(data) == 0 {
		return nil, ErrInvalidImage
	}

	fileType := DetectFileType(data)
	if !fileType.IsImage() {
		return nil, ErrUnsupportedImage
	}

	var info *ImageInfo
	var err error

	switch fileType {
	case TypeJPEG:
		info, err = v.parseJPEG(data)
	case TypePNG:
		info, err = v.parsePNG(data)
	case TypeGIF:
		info, err = v.parseGIF(data)
	case TypeWebP:
		info, err = v.parseWebP(data)
	case TypeBMP:
		info, err = v.parseBMP(data)
	default:
		return nil, ErrUnsupportedImage
	}

	if err != nil {
		return nil, err
	}

	info.Format = fileType

	if info.Width > v.maxWidth || info.Height > v.maxHeight {
		v.logger.Warn("image dimensions exceed maximum",
			"width", info.Width,
			"height", info.Height,
			"max_width", v.maxWidth,
			"max_height", v.maxHeight)
		return info, ErrImageTooLarge
	}

	v.logger.Debug("image validated",
		"format", fileType.String(),
		"width", info.Width,
		"height", info.Height)

	return info, nil
}

func (v *ImageValidator) parseJPEG(data []byte) (*ImageInfo, error) {
	if len(data) < 4 {
		return nil, ErrInvalidImage
	}

	reader := bytes.NewReader(data)
	reader.Seek(2, io.SeekStart)

	for {
		var marker [2]byte
		if _, err := io.ReadFull(reader, marker[:]); err != nil {
			return nil, ErrInvalidImage
		}

		if marker[0] != 0xFF {
			return nil, ErrInvalidImage
		}

		for marker[1] == 0xFF {
			if _, err := reader.Read(marker[1:]); err != nil {
				return nil, ErrInvalidImage
			}
		}

		if (marker[1] >= 0xC0 && marker[1] <= 0xC3) ||
			(marker[1] >= 0xC5 && marker[1] <= 0xC7) ||
			(marker[1] >= 0xC9 && marker[1] <= 0xCB) ||
			(marker[1] >= 0xCD && marker[1] <= 0xCF) {

			var length uint16
			if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
				return nil, ErrInvalidImage
			}

			var precision uint8
			var height, width uint16
			if err := binary.Read(reader, binary.BigEndian, &precision); err != nil {
				return nil, ErrInvalidImage
			}
			if err := binary.Read(reader, binary.BigEndian, &height); err != nil {
				return nil, ErrInvalidImage
			}
			if err := binary.Read(reader, binary.BigEndian, &width); err != nil {
				return nil, ErrInvalidImage
			}

			return &ImageInfo{
				Width:    int(width),
				Height:   int(height),
				BitDepth: int(precision),
			}, nil
		}

		if marker[1] == 0xD9 || marker[1] == 0xDA {
			break
		}

		if marker[1] != 0x00 && marker[1] != 0x01 && (marker[1] < 0xD0 || marker[1] > 0xD7) {
			var length uint16
			if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
				return nil, ErrInvalidImage
			}
			if length >= 2 {
				reader.Seek(int64(length-2), io.SeekCurrent)
			}
		}
	}

	return nil, ErrInvalidImage
}

func (v *ImageValidator) parsePNG(data []byte) (*ImageInfo, error) {
	if len(data) < 24 {
		return nil, ErrInvalidImage
	}

	reader := bytes.NewReader(data)
	reader.Seek(8, io.SeekStart)

	var chunkLen uint32
	if err := binary.Read(reader, binary.BigEndian, &chunkLen); err != nil {
		return nil, ErrInvalidImage
	}

	var chunkType [4]byte
	if _, err := io.ReadFull(reader, chunkType[:]); err != nil {
		return nil, ErrInvalidImage
	}

	if string(chunkType[:]) != "IHDR" {
		return nil, ErrInvalidImage
	}

	var width, height uint32
	if err := binary.Read(reader, binary.BigEndian, &width); err != nil {
		return nil, ErrInvalidImage
	}
	if err := binary.Read(reader, binary.BigEndian, &height); err != nil {
		return nil, ErrInvalidImage
	}

	var bitDepth, colorType uint8
	if err := binary.Read(reader, binary.BigEndian, &bitDepth); err != nil {
		return nil, ErrInvalidImage
	}
	if err := binary.Read(reader, binary.BigEndian, &colorType); err != nil {
		return nil, ErrInvalidImage
	}

	colorTypeStr := map[uint8]string{
		0: "grayscale",
		2: "rgb",
		3: "indexed",
		4: "grayscale+alpha",
		6: "rgba",
	}[colorType]

	return &ImageInfo{
		Width:     int(width),
		Height:    int(height),
		BitDepth:  int(bitDepth),
		ColorType: colorTypeStr,
	}, nil
}

func (v *ImageValidator) parseGIF(data []byte) (*ImageInfo, error) {
	if len(data) < 10 {
		return nil, ErrInvalidImage
	}

	width := int(binary.LittleEndian.Uint16(data[6:8]))
	height := int(binary.LittleEndian.Uint16(data[8:10]))

	return &ImageInfo{
		Width:    width,
		Height:   height,
		BitDepth: 8,
	}, nil
}

func (v *ImageValidator) parseWebP(data []byte) (*ImageInfo, error) {
	if len(data) < 30 {
		return nil, ErrInvalidImage
	}

	reader := bytes.NewReader(data)
	reader.Seek(12, io.SeekStart)

	var chunkType [4]byte
	if _, err := io.ReadFull(reader, chunkType[:]); err != nil {
		return nil, ErrInvalidImage
	}

	switch string(chunkType[:]) {
	case "VP8 ":
		reader.Seek(4, io.SeekCurrent)
		reader.Seek(3, io.SeekCurrent)
		reader.Seek(3, io.SeekCurrent)

		var width, height uint16
		if err := binary.Read(reader, binary.LittleEndian, &width); err != nil {
			return nil, ErrInvalidImage
		}
		if err := binary.Read(reader, binary.LittleEndian, &height); err != nil {
			return nil, ErrInvalidImage
		}

		return &ImageInfo{
			Width:  int(width & 0x3FFF),
			Height: int(height & 0x3FFF),
		}, nil

	case "VP8L":
		reader.Seek(4, io.SeekCurrent)
		reader.Seek(1, io.SeekCurrent)

		var bits uint32
		if err := binary.Read(reader, binary.LittleEndian, &bits); err != nil {
			return nil, ErrInvalidImage
		}

		width := int((bits & 0x3FFF) + 1)
		height := int(((bits >> 14) & 0x3FFF) + 1)

		return &ImageInfo{
			Width:  width,
			Height: height,
		}, nil

	case "VP8X":
		reader.Seek(4, io.SeekCurrent)
		reader.Seek(4, io.SeekCurrent)

		var widthBuf, heightBuf [3]byte
		if _, err := io.ReadFull(reader, widthBuf[:]); err != nil {
			return nil, ErrInvalidImage
		}
		if _, err := io.ReadFull(reader, heightBuf[:]); err != nil {
			return nil, ErrInvalidImage
		}

		width := int(widthBuf[0]) | int(widthBuf[1])<<8 | int(widthBuf[2])<<16 + 1
		height := int(heightBuf[0]) | int(heightBuf[1])<<8 | int(heightBuf[2])<<16 + 1

		return &ImageInfo{
			Width:  width,
			Height: height,
		}, nil
	}

	return nil, ErrInvalidImage
}

func (v *ImageValidator) parseBMP(data []byte) (*ImageInfo, error) {
	if len(data) < 26 {
		return nil, ErrInvalidImage
	}

	width := int(binary.LittleEndian.Uint32(data[18:22]))
	height := int(binary.LittleEndian.Uint32(data[22:26]))

	if height < 0 {
		height = -height
	}

	bitDepth := 24
	if len(data) >= 30 {
		bitDepth = int(binary.LittleEndian.Uint16(data[28:30]))
	}

	return &ImageInfo{
		Width:    width,
		Height:   height,
		BitDepth: bitDepth,
	}, nil
}
