package openaicompat_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/openaicompat"
)

// sttForm captures what the fake endpoint received.
type sttForm struct {
	path     string
	auth     string
	filename string
	fileData string
	model    string
	language string
	format   string
}

// newSTTServer returns a fake OpenAI-compatible transcription endpoint that
// records the multipart form and replies with body.
func newSTTServer(t *testing.T, got *sttForm, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		require.NoError(t, r.ParseMultipartForm(32<<20))
		got.model = r.FormValue("model")
		got.language = r.FormValue("language")
		got.format = r.FormValue("response_format")

		file, header, err := r.FormFile("file")
		if err == nil {
			defer file.Close()
			got.filename = header.Filename
			data, err := io.ReadAll(file)
			require.NoError(t, err)
			got.fileData = string(data)
		}

		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(func() { srv.Close() })
	return srv
}

func TestNewSTTProviderRequiresAPIBase(t *testing.T) {
	_, err := openaicompat.NewSTTProvider(openaicompat.Config{})
	require.ErrorIs(t, err, openaicompat.ErrAPIBaseRequired)
}

func TestSTTProviderName(t *testing.T) {
	provider, err := openaicompat.NewSTTProvider(openaicompat.Config{APIBase: "http://x/v1"})
	require.NoError(t, err)
	assert.Equal(t, "openai_compat", provider.Name())
}

func TestSTTTranscribeFromBytes(t *testing.T) {
	var got sttForm
	srv := newSTTServer(t, &got, http.StatusOK, `{"text":"bonjour le monde","language":"fr","duration":1.5}`)

	provider, err := openaicompat.NewSTTProvider(openaicompat.Config{
		APIBase:  srv.URL + "/v1",
		APIKey:   "secret",
		STTModel: "whisper-1",
	})
	require.NoError(t, err)

	result, err := provider.Transcribe(context.Background(),
		audio.STTInput{Bytes: []byte("RIFFfake"), MimeType: "audio/wav", Filename: "utterance.wav"},
		audio.STTOptions{Language: "fr"},
	)
	require.NoError(t, err)

	assert.Equal(t, "/v1/audio/transcriptions", got.path)
	assert.Equal(t, "Bearer secret", got.auth)
	assert.Equal(t, "utterance.wav", got.filename)
	assert.Equal(t, "RIFFfake", got.fileData)
	assert.Equal(t, "whisper-1", got.model)
	assert.Equal(t, "fr", got.language)
	assert.Equal(t, "json", got.format)

	assert.Equal(t, "bonjour le monde", result.Text)
	assert.Equal(t, "fr", result.Language)
	assert.InEpsilon(t, 1.5, result.Duration, 0.0001)
	assert.Equal(t, "openai_compat", result.Provider)
}

func TestSTTTranscribeFromFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "voice-note.ogg")
	require.NoError(t, os.WriteFile(path, []byte("OggS-fake"), 0o600))

	var got sttForm
	srv := newSTTServer(t, &got, http.StatusOK, `{"text":"salut"}`)

	provider, err := openaicompat.NewSTTProvider(openaicompat.Config{APIBase: srv.URL})
	require.NoError(t, err)

	result, err := provider.Transcribe(context.Background(),
		audio.STTInput{FilePath: path, MimeType: "audio/ogg"},
		audio.STTOptions{},
	)
	require.NoError(t, err)

	assert.Equal(t, "voice-note.ogg", got.filename, "filename falls back to the base of FilePath")
	assert.Equal(t, "OggS-fake", got.fileData)
	assert.Equal(t, "salut", result.Text)
}

// TestSTTFilenameIsNeverBlank guards the extension-typing requirement: some
// compatible engines (gpu-manager among them) infer the audio format from the
// filename and reject an upload they cannot type.
func TestSTTFilenameIsNeverBlank(t *testing.T) {
	tests := []struct {
		name     string
		in       audio.STTInput
		wantName string
	}{
		{
			name:     "explicit filename wins",
			in:       audio.STTInput{Bytes: []byte("x"), Filename: "utterance.wav", MimeType: "audio/ogg"},
			wantName: "utterance.wav",
		},
		{
			name:     "derived from wav mime",
			in:       audio.STTInput{Bytes: []byte("x"), MimeType: "audio/wav"},
			wantName: "audio.wav",
		},
		{
			name:     "derived from ogg mime",
			in:       audio.STTInput{Bytes: []byte("x"), MimeType: "audio/ogg"},
			wantName: "audio.ogg",
		},
		{
			name:     "codec parameters stripped",
			in:       audio.STTInput{Bytes: []byte("x"), MimeType: "audio/ogg; codecs=opus"},
			wantName: "audio.ogg",
		},
		{
			name:     "mpeg mime",
			in:       audio.STTInput{Bytes: []byte("x"), MimeType: "audio/mpeg"},
			wantName: "audio.mp3",
		},
		{
			name:     "unknown mime still carries an extension",
			in:       audio.STTInput{Bytes: []byte("x"), MimeType: "audio/weird"},
			wantName: "audio.bin",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got sttForm
			srv := newSTTServer(t, &got, http.StatusOK, `{"text":"ok"}`)

			provider, err := openaicompat.NewSTTProvider(openaicompat.Config{APIBase: srv.URL})
			require.NoError(t, err)

			_, err = provider.Transcribe(context.Background(), tc.in, audio.STTOptions{})
			require.NoError(t, err)
			assert.Equal(t, tc.wantName, got.filename)
		})
	}
}

