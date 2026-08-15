package openaicompat

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

// sttMaxBytes matches the OpenAI upload limit, which compatible engines
// generally adopt. Enforced before the request so an oversized file fails
// locally instead of after a full upload.
const sttMaxBytes = 25 << 20 // 25 MB

// STTProvider transcribes audio via an OpenAI-compatible
// POST /audio/transcriptions.
type STTProvider struct {
	cfg Config
}

// NewSTTProvider returns an STT provider for the configured endpoint.
// Returns ErrAPIBaseRequired when cfg.APIBase is empty.
func NewSTTProvider(cfg Config) (*STTProvider, error) {
	if strings.TrimSpace(cfg.APIBase) == "" {
		return nil, ErrAPIBaseRequired
	}
	return &STTProvider{cfg: cfg.withDefaults()}, nil
}

// Name returns the stable provider identifier.
func (p *STTProvider) Name() string { return providerName }

// Transcribe converts audio to text.
//
// The multipart filename is significant, not cosmetic: several compatible
// engines type the audio by its extension rather than by Content-Type, and
// reject an upload whose name carries no format they recognise. The filename is
// therefore derived from STTInput.Filename, then the base of FilePath, then the
// MIME type — never left blank.
func (p *STTProvider) Transcribe(ctx context.Context, in audio.STTInput, opts audio.STTOptions) (*audio.TranscriptResult, error) {
	body, contentType, err := p.buildForm(in, opts)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(p.cfg.APIBase, "/") + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("openai_compat stt: create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	if p.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	timeout := time.Duration(p.cfg.TimeoutMs) * time.Millisecond
	if opts.TimeoutMs > 0 {
		timeout = time.Duration(opts.TimeoutMs) * time.Millisecond
	}
	hc := &http.Client{Timeout: timeout}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai_compat stt: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyLimit))
		return nil, fmt.Errorf("openai_compat stt: endpoint error %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	// verbose_json is a superset of json; language and duration are absent from
	// a plain json response and stay zero-valued, which the Manager tolerates.
	var result struct {
		Text     string  `json:"text"`
		Language string  `json:"language"`
		Duration float64 `json:"duration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openai_compat stt: parse response: %w", err)
	}

	language := result.Language
	if language == "" {
		language = opts.Language
	}
	return &audio.TranscriptResult{
		Text:     result.Text,
		Language: language,
		Duration: result.Duration,
		Provider: providerName,
	}, nil
}

// buildForm assembles the multipart body, reading the audio from FilePath or
// Bytes. Returns the body and its Content-Type header value.
func (p *STTProvider) buildForm(in audio.STTInput, opts audio.STTOptions) (io.Reader, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	if model := firstNonEmpty(opts.ModelID, p.cfg.STTModel); model != "" {
		if err := mw.WriteField("model", model); err != nil {
			return nil, "", fmt.Errorf("openai_compat stt: write model field: %w", err)
		}
	}
	if opts.Language != "" {
		if err := mw.WriteField("language", opts.Language); err != nil {
			return nil, "", fmt.Errorf("openai_compat stt: write language field: %w", err)
		}
	}
	if err := mw.WriteField("response_format", "json"); err != nil {
		return nil, "", fmt.Errorf("openai_compat stt: write response_format field: %w", err)
	}

	fw, err := mw.CreateFormFile("file", sttFilename(in))
	if err != nil {
		return nil, "", fmt.Errorf("openai_compat stt: create form file: %w", err)
	}
	if err := writeAudio(fw, in); err != nil {
		return nil, "", err
	}
	if err := mw.Close(); err != nil {
		return nil, "", fmt.Errorf("openai_compat stt: close multipart writer: %w", err)
	}
	return &buf, mw.FormDataContentType(), nil
}

// writeAudio copies the input audio into dst, enforcing the size cap.
func writeAudio(dst io.Writer, in audio.STTInput) error {
	if in.FilePath != "" {
		info, err := os.Stat(in.FilePath)
		if err != nil {
			return fmt.Errorf("openai_compat stt: stat file: %w", err)
		}
		if info.Size() > sttMaxBytes {
			return fmt.Errorf("openai_compat stt: file too large (%d bytes, max %d)", info.Size(), sttMaxBytes)
		}
		f, err := os.Open(in.FilePath)
		if err != nil {
			return fmt.Errorf("openai_compat stt: open file: %w", err)
		}
		defer f.Close()
		if _, err := io.Copy(dst, f); err != nil {
			return fmt.Errorf("openai_compat stt: read file: %w", err)
		}
		return nil
	}

	if len(in.Bytes) == 0 {
		return fmt.Errorf("openai_compat stt: neither FilePath nor Bytes provided")
	}
	if len(in.Bytes) > sttMaxBytes {
		return fmt.Errorf("openai_compat stt: audio too large (%d bytes, max %d)", len(in.Bytes), sttMaxBytes)
	}
	if _, err := dst.Write(in.Bytes); err != nil {
		return fmt.Errorf("openai_compat stt: write audio bytes: %w", err)
	}
	return nil
}

// sttFilename derives the multipart filename. See Transcribe for why a blank
// name is not an option.
func sttFilename(in audio.STTInput) string {
	if in.Filename != "" {
		return in.Filename
	}
	if in.FilePath != "" {
		return filepath.Base(in.FilePath)
	}
	return "audio" + extFromMime(in.MimeType)
}

// extFromMime maps a MIME type to a file extension, including the leading dot.
func extFromMime(mime string) string {
	// Strip codec parameters, e.g. "audio/ogg; codecs=opus".
	if idx := strings.IndexByte(mime, ';'); idx >= 0 {
		mime = mime[:idx]
	}
	switch strings.TrimSpace(mime) {
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/wave", "audio/x-wav":
		return ".wav"
	case "audio/pcm", "audio/l16":
		return ".pcm"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return ".m4a"
	case "audio/webm":
		return ".webm"
	case "audio/flac", "audio/x-flac":
		return ".flac"
	default:
		return ".bin"
	}
}
