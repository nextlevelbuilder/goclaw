package validation

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
)

// AudioExtractor extracts duration from audio files
type AudioExtractor struct {
	logger *slog.Logger
}

// NewAudioExtractor creates a new audio duration extractor
func NewAudioExtractor(logger *slog.Logger) *AudioExtractor {
	return &AudioExtractor{
		logger: logger.With("component", "audio-extractor"),
	}
}

// GetDuration extracts duration from audio file content in seconds.
func (e *AudioExtractor) GetDuration(data []byte, filename string) float64 {
	if len(data) == 0 {
		return 0
	}

	fileType := DetectFileType(data)

	switch fileType {
	case TypeWAV:
		if dur := e.getWAVDuration(data); dur > 0 {
			e.logger.Debug("extracted WAV duration", "filename", filename, "duration", dur)
			return dur
		}
	case TypeMP3:
		if dur := e.getMP3Duration(data); dur > 0 {
			e.logger.Debug("extracted MP3 duration", "filename", filename, "duration", dur)
			return dur
		}
	case TypeOGG:
		if dur := e.getOGGDuration(data); dur > 0 {
			e.logger.Debug("extracted OGG duration", "filename", filename, "duration", dur)
			return dur
		}
	case TypeFLAC:
		if dur := e.getFLACDuration(data); dur > 0 {
			e.logger.Debug("extracted FLAC duration", "filename", filename, "duration", dur)
			return dur
		}
	case TypeAAC:
		e.logger.Debug("AAC detected, using bitrate estimation", "filename", filename)
	case TypeM4A:
		if dur := e.getM4ADuration(data); dur > 0 {
			e.logger.Debug("extracted M4A duration", "filename", filename, "duration", dur)
			return dur
		}
	case TypeAIFF:
		if dur := e.getAIFFDuration(data); dur > 0 {
			e.logger.Debug("extracted AIFF duration", "filename", filename, "duration", dur)
			return dur
		}
	case TypeWebM:
		e.logger.Debug("WebM detected, using bitrate estimation", "filename", filename)
	}

	estimatedDuration := float64(len(data)) / (128 * 1000 / 8)
	e.logger.Debug("estimated audio duration from file size",
		"filename", filename,
		"detected_type", fileType.String(),
		"file_size", len(data),
		"estimated_duration", estimatedDuration)
	return estimatedDuration
}

func (e *AudioExtractor) getWAVDuration(data []byte) float64 {
	if len(data) < 44 {
		return 0
	}

	reader := bytes.NewReader(data)
	reader.Seek(22, io.SeekStart)

	var numChannels uint16
	if err := binary.Read(reader, binary.LittleEndian, &numChannels); err != nil {
		return 0
	}

	var sampleRate uint32
	if err := binary.Read(reader, binary.LittleEndian, &sampleRate); err != nil {
		return 0
	}

	var byteRate uint32
	if err := binary.Read(reader, binary.LittleEndian, &byteRate); err != nil {
		return 0
	}

	reader.Seek(34, io.SeekStart)
	var bitsPerSample uint16
	if err := binary.Read(reader, binary.LittleEndian, &bitsPerSample); err != nil {
		return 0
	}

	dataSize := e.findWAVDataChunkSize(data)
	if dataSize == 0 {
		return 0
	}

	if byteRate > 0 {
		return float64(dataSize) / float64(byteRate)
	}

	if sampleRate > 0 && numChannels > 0 && bitsPerSample > 0 {
		bytesPerSample := float64(bitsPerSample) / 8
		return float64(dataSize) / (float64(sampleRate) * float64(numChannels) * bytesPerSample)
	}

	return 0
}

func (e *AudioExtractor) findWAVDataChunkSize(data []byte) uint32 {
	for i := 12; i < len(data)-8; i++ {
		if string(data[i:i+4]) == "data" {
			reader := bytes.NewReader(data[i+4:])
			var size uint32
			if err := binary.Read(reader, binary.LittleEndian, &size); err != nil {
				return 0
			}
			return size
		}
	}
	return 0
}

func (e *AudioExtractor) getMP3Duration(data []byte) float64 {
	offset := 0
	if len(data) > 10 && string(data[0:3]) == "ID3" {
		tagSize := int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9])
		offset = 10 + tagSize
	}

	if offset >= len(data)-4 {
		return 0
	}

	frameCount := 0
	totalDuration := 0.0

	for offset < len(data)-4 {
		if data[offset] == 0xFF && (data[offset+1]&0xE0) == 0xE0 {
			header := binary.BigEndian.Uint32(data[offset : offset+4])

			version := (header >> 19) & 0x03
			layer := (header >> 17) & 0x03
			bitrateIndex := (header >> 12) & 0x0F
			freqIndex := (header >> 10) & 0x03
			padding := (header >> 9) & 0x01

			sampleRate := getMPEGSampleRate(version, freqIndex)
			if sampleRate == 0 {
				offset++
				continue
			}

			bitrate := getMPEGBitrate(version, layer, int(bitrateIndex))
			if bitrate == 0 {
				offset++
				continue
			}

			samplesPerFrame := getMPEGSamplesPerFrame(version, layer)
			frameDuration := float64(samplesPerFrame) / float64(sampleRate)
			totalDuration += frameDuration

			frameSize := (samplesPerFrame / 8 * bitrate * 1000 / sampleRate) + int(padding)
			if frameSize <= 0 {
				offset++
				continue
			}

			offset += frameSize
			frameCount++
		} else {
			offset++
		}

		if frameCount > 100000 {
			break
		}
	}

	return totalDuration
}

