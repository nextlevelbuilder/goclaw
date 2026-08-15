package minimax

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

func TestMusicProvider_GenerateMusic(t *testing.T) {
	dlBytes := []byte("MP3DATA")

	makeServers := func(t *testing.T, wantBody map[string]any, apiStatusCode int) (*httptest.Server, *httptest.Server) {
		t.Helper()
		// Download server — returns raw audio bytes.
		dlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(dlBytes)
		}))

		apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/music_generation" {
				t.Errorf("unexpected path: %s", r.URL.Path)
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("missing Content-Type: got %q", r.Header.Get("Content-Type"))
			}
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("bad Authorization: got %q", r.Header.Get("Authorization"))
			}

			body, _ := io.ReadAll(r.Body)
			var got map[string]any
			if err := json.Unmarshal(body, &got); err != nil {
				t.Errorf("bad json body: %v", err)
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			for k, want := range wantBody {
				v, ok := got[k]
				if !ok {
					t.Errorf("missing key %q in request body", k)
					continue
				}
				switch wv := want.(type) {
				case int:
					if gv, ok2 := v.(float64); ok2 && int(gv) == wv {
						continue
					}
				case bool:
					if gv, ok2 := v.(bool); ok2 && gv == wv {
						continue
					}
				case string:
					if gv, ok2 := v.(string); ok2 && gv == wv {
						continue
					}
				}
				t.Errorf("key %q: want %v (%T), got %v (%T)", k, want, want, v, v)
			}
			// Verify audio_setting sub-object.
			if as, ok := got["audio_setting"].(map[string]any); ok {
				if sr, ok2 := as["sample_rate"].(float64); !ok2 || int(sr) != 44100 {
					t.Errorf("audio_setting.sample_rate: want 44100, got %v", as["sample_rate"])
				}
				if br, ok2 := as["bitrate"].(float64); !ok2 || int(br) != 256000 {
					t.Errorf("audio_setting.bitrate: want 256000, got %v", as["bitrate"])
				}
				if f, ok2 := as["format"].(string); !ok2 || f != "mp3" {
					t.Errorf("audio_setting.format: want mp3, got %v", as["format"])
				}
			} else {
				t.Errorf("missing audio_setting object")
			}
			if got["output_format"] != "url" {
				t.Errorf("output_format: want url, got %v", got["output_format"])
			}

			if apiStatusCode != http.StatusOK {
				w.WriteHeader(apiStatusCode)
				_, _ = w.Write([]byte(`internal error`))
				return
			}

			resp := map[string]any{
				"data":      map[string]any{"audio": dlSrv.URL + "/"},
				"base_resp": map[string]any{"status_code": 0, "status_msg": "success"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		return apiSrv, dlSrv
	}

	t.Run("vocal_with_lyrics", func(t *testing.T) {
		apiSrv, dlSrv := makeServers(t, map[string]any{
			"model":            "music-2.5+",
			"prompt":           "happy pop song",
			"is_instrumental":  false,
			"lyrics_optimizer": false,
			"output_format":    "url",
		}, http.StatusOK)
		defer apiSrv.Close()
		defer dlSrv.Close()

		p := NewMusicProvider(MusicConfig{APIKey: "test-key", APIBase: apiSrv.URL})
		res, err := p.GenerateMusic(context.Background(), audio.MusicOptions{
			Prompt:       "happy pop song",
			Lyrics:       "la la la",
			Instrumental: false,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(res.Audio) != string(dlBytes) {
			t.Errorf("audio bytes: want %q, got %q", dlBytes, res.Audio)
		}
		if res.Extension != "mp3" {
			t.Errorf("extension: want mp3, got %s", res.Extension)
		}
		if res.MimeType != "audio/mpeg" {
			t.Errorf("mimetype: want audio/mpeg, got %s", res.MimeType)
		}
	})

	t.Run("instrumental_flag", func(t *testing.T) {
		apiSrv, dlSrv := makeServers(t, map[string]any{
			"is_instrumental": true,
		}, http.StatusOK)
		defer apiSrv.Close()
		defer dlSrv.Close()

		p := NewMusicProvider(MusicConfig{APIKey: "test-key", APIBase: apiSrv.URL})
		_, err := p.GenerateMusic(context.Background(), audio.MusicOptions{
			Prompt:       "epic battle",
			Instrumental: true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("no_lyrics_forces_instrumental", func(t *testing.T) {
		apiSrv, dlSrv := makeServers(t, map[string]any{
			"is_instrumental": true,
		}, http.StatusOK)
		defer apiSrv.Close()
		defer dlSrv.Close()

		p := NewMusicProvider(MusicConfig{APIKey: "test-key", APIBase: apiSrv.URL})
		_, err := p.GenerateMusic(context.Background(), audio.MusicOptions{
			Prompt:       "ambient",
			Instrumental: false, // no lyrics → should auto-force instrumental
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("api_error_status_msg", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"data":      nil,
				"base_resp": map[string]any{"status_code": 1002, "status_msg": "rate limited"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer srv.Close()

		p := NewMusicProvider(MusicConfig{APIKey: "test-key", APIBase: srv.URL})
		_, err := p.GenerateMusic(context.Background(), audio.MusicOptions{Prompt: "test"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "rate limited") {
			t.Errorf("error should contain StatusMsg: %v", err)
		}
	})

	t.Run("data_music_fallback", func(t *testing.T) {
		dlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(dlBytes)
		}))
		defer dlSrv.Close()

		apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"data":      map[string]any{"audio": "", "music": dlSrv.URL + "/"},
				"base_resp": map[string]any{"status_code": 0},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer apiSrv.Close()

		p := NewMusicProvider(MusicConfig{APIKey: "test-key", APIBase: apiSrv.URL})
		res, err := p.GenerateMusic(context.Background(), audio.MusicOptions{Prompt: "test"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(res.Audio) != string(dlBytes) {
			t.Errorf("audio bytes: want %q, got %q", dlBytes, res.Audio)
		}
	})
}
