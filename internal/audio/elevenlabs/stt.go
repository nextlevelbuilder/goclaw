package elevenlabs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

const (
	sttMaxBytes       = 20 << 20 // 20 MB safety cap (conservative; ElevenLabs allows 3 GB)
	sttDefaultModelID = "scribe_v1"
	sttDefaultTimeout = 60 * time.Second
)

// STTProvider transcribes audio via ElevenLabs Scribe.
// Endpoint: POST /v1/speech-to-text (xi-api-key auth).
type STTProvider struct {
	cfg Config
	c   *client
}

// NewSTTProvider returns an ElevenLabs STT (Scribe) provider.
func NewSTTProvider(cfg Config) *STTProvider {
	cfg = cfg.withDefaults()
	timeoutMs := max(cfg.TimeoutMs, int(sttDefaultTimeout.Milliseconds()))
	return &STTProvider{
		cfg: cfg,
		c:   newClient(cfg.APIKey, cfg.BaseURL, timeoutMs),
	}
}

// Name returns the stable provider identifier.
func (p *STTProvider) Name() string { return "elevenlabs" }

// Transcribe converts audio to text via Scribe. FilePath is preferred over
// Bytes to avoid buffering large files in memory. 20 MB cap enforced before POST.
func (p *STTProvider) Transcribe(ctx context.Context, in audio.STTInput, opts audio.STTOptions) (*audio.TranscriptResult, error) {
	// Resolve file path or temp file from bytes.
	filePath, cleanup, err := resolveFilePath(in)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs stt: resolve input: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Size cap before multipart assembly.
	if err := checkFileSize(filePath, sttMaxBytes); err != nil {
		return nil, fmt.Errorf("elevenlabs stt: %w", err)
	}

	// Build multipart body.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	modelID := opts.ModelID
	if modelID == "" {
		modelID = sttDefaultModelID
	}
	if err := mw.WriteField("model_id", modelID); err != nil {
		return nil, fmt.Errorf("elevenlabs stt: write model_id field: %w", err)
	}

	if opts.Language != "" {
		if err := mw.WriteField("language_code", opts.Language); err != nil {
			return nil, fmt.Errorf("elevenlabs stt: write language_code field: %w", err)
		}
	}

	if opts.Diarize {
		if err := mw.WriteField("diarize", "true"); err != nil {
			return nil, fmt.Errorf("elevenlabs stt: write diarize field: %w", err)
		}
	}

	// Attach file.
	filename := in.Filename
	if filename == "" {
		filename = filepath.Base(filePath)
	}
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs stt: create form file: %w", err)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs stt: open file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(fw, f); err != nil {
		return nil, fmt.Errorf("elevenlabs stt: write file bytes: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("elevenlabs stt: close multipart writer: %w", err)
	}

	// Build HTTP request.
	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/v1/speech-to-text"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs stt: create request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("xi-api-key", p.cfg.APIKey)

	timeout := sttDefaultTimeout
	if opts.TimeoutMs > 0 {
		timeout = time.Duration(opts.TimeoutMs) * time.Millisecond
	}
	hc := &http.Client{Timeout: timeout}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs stt: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("elevenlabs stt: API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Text         string  `json:"text"`
		LanguageCode string  `json:"language_code"`
		Duration     float64 `json:"audio_duration_secs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("elevenlabs stt: parse response: %w", err)
	}

	return &audio.TranscriptResult{
		Text:     result.Text,
		Language: result.LanguageCode,
		Duration: result.Duration,
		Provider: "elevenlabs",
	}, nil
}

// resolveFilePath returns a usable file path. When only Bytes is set, writes a
// temp file (0600) and returns a cleanup func to remove it.
func resolveFilePath(in audio.STTInput) (path string, cleanup func(), err error) {
	if in.FilePath != "" {
		return in.FilePath, nil, nil
	}
	if len(in.Bytes) == 0 {
		return "", nil, fmt.Errorf("neither FilePath nor Bytes provided")
	}
	ext := extFromMime(in.MimeType)
	f, err := os.CreateTemp("", "stt-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("create temp file: %w", err)
	}
	if err := os.Chmod(f.Name(), 0600); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := f.Write(in.Bytes); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, fmt.Errorf("write temp file: %w", err)
	}
	f.Close()
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

// checkFileSize returns an error if the file exceeds maxBytes.
func checkFileSize(path string, maxBytes int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}
	if info.Size() > maxBytes {
		return fmt.Errorf("file too large (%d bytes, max %d)", info.Size(), maxBytes)
	}
	return nil
}

// extFromMime returns a file extension for a MIME type.
func extFromMime(mime string) string {
	switch mime {
	case "audio/ogg", "audio/ogg; codecs=opus":
		return ".ogg"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/wave":
		return ".wav"
	case "audio/mp4", "audio/m4a":
		return ".m4a"
	case "audio/webm":
		return ".webm"
	case "audio/flac":
		return ".flac"
	default:
		return ".bin"
	}
}