var mpegSampleRates = map[uint32][]int{
	0: {11025, 12000, 8000, 0},
	2: {22050, 24000, 16000, 0},
	3: {44100, 48000, 32000, 0},
}

func getMPEGSampleRate(version, freqIndex uint32) int {
	rates, ok := mpegSampleRates[version]
	if !ok || int(freqIndex) >= len(rates) {
		return 0
	}
	return rates[freqIndex]
}

var mpegBitrates = map[uint32]map[uint32][]int{
	3: {
		3: {0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, 0},
		2: {0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 0},
		1: {0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
	},
	2: {
		3: {0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, 0},
		2: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
		1: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
	},
}

func getMPEGBitrate(version, layer uint32, bitrateIndex int) int {
	ver := version
	if version == 0 {
		ver = 2
	}
	layers, ok := mpegBitrates[ver]
	if !ok {
		return 0
	}
	bitrates, ok := layers[layer]
	if !ok || bitrateIndex >= len(bitrates) {
		return 0
	}
	return bitrates[bitrateIndex]
}

func getMPEGSamplesPerFrame(version, layer uint32) int {
	if layer == 3 {
		return 384
	}
	if version == 3 {
		return 1152
	}
	if layer == 2 {
		return 1152
	}
	return 576
}

func (e *AudioExtractor) getOGGDuration(data []byte) float64 {
	if len(data) < 58 {
		return 0
	}

	var sampleRate uint32
	vorbisID := []byte{0x01, 'v', 'o', 'r', 'b', 'i', 's'}
	for i := 0; i < len(data)-30 && i < 200; i++ {
		if bytes.Equal(data[i:i+7], vorbisID) {
			if i+15 > len(data) {
				break
			}
			sampleRate = binary.LittleEndian.Uint32(data[i+11 : i+15])
			break
		}
	}

	if sampleRate == 0 {
		opusHead := []byte("OpusHead")
		for i := 0; i < len(data)-20 && i < 200; i++ {
			if bytes.Equal(data[i:i+8], opusHead) {
				sampleRate = 48000
				break
			}
		}
	}

	if sampleRate == 0 {
		return 0
	}

	lastGranule := int64(0)
	for i := len(data) - 14; i >= 0; i-- {
		if string(data[i:i+4]) == "OggS" {
			lastGranule = int64(binary.LittleEndian.Uint64(data[i+6 : i+14]))
			if lastGranule > 0 {
				break
			}
		}
	}

	if lastGranule <= 0 {
		return 0
	}

	return float64(lastGranule) / float64(sampleRate)
}

func (e *AudioExtractor) getFLACDuration(data []byte) float64 {
	if len(data) < 42 {
		return 0
	}

	offset := 4
	blockType := data[offset] & 0x7F
	if blockType != 0 {
		return 0
	}
	offset += 4

	si := data[offset:]
	if len(si) < 18 {
		return 0
	}

	sampleRate := uint32(si[10])<<12 | uint32(si[11])<<4 | uint32(si[12]>>4)
	totalSamplesHigh := uint64(si[12]&0x0F) << 32
	totalSamplesLow := uint64(binary.BigEndian.Uint32(si[13:17]))
	totalSamples := totalSamplesHigh | totalSamplesLow

	if sampleRate == 0 || totalSamples == 0 {
		return 0
	}

	return float64(totalSamples) / float64(sampleRate)
}

func (e *AudioExtractor) getM4ADuration(data []byte) float64 {
	mvhd := []byte("mvhd")
	for i := 0; i < len(data)-28; i++ {
		if bytes.Equal(data[i:i+4], mvhd) {
			version := data[i+4]
			if version == 0 && i+24 <= len(data) {
				timescale := binary.BigEndian.Uint32(data[i+16 : i+20])
				duration := binary.BigEndian.Uint32(data[i+20 : i+24])
				if timescale > 0 && duration > 0 {
					return float64(duration) / float64(timescale)
				}
			} else if version == 1 && i+36 <= len(data) {
				timescale := binary.BigEndian.Uint32(data[i+24 : i+28])
				duration := binary.BigEndian.Uint64(data[i+28 : i+36])
				if timescale > 0 && duration > 0 {
					return float64(duration) / float64(timescale)
				}
			}
			break
		}
	}
	return 0
}

func (e *AudioExtractor) getAIFFDuration(data []byte) float64 {
	if len(data) < 54 {
		return 0
	}

	for i := 12; i < len(data)-26; i++ {
		if string(data[i:i+4]) == "COMM" {
			numSampleFrames := binary.BigEndian.Uint32(data[i+10 : i+14])
			sampleRate := parseIEEE80(data[i+16 : i+26])

			if sampleRate > 0 && numSampleFrames > 0 {
				return float64(numSampleFrames) / sampleRate
			}
			break
		}
	}
	return 0
}

func parseIEEE80(data []byte) float64 {
	if len(data) < 10 {
		return 0
	}
	exponent := int(binary.BigEndian.Uint16(data[0:2]))&0x7FFF - 16383
	mantissa := binary.BigEndian.Uint64(data[2:10])

	if exponent < 0 || exponent > 63 {
		return 0
	}

	result := float64(mantissa) / float64(uint64(1)<<63) * float64(uint64(1)<<uint(exponent))
	return result
}
