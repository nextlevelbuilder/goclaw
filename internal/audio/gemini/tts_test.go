package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// geminiResponseWith builds a minimal Gemini generateContent response
// wrapping the given base64-encoded data string.
func geminiResponseWith(b64data string) []byte {
	resp := map[string]any{
		"candidates": []map[string]any{
			{
				"content": map[string]any{
					"parts": []map[string]any{
						{
							"inlineData": map[string]any{
								"data": b64data,
							},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// capturedRequest records the last request received by the mock server.
type capturedRequest struct {
	path   string
	header http.Header
	body   map[string]any
}

func newMockServer(t *testing.T, status int, respBody []byte) (*httptest.Server, *capturedRequest) {
	t.Helper()
	cap := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.path = r.URL.Path
		cap.header = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&cap.body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(status)
		_, _ = w.Write(respBody)
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

// TestSynthesize_SingleVoice_RequestShape verifies URL, auth header, and JSON body
// for a single-voice synthesis call.
func TestSynthesize_SingleVoice_RequestShape(t *testing.T) {
	pcm := make([]byte, 1024)
	b64 := base64.StdEncoding.EncodeToString(pcm)
	srv, cap := newMockServer(t, http.StatusOK, geminiResponseWith(b64))

	p := NewProvider(Config{
		APIKey:  "test-key",
		APIBase: srv.URL,
		Voice:   "Kore",
		Model:   "gemini-3.1-flash-tts-preview",
	})

	result, err := p.Synthesize(context.Background(), "Hello world", audio.TTSOptions{})
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}

	// URL path
	if !strings.HasSuffix(cap.path, "/v1beta/models/gemini-3.1-flash-tts-preview:generateContent") {
		t.Errorf("path = %q, want suffix .../v1beta/models/gemini-3.1-flash-tts-preview:generateContent", cap.path)
	}

	// Auth header
	if got := cap.header.Get("x-goog-api-key"); got != "test-key" {
		t.Errorf("x-goog-api-key = %q, want test-key", got)
	}

	// generationConfig (now also holds speechConfig — Gemini API requires this nesting)
	gc, _ := cap.body["generationConfig"].(map[string]any)
	mods, _ := gc["responseModalities"].([]any)
	if len(mods) != 1 || mods[0] != "AUDIO" {
		t.Errorf("responseModalities = %v, want [AUDIO]", mods)
	}
	if _, hasRoot := cap.body["speechConfig"]; hasRoot {
		t.Error("speechConfig must be NESTED in generationConfig, not at body root")
	}

	// speechConfig — single voice (nested under generationConfig)
	sc, _ := gc["speechConfig"].(map[string]any)
	vc, _ := sc["voiceConfig"].(map[string]any)
	pvc, _ := vc["prebuiltVoiceConfig"].(map[string]any)
	if got, _ := pvc["voiceName"].(string); got != "Kore" {
		t.Errorf("voiceName = %q, want Kore", got)
	}
	if _, hasMSV := sc["multiSpeakerVoiceConfig"]; hasMSV {
		t.Error("single-voice request must NOT have multiSpeakerVoiceConfig")
	}

	// contents text
	contents, _ := cap.body["contents"].([]any)
	if len(contents) == 0 {
		t.Fatal("contents empty")
	}
	part0 := contents[0].(map[string]any)
	parts, _ := part0["parts"].([]any)
	text, _ := parts[0].(map[string]any)["text"].(string)
	wantText := DefaultTextPrefix + "Hello world"
	if text != wantText {
		t.Errorf("text = %q, want %q", text, wantText)
	}

	// result
	if result.MimeType != "audio/wav" {
		t.Errorf("MimeType = %q, want audio/wav", result.MimeType)
	}
	if len(result.Audio) != wavHeaderSize+1024 {
		t.Errorf("len(result.Audio) = %d, want %d", len(result.Audio), wavHeaderSize+1024)
	}
}

// TestSynthesize_MultiSpeaker_RequestShape verifies multi-speaker body structure.
func TestSynthesize_MultiSpeaker_RequestShape(t *testing.T) {
	pcm := make([]byte, 512)
	b64 := base64.StdEncoding.EncodeToString(pcm)
	srv, cap := newMockServer(t, http.StatusOK, geminiResponseWith(b64))

	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	opts := audio.TTSOptions{
		Speakers: []audio.SpeakerVoice{
			{Speaker: "Joe", VoiceID: "Kore"},
			{Speaker: "Jane", VoiceID: "Puck"},
		},
	}
	if _, err := p.Synthesize(context.Background(), "Joe: Hi\nJane: Hello", opts); err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}

	// Verify transcript passed through unchanged — no inline prefix in multi-speaker mode.
	contents, _ := cap.body["contents"].([]any)
	if len(contents) == 0 {
		t.Fatal("contents empty")
	}
	msparts, _ := contents[0].(map[string]any)["parts"].([]any)
	mstext, _ := msparts[0].(map[string]any)["text"].(string)
	if mstext != "Joe: Hi\nJane: Hello" {
		t.Errorf("multi-speaker text = %q, want %q (no prefix)", mstext, "Joe: Hi\nJane: Hello")
	}

	gc, _ := cap.body["generationConfig"].(map[string]any)
	sc, _ := gc["speechConfig"].(map[string]any)
	if _, hasRoot := cap.body["speechConfig"]; hasRoot {
		t.Error("speechConfig must be NESTED in generationConfig, not at body root")
	}

	// Must have multiSpeakerVoiceConfig, must NOT have voiceConfig at root.
	msv, hasMSV := sc["multiSpeakerVoiceConfig"].(map[string]any)
	if !hasMSV {
		t.Fatal("speechConfig missing multiSpeakerVoiceConfig")
	}
	if _, hasVC := sc["voiceConfig"]; hasVC {
		t.Error("multi-speaker must NOT have voiceConfig at speechConfig root")
	}

	svcs, _ := msv["speakerVoiceConfigs"].([]any)
	if len(svcs) != 2 {
		t.Fatalf("speakerVoiceConfigs len = %d, want 2", len(svcs))
	}
	for i, want := range []struct{ name, voice string }{{"Joe", "Kore"}, {"Jane", "Puck"}} {
		svc := svcs[i].(map[string]any)
		if got, _ := svc["speaker"].(string); got != want.name {
			t.Errorf("[%d] speaker = %q, want %q", i, got, want.name)
		}
		vc := svc["voiceConfig"].(map[string]any)
		pvc := vc["prebuiltVoiceConfig"].(map[string]any)
		if got, _ := pvc["voiceName"].(string); got != want.voice {
			t.Errorf("[%d] voiceName = %q, want %q", i, got, want.voice)
		}
	}
}

// TestSynthesize_DoesNotMutateCallerOpts verifies that Synthesize does not
// modify the caller's opts.Params map or opts.Speakers slice.
func TestSynthesize_DoesNotMutateCallerOpts(t *testing.T) {
	pcm := make([]byte, 64)
	b64 := base64.StdEncoding.EncodeToString(pcm)
	srv, _ := newMockServer(t, http.StatusOK, geminiResponseWith(b64))

	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})

	originalParams := map[string]any{"foo": "bar"}
	originalSpeakers := []audio.SpeakerVoice{{Speaker: "A", VoiceID: "Kore"}}

	opts := audio.TTSOptions{
		Params:   originalParams,
		Speakers: originalSpeakers,
	}

	if _, err := p.Synthesize(context.Background(), "test", opts); err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}

	// Params must be untouched.
	if !reflect.DeepEqual(opts.Params, originalParams) {
		t.Errorf("opts.Params mutated: got %v", opts.Params)
	}
	// Speakers must be untouched.
	if !reflect.DeepEqual(opts.Speakers, originalSpeakers) {
		t.Errorf("opts.Speakers mutated: got %v", opts.Speakers)
	}
}

// TestSynthesize_AuthError verifies 401 produces an auth-related error.
func TestSynthesize_AuthError(t *testing.T) {
	srv, _ := newMockServer(t, http.StatusUnauthorized, []byte(`{"error":"unauthorized"}`))
	p := NewProvider(Config{APIKey: "bad", APIBase: srv.URL})
	_, err := p.Synthesize(context.Background(), "test", audio.TTSOptions{})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "auth") {
		t.Errorf("error %q should contain 'auth'", err.Error())
	}
}

// TestSynthesize_RateLimitError verifies 429 produces an error.
func TestSynthesize_RateLimitError(t *testing.T) {
	srv, _ := newMockServer(t, http.StatusTooManyRequests, []byte(`{"error":"rate limit"}`))
	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	_, err := p.Synthesize(context.Background(), "test", audio.TTSOptions{})
	if err == nil {
		t.Fatal("expected error for 429")
	}
}

// TestSynthesize_MalformedResponse_TextOnly verifies a response with a text part
// (no audio) surfaces the text snippet so the caller can debug.
func TestSynthesize_MalformedResponse_TextOnly(t *testing.T) {
	body := []byte(`{"candidates":[{"content":{"parts":[{"text":"no audio here"}]}}]}`)
	srv, _ := newMockServer(t, http.StatusOK, body)
	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	_, err := p.Synthesize(context.Background(), "test", audio.TTSOptions{})
	if err == nil {
		t.Fatal("expected error for text-only response")
	}
	if !strings.Contains(err.Error(), "text instead of audio") {
		t.Errorf("error %q should mention 'text instead of audio'", err.Error())
	}
	if !strings.Contains(err.Error(), "no audio here") {
		t.Errorf("error %q should include the text snippet", err.Error())
	}
}

// TestSynthesize_PromptBlocked verifies a safety-blocked prompt surfaces the block reason.
func TestSynthesize_PromptBlocked(t *testing.T) {
	body := []byte(`{"promptFeedback":{"blockReason":"SAFETY"}}`)
	srv, _ := newMockServer(t, http.StatusOK, body)
	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	_, err := p.Synthesize(context.Background(), "test", audio.TTSOptions{})
	if err == nil {
		t.Fatal("expected error for blocked prompt")
	}
	if !strings.Contains(err.Error(), "prompt blocked") || !strings.Contains(err.Error(), "SAFETY") {
		t.Errorf("error %q should mention 'prompt blocked (SAFETY)'", err.Error())
	}
}

// TestSynthesize_RetriesOnFinishReasonOTHER verifies the provider retries once
// when Gemini returns finishReason=OTHER (transient flake on the preview TTS
// endpoint), and returns the audio from the second attempt.
func TestSynthesize_RetriesOnFinishReasonOTHER(t *testing.T) {
	pcm := make([]byte, 64)
	b64 := base64.StdEncoding.EncodeToString(pcm)
	successBody := []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"data":"` + b64 + `"}}]}}]}`)
	otherBody := []byte(`{"candidates":[{"finishReason":"OTHER","content":{"parts":[]}}]}`)

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
		if calls == 1 {
			_, _ = w.Write(otherBody)
		} else {
			_, _ = w.Write(successBody)
		}
	}))
	t.Cleanup(srv.Close)

	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	r, err := p.Synthesize(context.Background(), "test", audio.TTSOptions{})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (1 retry), got %d", calls)
	}
	if r.MimeType != "audio/wav" {
		t.Errorf("MimeType = %q, want audio/wav", r.MimeType)
	}
}

// TestSynthesize_RetriesOnceMax verifies the retry happens at most once — two
// consecutive OTHER responses still surface the error to the caller.
func TestSynthesize_RetriesOnceMax(t *testing.T) {
	otherBody := []byte(`{"candidates":[{"finishReason":"OTHER","content":{"parts":[]}}]}`)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(otherBody)
	}))
	t.Cleanup(srv.Close)

	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	_, err := p.Synthesize(context.Background(), "test", audio.TTSOptions{})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls != 2 {
		t.Errorf("expected exactly 2 calls (1 initial + 1 retry), got %d", calls)
	}
	if !strings.Contains(err.Error(), "OTHER") {
		t.Errorf("error %q should mention OTHER finishReason", err.Error())
	}
}

// TestSynthesize_FinishReasonNonStop verifies a non-STOP finishReason without audio
// surfaces the reason (e.g. SAFETY, MAX_TOKENS).
func TestSynthesize_FinishReasonNonStop(t *testing.T) {
	body := []byte(`{"candidates":[{"finishReason":"PROHIBITED_CONTENT","content":{"parts":[]}}]}`)
	srv, _ := newMockServer(t, http.StatusOK, body)
	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	_, err := p.Synthesize(context.Background(), "test", audio.TTSOptions{})
	if err == nil {
		t.Fatal("expected error for non-STOP finishReason")
	}
	if !strings.Contains(err.Error(), "PROHIBITED_CONTENT") {
		t.Errorf("error %q should mention 'PROHIBITED_CONTENT'", err.Error())
	}
}

// TestSynthesize_LeadingTextThenAudio verifies that a response which leads with
// a text part followed by an audio part still extracts the audio successfully.
func TestSynthesize_LeadingTextThenAudio(t *testing.T) {
	pcm := make([]byte, 64)
	b64 := base64.StdEncoding.EncodeToString(pcm)
	body := []byte(`{"candidates":[{"content":{"parts":[{"text":"reading aloud"},{"inlineData":{"data":"` + b64 + `"}}]}}]}`)
	srv, _ := newMockServer(t, http.StatusOK, body)
	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	r, err := p.Synthesize(context.Background(), "test", audio.TTSOptions{})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if r.MimeType != "audio/wav" {
		t.Errorf("MimeType = %q, want audio/wav", r.MimeType)
	}
}

// TestSynthesize_BadBase64 verifies invalid base64 produces a decode error.
func TestSynthesize_BadBase64(t *testing.T) {
	body := geminiResponseWith("!!not-valid-base64!!")
	srv, _ := newMockServer(t, http.StatusOK, body)
	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	_, err := p.Synthesize(context.Background(), "test", audio.TTSOptions{})
	if err == nil {
		t.Fatal("expected base64 decode error")
	}
}

// TestSynthesize_PrependsInlinePrefix verifies the inline style prefix is prepended
// to user text in contents[0].parts[0].text for single-voice synthesis.
func TestSynthesize_PrependsInlinePrefix(t *testing.T) {
	pcm := make([]byte, 64)
	b64 := base64.StdEncoding.EncodeToString(pcm)
	srv, cap := newMockServer(t, http.StatusOK, geminiResponseWith(b64))

	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	if _, err := p.Synthesize(context.Background(), "hello", audio.TTSOptions{}); err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}

	contents, _ := cap.body["contents"].([]any)
	if len(contents) == 0 {
		t.Fatal("contents empty")
	}
	parts, _ := contents[0].(map[string]any)["parts"].([]any)
	text, _ := parts[0].(map[string]any)["text"].(string)

	want := DefaultTextPrefix + "hello"
	if text != want {
		t.Errorf("text = %q, want %q (prefix must be prepended)", text, want)
	}
}

// TestBuildStyledText verifies BuildStyledText pure helper behaviour.
func TestBuildStyledText(t *testing.T) {
	cases := []struct {
		prefix, text, want string
	}{
		{"Say: ", "hi", "Say: hi"},
		{"", "hi", "hi"},
		{"P: ", "", "P: "},
	}
	for _, c := range cases {
		got := BuildStyledText(c.prefix, c.text)
		if got != c.want {
			t.Errorf("BuildStyledText(%q, %q) = %q, want %q", c.prefix, c.text, got, c.want)
		}
	}
}

// TestSynthesize_Returns_ErrTextOnlyResponse_On400 verifies that a 400 with
// text-only phrasing is detected and returned as ErrTextOnlyResponse.
// Both calls return 400 (retry also fails); final error must match sentinel.
func TestSynthesize_Returns_ErrTextOnlyResponse_On400(t *testing.T) {
	body := []byte(`{"error":{"message":"The model returned text when audio was expected","code":400}}`)
	srv, _ := newMockServer(t, http.StatusBadRequest, body)
	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	_, err := p.Synthesize(context.Background(), "x", audio.TTSOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrTextOnlyResponse) {
		t.Errorf("got %v, want ErrTextOnlyResponse", err)
	}
}

// TestSynthesize_Retries_With_StrongerPrefix_On_TextOnly400 verifies that on a
// 400 text-only error the second call uses StrongerTextPrefix and succeeds.
func TestSynthesize_Retries_With_StrongerPrefix_On_TextOnly400(t *testing.T) {
	pcm := make([]byte, 64)
	b64 := base64.StdEncoding.EncodeToString(pcm)
	successBody := geminiResponseWith(b64)
	textOnlyBody := []byte(`{"error":{"message":"returned text instead of audio","code":400}}`)

	var calls int
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		if calls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(textOnlyBody)
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(successBody)
		}
	}))
	t.Cleanup(srv.Close)

	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	_, err := p.Synthesize(context.Background(), "hello", audio.TTSOptions{})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}

	extractText := func(b map[string]any) string {
		contents, _ := b["contents"].([]any)
		if len(contents) == 0 {
			return ""
		}
		parts, _ := contents[0].(map[string]any)["parts"].([]any)
		if len(parts) == 0 {
			return ""
		}
		text, _ := parts[0].(map[string]any)["text"].(string)
		return text
	}

	want1 := DefaultTextPrefix + "hello"
	if got := extractText(bodies[0]); got != want1 {
		t.Errorf("call1 text = %q, want %q", got, want1)
	}
	want2 := StrongerTextPrefix + "hello"
	if got := extractText(bodies[1]); got != want2 {
		t.Errorf("call2 text = %q, want %q", got, want2)
	}
}

// TestIsTextOnlyError is a table-driven unit test for the isTextOnlyError helper.
func TestIsTextOnlyError(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   bool
	}{
		{400, `{"error":{"message":"returned text when audio was expected"}}`, true},
		{400, `{"error":{"message":"The model tried to generate text"}}`, true}, // case-insensitive
		{400, `{"error":{"message":"got text instead of audio"}}`, true},
		{400, `{"error":{"message":"unable to generate text in format"}}`, false}, // bare "generate text" not in list
		{400, `{"error":{"message":"rate limit"}}`, false},
		{400, `{"error":{"message":"invalid voice"}}`, false},
		{400, `not-json`, false}, // no substring match
		{500, `{"error":{"message":"returned text"}}`, false}, // only 400
		{400, ``, false},                                       // empty
		{400, `{"error":{"message":"text-only output detected"}}`, true},
		{400, `{"error":{"message":"text output returned"}}`, true},
	}
	for _, c := range cases {
		got := isTextOnlyError(c.status, []byte(c.body))
		if got != c.want {
			t.Errorf("isTextOnlyError(%d, %q) = %v, want %v", c.status, c.body, got, c.want)
		}
	}
}

// TestSynthesize_Generic400_Unchanged verifies non-text-only 400 errors do not
// match ErrTextOnlyResponse and still surface "unexpected status 400".
func TestSynthesize_Generic400_Unchanged(t *testing.T) {
	body := []byte(`{"error":{"message":"invalid voice name"}}`)
	srv, _ := newMockServer(t, http.StatusBadRequest, body)
	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	_, err := p.Synthesize(context.Background(), "x", audio.TTSOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrTextOnlyResponse) {
		t.Errorf("non-text-only 400 should not match ErrTextOnlyResponse")
	}
	if !strings.Contains(err.Error(), "unexpected status 400") {
		t.Errorf("error %q should contain 'unexpected status 400'", err.Error())
	}
}

// TestSynthesize_RetryRespectsContextCancel verifies that context cancellation
// during the retry backoff aborts without issuing a second request.
func TestSynthesize_RetryRespectsContextCancel(t *testing.T) {
	textOnlyBody := []byte(`{"error":{"message":"returned text when audio was expected","code":400}}`)
	var calls int
	// firstCallDone is closed after the first request handler returns,
	// so the test can cancel ctx immediately after the first call completes.
	firstCallDone := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(textOnlyBody)
		// Signal after first call and cancel immediately so backoff sees ctx.Done().
		if calls == 1 {
			close(firstCallDone)
			cancel()
		}
	}))
	t.Cleanup(srv.Close)

	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	_, err := p.Synthesize(ctx, "x", audio.TTSOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry after cancel), got %d", calls)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestSynthesize_MultiSpeaker_TextOnly_NotRetried verifies that multi-speaker
// mode returns ErrTextOnlyResponse unretried (exactly 1 call, no stronger prefix retry).
func TestSynthesize_MultiSpeaker_TextOnly_NotRetried(t *testing.T) {
	body := []byte(`{"error":{"message":"returned text when audio was expected","code":400}}`)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	opts := audio.TTSOptions{
		Speakers: []audio.SpeakerVoice{
			{Speaker: "Joe", VoiceID: "Kore"},
			{Speaker: "Jane", VoiceID: "Puck"},
		},
	}
	_, err := p.Synthesize(context.Background(), "Joe: Hi\nJane: Hello", opts)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrTextOnlyResponse) {
		t.Errorf("got %v, want ErrTextOnlyResponse", err)
	}
	if calls != 1 {
		t.Errorf("multi-speaker must not retry: expected 1 call, got %d", calls)
	}
}

// TestSynthesize_MultiSpeaker_NoPrefix pins the invariant that multi-speaker
// transcripts pass through unchanged — no inline prefix applied.
func TestSynthesize_MultiSpeaker_NoPrefix(t *testing.T) {
	pcm := make([]byte, 64)
	b64 := base64.StdEncoding.EncodeToString(pcm)
	srv, cap := newMockServer(t, http.StatusOK, geminiResponseWith(b64))

	p := NewProvider(Config{APIKey: "k", APIBase: srv.URL})
	opts := audio.TTSOptions{
		Speakers: []audio.SpeakerVoice{
			{Speaker: "Joe", VoiceID: "Kore"},
			{Speaker: "Jane", VoiceID: "Puck"},
		},
	}
	transcript := "Joe: Hi\nJane: Hello"
	if _, err := p.Synthesize(context.Background(), transcript, opts); err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}

	contents, _ := cap.body["contents"].([]any)
	if len(contents) == 0 {
		t.Fatal("contents empty")
	}
	parts, _ := contents[0].(map[string]any)["parts"].([]any)
	text, _ := parts[0].(map[string]any)["text"].(string)

	if text != transcript {
		t.Errorf("multi-speaker text = %q, want %q (prefix must NOT apply)", text, transcript)
	}
}
