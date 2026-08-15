package openaicompat_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/openaicompat"
)

// piperVoice is a real Piper voice ID. It is deliberately used throughout:
// it is exactly the kind of self-hosted voice that the "openai" provider name
// would silently rewrite to "alloy".
const piperVoice = "fr_FR-gilles-low"

func TestNewTTSProviderRequiresAPIBase(t *testing.T) {
	_, err := openaicompat.NewTTSProvider(openaicompat.Config{})
	require.ErrorIs(t, err, openaicompat.ErrAPIBaseRequired)

	_, err = openaicompat.NewTTSProvider(openaicompat.Config{APIBase: "   "})
	require.ErrorIs(t, err, openaicompat.ErrAPIBaseRequired, "whitespace-only api_base must not pass")
}

func TestTTSProviderName(t *testing.T) {
	provider, err := openaicompat.NewTTSProvider(openaicompat.Config{APIBase: "http://x/v1"})
	require.NoError(t, err)
	assert.Equal(t, "openai_compat", provider.Name())
}

// TestTTSProviderNameEscapesOpenAIVoiceValidation is the regression guard for
// the reason this package exists. Named "openai", a self-hosted voice ID is
// replaced by "alloy" before the request is ever sent.
func TestTTSProviderNameEscapesOpenAIVoiceValidation(t *testing.T) {
	provider, err := openaicompat.NewTTSProvider(openaicompat.Config{APIBase: "http://x/v1"})
	require.NoError(t, err)

	assert.True(t, audio.IsVoiceCompatible(provider.Name(), piperVoice),
		"a self-hosted voice must survive validation under this provider name")

	filtered, changed := audio.FilterVoiceForProvider(provider.Name(), piperVoice, false)
	assert.Equal(t, piperVoice, filtered)
	assert.False(t, changed)

	// Contrast: the same voice under the "openai" name is silently replaced.
	clobbered, changed := audio.FilterVoiceForProvider("openai", piperVoice, false)
	assert.Equal(t, "alloy", clobbered)
	assert.True(t, changed)
}

func TestTTSSynthesize(t *testing.T) {
	var gotBody map[string]any
	var gotAuth, gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("RIFFfake"))
	}))
	t.Cleanup(func() { srv.Close() })

	provider, err := openaicompat.NewTTSProvider(openaicompat.Config{
		APIBase:   srv.URL + "/v1",
		APIKey:    "secret",
		TTSFormat: "wav",
		TTSVoice:  piperVoice,
	})
	require.NoError(t, err)

	result, err := provider.Synthesize(context.Background(), "bonjour", audio.TTSOptions{})
	require.NoError(t, err)

	assert.Equal(t, "/v1/audio/speech", gotPath)
	assert.Equal(t, "Bearer secret", gotAuth)
	assert.Equal(t, "bonjour", gotBody["input"])
	assert.Equal(t, piperVoice, gotBody["voice"], "configured voice must reach the endpoint verbatim")
	assert.Equal(t, "wav", gotBody["response_format"])
	assert.NotContains(t, gotBody, "model", "an empty model must be omitted, not sent blank")

	assert.Equal(t, []byte("RIFFfake"), result.Audio)
	assert.Equal(t, "wav", result.Extension)
	assert.Equal(t, "audio/wav", result.MimeType)
}

func TestTTSSynthesizeResolutionOrder(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(func() { srv.Close() })

	provider, err := openaicompat.NewTTSProvider(openaicompat.Config{
		APIBase:   srv.URL,
		TTSVoice:  "config-voice",
		TTSModel:  "config-model",
		TTSFormat: "wav",
	})
	require.NoError(t, err)

	_, err = provider.Synthesize(context.Background(), "hi", audio.TTSOptions{
		Voice:  "opts-voice",
		Model:  "opts-model",
		Format: "pcm",
		Params: map[string]any{"speed": 1.5},
	})
	require.NoError(t, err)

	assert.Equal(t, "opts-voice", gotBody["voice"], "per-request opts win over config")
	assert.Equal(t, "opts-model", gotBody["model"])
	assert.Equal(t, "pcm", gotBody["response_format"])
	assert.InEpsilon(t, 1.5, gotBody["speed"], 0.0001)
}

func TestTTSSynthesizeParamsFormatOverridesConfig(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(func() { srv.Close() })

	provider, err := openaicompat.NewTTSProvider(openaicompat.Config{
		APIBase:   srv.URL,
		TTSFormat: "wav",
	})
	require.NoError(t, err)

	_, err = provider.Synthesize(context.Background(), "hi", audio.TTSOptions{
		Params: map[string]any{"response_format": "mp3"},
	})
	require.NoError(t, err)
	assert.Equal(t, "mp3", gotBody["response_format"])
}

func TestTTSSynthesizeOmitsAuthWhenNoKey(t *testing.T) {
	var hasAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasAuth = r.Header["Authorization"]
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(func() { srv.Close() })

	provider, err := openaicompat.NewTTSProvider(openaicompat.Config{APIBase: srv.URL})
	require.NoError(t, err)

	_, err = provider.Synthesize(context.Background(), "hi", audio.TTSOptions{})
	require.NoError(t, err)
	assert.False(t, hasAuth, "self-hosted endpoints without auth must not receive an empty Bearer header")
}

func TestTTSSynthesizeErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"response_format must be wav or pcm"}`)
	}))
	t.Cleanup(func() { srv.Close() })

	provider, err := openaicompat.NewTTSProvider(openaicompat.Config{APIBase: srv.URL})
	require.NoError(t, err)

	_, err = provider.Synthesize(context.Background(), "hi", audio.TTSOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
	assert.Contains(t, err.Error(), "response_format must be wav or pcm",
		"the endpoint's own message must survive so a format mismatch is diagnosable")
}

func TestTTSFormatExtensionMapping(t *testing.T) {
	tests := []struct {
		format   string
		wantExt  string
		wantMIME string
	}{
		{"mp3", "mp3", "audio/mpeg"},
		{"opus", "ogg", "audio/ogg"},
		{"wav", "wav", "audio/wav"},
		{"pcm", "pcm", "audio/pcm"},
		{"flac", "flac", "audio/flac"},
		{"aac", "aac", "audio/aac"},
	}
	for _, tc := range tests {
		t.Run(tc.format, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("x"))
			}))
			t.Cleanup(func() { srv.Close() })

			provider, err := openaicompat.NewTTSProvider(openaicompat.Config{
				APIBase:   srv.URL,
				TTSFormat: tc.format,
			})
			require.NoError(t, err)

			result, err := provider.Synthesize(context.Background(), "hi", audio.TTSOptions{})
			require.NoError(t, err)
			assert.Equal(t, tc.wantExt, result.Extension)
			assert.Equal(t, tc.wantMIME, result.MimeType)
		})
	}
}
