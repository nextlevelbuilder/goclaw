package validation

import (
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
)

// Security-related errors
var (
	ErrExtensionMismatch = errors.New("file extension does not match content type")
	ErrExecutableBlocked = errors.New("executable files are not allowed")
	ErrFileTooLarge      = errors.New("file exceeds maximum allowed size")
	ErrEmptyFile         = errors.New("file is empty")
	ErrPathTraversal     = errors.New("filename contains path traversal")
	ErrUnsupportedType   = errors.New("file type is not supported")
)

// SecurityConfig holds security validation settings
type SecurityConfig struct {
	MaxFileSize       int64      // Maximum file size in bytes (0 = unlimited)
	MaxAudioSize      int64      // Maximum audio file size
	MaxImageSize      int64      // Maximum image file size
	AllowedTypes      []FileType // Allowed file types (empty = all except executable)
	BlockExecutables  bool       // Block executable files
	ValidateExtension bool       // Validate extension matches content
}

// DefaultSecurityConfig returns sensible defaults
func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		MaxFileSize:       100 * 1024 * 1024, // 100MB
		MaxAudioSize:      50 * 1024 * 1024,  // 50MB for audio
		MaxImageSize:      20 * 1024 * 1024,  // 20MB for images
		AllowedTypes:      nil,               // All types allowed
		BlockExecutables:  true,
		ValidateExtension: true,
	}
}

// SecurityValidator validates files for security concerns
type SecurityValidator struct {
	config *SecurityConfig
	logger *slog.Logger
}

// NewSecurityValidator creates a new security validator
func NewSecurityValidator(config *SecurityConfig, logger *slog.Logger) *SecurityValidator {
	if config == nil {
		config = DefaultSecurityConfig()
	}
	return &SecurityValidator{
		config: config,
		logger: logger.With("component", "security-validator"),
	}
}

// ValidateFile performs comprehensive security validation
func (v *SecurityValidator) ValidateFile(filename string, data []byte) (FileType, error) {
	if len(data) == 0 {
		return TypeUnknown, ErrEmptyFile
	}

	if err := v.validateFilename(filename); err != nil {
		return TypeUnknown, err
	}

	detectedType := DetectFileType(data)

	if v.config.BlockExecutables && detectedType == TypeExecutable {
		v.logger.Warn("blocked executable file", "filename", filename)
		return detectedType, ErrExecutableBlocked
	}

	if v.config.ValidateExtension {
		if err := v.validateExtensionMatch(filename, detectedType); err != nil {
			v.logger.Warn("extension mismatch detected",
				"filename", filename,
				"extension", filepath.Ext(filename),
				"detected_type", detectedType.String())
			return detectedType, err
		}
	}

	if err := v.validateFileSize(data, detectedType); err != nil {
		return detectedType, err
	}

	if len(v.config.AllowedTypes) > 0 {
		allowed := false
		for _, t := range v.config.AllowedTypes {
			if t == detectedType {
				allowed = true
				break
			}
		}
		if !allowed && detectedType != TypeUnknown {
			return detectedType, ErrUnsupportedType
		}
	}

	return detectedType, nil
}

func (v *SecurityValidator) validateFilename(filename string) error {
	if strings.Contains(filename, "..") {
		return ErrPathTraversal
	}
	if strings.Contains(filename, "\x00") {
		return ErrPathTraversal
	}
	return nil
}

func (v *SecurityValidator) validateExtensionMatch(filename string, detectedType FileType) error {
	if detectedType == TypeUnknown {
		return nil
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return nil
	}

	expectedType, hasMapping := ExtensionToType[ext]
	if !hasMapping {
		return nil
	}

	if expectedType != detectedType {
		if detectedType == TypeExecutable {
			return ErrExecutableBlocked
		}
		return ErrExtensionMismatch
	}

	return nil
}

func (v *SecurityValidator) validateFileSize(data []byte, fileType FileType) error {
	size := int64(len(data))

	if fileType.IsAudio() && v.config.MaxAudioSize > 0 && size > v.config.MaxAudioSize {
		return ErrFileTooLarge
	}
	if fileType.IsImage() && v.config.MaxImageSize > 0 && size > v.config.MaxImageSize {
		return ErrFileTooLarge
	}
	if v.config.MaxFileSize > 0 && size > v.config.MaxFileSize {
		return ErrFileTooLarge
	}

	return nil
}

// SanitizeFilename removes dangerous characters from filename
func SanitizeFilename(filename string) string {
	filename = filepath.Base(filename)
	filename = strings.ReplaceAll(filename, "\x00", "")
	dangerous := []string{"..", "/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	for _, d := range dangerous {
		filename = strings.ReplaceAll(filename, d, "_")
	}
	return filename
}