func TestSTTOmitsModelWhenUnset(t *testing.T) {
	var got sttForm
	srv := newSTTServer(t, &got, http.StatusOK, `{"text":"ok"}`)

	provider, err := openaicompat.NewSTTProvider(openaicompat.Config{APIBase: srv.URL})
	require.NoError(t, err)

	_, err = provider.Transcribe(context.Background(),
		audio.STTInput{Bytes: []byte("x"), MimeType: "audio/wav"}, audio.STTOptions{})
	require.NoError(t, err)
	assert.Empty(t, got.model, "an unset model must be omitted so the endpoint picks its own")
}

func TestSTTOptionsModelOverridesConfig(t *testing.T) {
	var got sttForm
	srv := newSTTServer(t, &got, http.StatusOK, `{"text":"ok"}`)

	provider, err := openaicompat.NewSTTProvider(openaicompat.Config{APIBase: srv.URL, STTModel: "config-model"})
	require.NoError(t, err)

	_, err = provider.Transcribe(context.Background(),
		audio.STTInput{Bytes: []byte("x"), MimeType: "audio/wav"},
		audio.STTOptions{ModelID: "opts-model"})
	require.NoError(t, err)
	assert.Equal(t, "opts-model", got.model)
}

// TestSTTLanguageFallsBackToHint covers a plain (non-verbose) json response,
// which carries no language field.
func TestSTTLanguageFallsBackToHint(t *testing.T) {
	var got sttForm
	srv := newSTTServer(t, &got, http.StatusOK, `{"text":"bonjour"}`)

	provider, err := openaicompat.NewSTTProvider(openaicompat.Config{APIBase: srv.URL})
	require.NoError(t, err)

	result, err := provider.Transcribe(context.Background(),
		audio.STTInput{Bytes: []byte("x"), MimeType: "audio/wav"},
		audio.STTOptions{Language: "fr"})
	require.NoError(t, err)
	assert.Equal(t, "fr", result.Language)
}

func TestSTTErrorStatus(t *testing.T) {
	var got sttForm
	srv := newSTTServer(t, &got, http.StatusUnsupportedMediaType, `{"error":"unsupported audio format"}`)

	provider, err := openaicompat.NewSTTProvider(openaicompat.Config{APIBase: srv.URL})
	require.NoError(t, err)

	_, err = provider.Transcribe(context.Background(),
		audio.STTInput{Bytes: []byte("x"), MimeType: "audio/ogg"}, audio.STTOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "415")
	assert.Contains(t, err.Error(), "unsupported audio format")
}

func TestSTTRejectsEmptyInput(t *testing.T) {
	provider, err := openaicompat.NewSTTProvider(openaicompat.Config{APIBase: "http://x/v1"})
	require.NoError(t, err)

	_, err = provider.Transcribe(context.Background(), audio.STTInput{}, audio.STTOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "neither FilePath nor Bytes")
}

func TestSTTRejectsOversizedBytes(t *testing.T) {
	provider, err := openaicompat.NewSTTProvider(openaicompat.Config{APIBase: "http://x/v1"})
	require.NoError(t, err)

	_, err = provider.Transcribe(context.Background(),
		audio.STTInput{Bytes: make([]byte, (25<<20)+1), MimeType: "audio/wav"},
		audio.STTOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestSTTRejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.wav")
	require.NoError(t, os.WriteFile(path, make([]byte, (25<<20)+1), 0o600))

	provider, err := openaicompat.NewSTTProvider(openaicompat.Config{APIBase: "http://x/v1"})
	require.NoError(t, err)

	_, err = provider.Transcribe(context.Background(),
		audio.STTInput{FilePath: path, MimeType: "audio/wav"}, audio.STTOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}
