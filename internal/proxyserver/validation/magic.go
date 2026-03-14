package validation

// FileType represents detected file types
type FileType int

const (
	TypeUnknown FileType = iota
	TypeWAV
	TypeMP3
	TypeOGG
	TypeFLAC
	TypeAAC
	TypeM4A
	TypeAIFF
	TypeWebM
	TypeJPEG
	TypePNG
	TypeGIF
	TypeWebP
	TypeBMP
	TypePDF
	TypeZIP
	TypeExecutable
)

// String returns human-readable file type name
func (t FileType) String() string {
	names := map[FileType]string{
		TypeUnknown:    "unknown",
		TypeWAV:        "audio/wav",
		TypeMP3:        "audio/mpeg",
		TypeOGG:        "audio/ogg",
		TypeFLAC:       "audio/flac",
		TypeAAC:        "audio/aac",
		TypeM4A:        "audio/mp4",
		TypeAIFF:       "audio/aiff",
		TypeWebM:       "audio/webm",
		TypeJPEG:       "image/jpeg",
		TypePNG:        "image/png",
		TypeGIF:        "image/gif",
		TypeWebP:       "image/webp",
		TypeBMP:        "image/bmp",
		TypePDF:        "application/pdf",
		TypeZIP:        "application/zip",
		TypeExecutable: "application/x-executable",
	}
	if name, ok := names[t]; ok {
		return name
	}
	return "unknown"
}

// IsAudio returns true if file type is audio
func (t FileType) IsAudio() bool {
	return t == TypeWAV || t == TypeMP3 || t == TypeOGG || t == TypeFLAC ||
		t == TypeAAC || t == TypeM4A || t == TypeAIFF || t == TypeWebM
}

// IsImage returns true if file type is image
func (t FileType) IsImage() bool {
	return t == TypeJPEG || t == TypePNG || t == TypeGIF || t == TypeWebP || t == TypeBMP
}

// MagicSignature defines a magic byte pattern
type MagicSignature struct {
	Offset int
	Bytes  []byte
}

// magicPatterns defines file type signatures
// Order matters: more specific patterns first
var magicPatterns = []struct {
	Type       FileType
	Signatures [][]MagicSignature // OR conditions
}{
	// Audio formats — specific first, then generic
	{TypeWAV, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte("RIFF")}, {Offset: 8, Bytes: []byte("WAVE")}},
	}},
	{TypeAIFF, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte("FORM")}, {Offset: 8, Bytes: []byte("AIFF")}},
		{{Offset: 0, Bytes: []byte("FORM")}, {Offset: 8, Bytes: []byte("AIFC")}},
	}},
	{TypeWebM, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte{0x1A, 0x45, 0xDF, 0xA3}}},
	}},
	{TypeMP3, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte("ID3")}},
		{{Offset: 0, Bytes: []byte{0xFF, 0xFB}}},
		{{Offset: 0, Bytes: []byte{0xFF, 0xFA}}},
		{{Offset: 0, Bytes: []byte{0xFF, 0xF3}}},
		{{Offset: 0, Bytes: []byte{0xFF, 0xF2}}},
	}},
	{TypeOGG, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte("OggS")}},
	}},
	{TypeFLAC, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte("fLaC")}},
	}},
	{TypeM4A, [][]MagicSignature{
		{{Offset: 4, Bytes: []byte("ftypM4A ")}},
		{{Offset: 4, Bytes: []byte("ftypM4B ")}},
		{{Offset: 4, Bytes: []byte("ftypisom")}},
		{{Offset: 4, Bytes: []byte("ftypmp41")}},
		{{Offset: 4, Bytes: []byte("ftypmp42")}},
		{{Offset: 4, Bytes: []byte("ftypMSNV")}},
		{{Offset: 4, Bytes: []byte("ftypdash")}},
		{{Offset: 4, Bytes: []byte("ftyp")}},
	}},
	{TypeAAC, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte{0xFF, 0xF1}}},
		{{Offset: 0, Bytes: []byte{0xFF, 0xF9}}},
	}},

	// Image formats
	{TypeJPEG, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte{0xFF, 0xD8, 0xFF}}},
	}},
	{TypePNG, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}}},
	}},
	{TypeGIF, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte("GIF87a")}},
		{{Offset: 0, Bytes: []byte("GIF89a")}},
	}},
	{TypeWebP, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte("RIFF")}, {Offset: 8, Bytes: []byte("WEBP")}},
	}},
	{TypeBMP, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte("BM")}},
	}},

	// Documents
	{TypePDF, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte("%PDF")}},
	}},
	{TypeZIP, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte{0x50, 0x4B, 0x03, 0x04}}},
	}},

	// Dangerous: Executables
	{TypeExecutable, [][]MagicSignature{
		{{Offset: 0, Bytes: []byte("MZ")}},
		{{Offset: 0, Bytes: []byte{0x7F, 'E', 'L', 'F'}}},
		{{Offset: 0, Bytes: []byte("#!/")}},
		{{Offset: 0, Bytes: []byte("#!")}},
	}},
}

// DetectFileType detects file type from content using magic bytes
func DetectFileType(data []byte) FileType {
	if len(data) == 0 {
		return TypeUnknown
	}

	for _, pattern := range magicPatterns {
		for _, signatures := range pattern.Signatures {
			if matchesAllSignatures(data, signatures) {
				return pattern.Type
			}
		}
	}

	return TypeUnknown
}

// matchesAllSignatures checks if data matches all signatures (AND condition)
func matchesAllSignatures(data []byte, signatures []MagicSignature) bool {
	for _, sig := range signatures {
		if sig.Offset+len(sig.Bytes) > len(data) {
			return false
		}
		for i, b := range sig.Bytes {
			if data[sig.Offset+i] != b {
				return false
			}
		}
	}
	return true
}

// ExtensionToType maps file extensions to expected types
var ExtensionToType = map[string]FileType{
	".wav":  TypeWAV,
	".mp3":  TypeMP3,
	".ogg":  TypeOGG,
	".oga":  TypeOGG,
	".opus": TypeOGG,
	".flac": TypeFLAC,
	".aac":  TypeAAC,
	".m4a":  TypeM4A,
	".mp4":  TypeM4A,
	".m4b":  TypeM4A,
	".aiff": TypeAIFF,
	".aif":  TypeAIFF,
	".aifc": TypeAIFF,
	".webm": TypeWebM,
	".mkv":  TypeWebM,
	".jpg":  TypeJPEG,
	".jpeg": TypeJPEG,
	".png":  TypePNG,
	".gif":  TypeGIF,
	".webp": TypeWebP,
	".bmp":  TypeBMP,
	".pdf":  TypePDF,
	".zip":  TypeZIP,
	".exe":  TypeExecutable,
	".dll":  TypeExecutable,
	".sh":   TypeExecutable,
	".bat":  TypeExecutable,
	".cmd":  TypeExecutable,
}
