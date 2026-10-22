package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	testToken       = "test-token"
	testBotID       = "bot1"
	testBotUsername = "goclawbot"
)

// recordedCall is one REST request the fake server received.
type recordedCall struct {
	Method string
	Path   string
	Body   map[string]any
}

// fakeServer is an in-memory Mattermost API v4 used by the tests.
type fakeServer struct {
	*httptest.Server
	t *testing.T

	mu       sync.Mutex
	calls    []recordedCall
	posts    map[string]*mmPost   // GET /posts/{id}
	files    map[string][]byte    // GET /files/{id}
	channels map[string]mmChannel // GET /channels/{id}; unknown IDs are public channels
	nextID   int

	// wsHandler, when set, serves /api/v4/websocket after the upgrade.
	wsHandler func(conn *websocket.Conn)
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	fs := &fakeServer{
		t:        t,
		posts:    make(map[string]*mmPost),
		files:    make(map[string][]byte),
		channels: make(map[string]mmChannel),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/users/me", func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, mmUser{ID: testBotID, Username: testBotUsername, IsBot: true})
	})
	mux.HandleFunc("GET /api/v4/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		writeJSONResponse(w, mmUser{ID: id, Username: "alice", FirstName: "Alice", LastName: "Smith"})
	})
	mux.HandleFunc("GET /api/v4/posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		p, ok := fs.posts[r.PathValue("id")]
		fs.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSONResponse(w, p)
	})
	mux.HandleFunc("GET /api/v4/channels/{id}", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		id := r.PathValue("id")
		fs.mu.Lock()
		ch, ok := fs.channels[id]
		fs.mu.Unlock()
		if !ok {
			ch = mmChannel{ID: id, Type: "O", DisplayName: "Town Square"}
		}
		writeJSONResponse(w, ch)
	})
	mux.HandleFunc("POST /api/v4/posts", func(w http.ResponseWriter, r *http.Request) {
		body := fs.record(r)
		var p mmPost
		raw, _ := json.Marshal(body)
		_ = json.Unmarshal(raw, &p)
		p.ID = fs.newID("post")
		writeJSONResponse(w, p)
	})
	mux.HandleFunc("PUT /api/v4/posts/{id}/patch", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		writeJSONResponse(w, map[string]string{"id": r.PathValue("id")})
	})
	mux.HandleFunc("DELETE /api/v4/posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		writeJSONResponse(w, map[string]string{"status": "OK"})
	})
	mux.HandleFunc("POST /api/v4/users/{id}/typing", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		writeJSONResponse(w, map[string]string{"status": "OK"})
	})
	mux.HandleFunc("POST /api/v4/reactions", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		writeJSONResponse(w, map[string]string{"status": "OK"})
	})
	mux.HandleFunc("DELETE /api/v4/users/{uid}/posts/{pid}/reactions/{emoji}", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r)
		writeJSONResponse(w, map[string]string{"status": "OK"})
	})
	mux.HandleFunc("GET /api/v4/files/{id}", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		data, ok := fs.files[r.PathValue("id")]
		fs.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(data)
	})
	mux.HandleFunc("POST /api/v4/files", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f, hdr, err := r.FormFile("files")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(f)
		fs.mu.Lock()
		fs.calls = append(fs.calls, recordedCall{Method: r.Method, Path: r.URL.Path, Body: map[string]any{
			"channel_id": r.FormValue("channel_id"),
			"filename":   hdr.Filename,
			"content":    string(data),
		}})
		fs.mu.Unlock()
		writeJSONResponse(w, fileUploadResponse{FileInfos: []mmFileInfo{{ID: fs.newID("file"), Name: hdr.Filename}}})
	})
	mux.HandleFunc("GET /api/v4/websocket", func(w http.ResponseWriter, r *http.Request) {
		if fs.wsHandler == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		fs.wsHandler(conn)
	})

	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/websocket" && r.Header.Get("Authorization") != "Bearer "+testToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"id":"api.context.session_expired.app_error","message":"Invalid or expired session","status_code":401}`))
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *fakeServer) record(r *http.Request) map[string]any {
	var body map[string]any
	if r.Body != nil {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
	}
	fs.mu.Lock()
	fs.calls = append(fs.calls, recordedCall{Method: r.Method, Path: r.URL.Path, Body: body})
	fs.mu.Unlock()
	return body
}

func (fs *fakeServer) newID(prefix string) string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.nextID++
	return fmt.Sprintf("%s%d", prefix, fs.nextID)
}

// callsTo returns recorded calls matching method and path.
func (fs *fakeServer) callsTo(method, path string) []recordedCall {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	var out []recordedCall
	for _, c := range fs.calls {
		if c.Method == method && c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

func (fs *fakeServer) addPost(p *mmPost) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.posts[p.ID] = p
}

func (fs *fakeServer) addFile(id string, data []byte) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.files[id] = data
}

func (fs *fakeServer) addChannel(ch mmChannel) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.channels[ch.ID] = ch
}

func writeJSONResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// newTestChannel builds a running channel against the fake server without
// opening the websocket.
func newTestChannel(t *testing.T, fs *fakeServer, mutate func(*config.MattermostConfig)) (*Channel, *bus.MessageBus) {
	t.Helper()
	cfg := config.MattermostConfig{
		ServerURL:   fs.URL,
		BotToken:    testToken,
		DMPolicy:    "open",
		GroupPolicy: "open",
	}
	if mutate != nil {
		mutate(&cfg)
	}
	msgBus := bus.New()
	ch, err := New(cfg, msgBus, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch.setBotIdentity(testBotID, testBotUsername)
	ch.SetRunning(true)
	t.Cleanup(ch.stopAllTyping)
	return ch, msgBus
}

// postedEvent wraps a post in a websocket "posted" event.
func postedEvent(t *testing.T, post mmPost, channelType string, mentions []string) wsEvent {
	t.Helper()
	raw, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshal post: %v", err)
	}
	data := map[string]any{
		"post":                 string(raw),
		"channel_type":         channelType,
		"channel_display_name": "Town Square",
	}
	if len(mentions) > 0 {
		m, _ := json.Marshal(mentions)
		data["mentions"] = string(m)
	}
	return wsEvent{Event: "posted", Data: data}
}

// editedEvent wraps a post in a websocket "post_edited" event, which carries
// only the post.
func editedEvent(t *testing.T, post mmPost) wsEvent {
	t.Helper()
	raw, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshal post: %v", err)
	}
	return wsEvent{Event: "post_edited", Data: map[string]any{"post": string(raw)}}
}

// consumeInbound waits briefly for the next inbound message.
func consumeInbound(t *testing.T, msgBus *bus.MessageBus, wait time.Duration) (bus.InboundMessage, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	return msgBus.ConsumeInbound(ctx)
}

// fakePairingStore implements only the methods the channel calls.
type fakePairingStore struct {
	store.PairingStore
	mu       sync.Mutex
	requests []string
}

func (f *fakePairingStore) RequestPairing(_ context.Context, senderID, _, _, _ string, _ map[string]string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, senderID)
	return "SECRET42", nil
}

func (f *fakePairingStore) IsPaired(context.Context, string, string) (bool, error) {
	return false, nil
}
