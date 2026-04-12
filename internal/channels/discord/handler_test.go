package discord

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// --- resolveDisplayName ---

func TestResolveDisplayName(t *testing.T) {
	tests := []struct {
		name   string
		nick   string
		global string
		uname  string
		want   string
	}{
		{"nick wins over all", "ServerNick", "GlobalName", "username", "ServerNick"},
		{"global wins over username", "", "GlobalName", "username", "GlobalName"},
		{"username fallback", "", "", "username", "username"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &discordgo.MessageCreate{
				Message: &discordgo.Message{
					Author: &discordgo.User{
						Username:   tt.uname,
						GlobalName: tt.global,
					},
				},
			}
			if tt.nick != "" {
				m.Member = &discordgo.Member{Nick: tt.nick}
			}
			got := resolveDisplayName(m)
			if got != tt.want {
				t.Errorf("resolveDisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- tryHandleCommand: routing only (no session calls) ---

func TestTryHandleCommandRoutingNonCommand(t *testing.T) {
	// Only test the routing decision for non-command inputs.
	// Known commands (addwriter, etc.) immediately call session.ChannelMessageSend
	// so they require a live test server — covered in TestTryHandleCommandKnownCommands.
	ch := &Channel{}

	nonCmds := []string{"hello world", "", "justtext", "123"}
	for _, content := range nonCmds {
		t.Run(content, func(t *testing.T) {
			m := &discordgo.MessageCreate{
				Message: &discordgo.Message{
					Content:   content,
					GuildID:   "guild-1",
					ChannelID: "chan-1",
					Author:    &discordgo.User{ID: "user-1"},
				},
			}
			got := ch.tryHandleCommand(m)
			if got != false {
				t.Errorf("tryHandleCommand(%q) = %v, want false", content, got)
			}
		})
	}
}

func TestTryHandleCommandKnownCommands(t *testing.T) {
	// Known commands return true and execute the handler. Use a test server so
	// session.ChannelMessageSend doesn't panic.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"m","channel_id":"chan-1","content":"ok"}`))
	}))
	defer server.Close()

	ch := newTestChannel(t, server)

	knownCmds := []string{"!addwriter", "/addwriter", "!removewriter", "!writers", "/writers", "!ADDWRITER"}
	for _, content := range knownCmds {
		t.Run(content, func(t *testing.T) {
			m := &discordgo.MessageCreate{
				Message: &discordgo.Message{
					Content:   content,
					GuildID:   "guild-1",
					ChannelID: "chan-1",
					Author:    &discordgo.User{ID: "user-1"},
				},
			}
			got := ch.tryHandleCommand(m)
			if !got {
				t.Errorf("tryHandleCommand(%q) = false, want true", content)
			}
		})
	}

	unknownCmds := []string{"!unknown", "/foo"}
	for _, content := range unknownCmds {
		t.Run(content, func(t *testing.T) {
			m := &discordgo.MessageCreate{
				Message: &discordgo.Message{
					Content:   content,
					GuildID:   "guild-1",
					ChannelID: "chan-1",
					Author:    &discordgo.User{ID: "user-1"},
				},
			}
			got := ch.tryHandleCommand(m)
			if got {
				t.Errorf("tryHandleCommand(%q) = true, want false", content)
			}
		})
	}
}

// --- lastIndexByte ---

func TestLastIndexByte(t *testing.T) {
	tests := []struct {
		name string
		s    string
		c    byte
		want int
	}{
		{"found at end", "hello\nworld", '\n', 5},
		{"found at start", "\nhello", '\n', 0},
		{"not found", "hello", '\n', -1},
		{"empty string", "", '\n', -1},
		{"multiple — last one", "a\nb\nc", '\n', 3},
		{"single char match", "\n", '\n', 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lastIndexByte(tt.s, tt.c)
			if got != tt.want {
				t.Errorf("lastIndexByte(%q, %q) = %d, want %d", tt.s, tt.c, got, tt.want)
			}
		})
	}
}

// --- classifyMediaType ---

func TestClassifyMediaType(t *testing.T) {
	tests := []struct {
		contentType string
		filename    string
		want        string
	}{
		{"image/jpeg", "photo.jpg", "image"},
		{"image/png", "photo.png", "image"},
		{"video/mp4", "video.mp4", "video"},
		{"audio/mpeg", "audio.mp3", "audio"},
		{"audio/ogg", "voice.ogg", "audio"},
		{"application/pdf", "doc.pdf", "document"},
		{"text/plain", "file.txt", "document"},
		{"", "file.bin", "document"},
	}

	for _, tt := range tests {
		t.Run(tt.contentType+"/"+tt.filename, func(t *testing.T) {
			got := classifyMediaType(tt.contentType, tt.filename)
			if got != tt.want {
				t.Errorf("classifyMediaType(%q, %q) = %q, want %q", tt.contentType, tt.filename, got, tt.want)
			}
		})
	}
}

// --- urlFileName ---

func TestURLFileName(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://cdn.discord.com/attachments/photo.jpg", "photo.jpg"},
		{"https://cdn.discord.com/attachments/photo.jpg?ex=abc&is=def", "photo.jpg"},
		{"https://cdn.discord.com/a/b/c/file.pdf", "file.pdf"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := urlFileName(tt.url)
			if got != tt.want {
				t.Errorf("urlFileName(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

// --- Send: not running returns error ---

func TestSendNotRunningReturnsError(t *testing.T) {
	ch := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypeDiscord, nil, nil),
	}
	// SetRunning(false) is default; just ensure not running.
	err := ch.Send(context.Background(), bus.OutboundMessage{ChatID: "chan-1", Content: "hi"})
	if err == nil {
		t.Error("Send() when not running should return error")
	}
}

// --- Send: empty chatID returns error ---

func TestSendEmptyChatIDReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"msg","content":"ok"}`))
	}))
	defer server.Close()

	ch := newTestChannel(t, server)

	err := ch.Send(context.Background(), bus.OutboundMessage{ChatID: "", Content: "hi"})
	if err == nil {
		t.Error("Send() with empty chatID should return error")
	}
}

// --- Send: empty content deletes placeholder ---

func TestSendDeletesPlaceholderOnEmptyContent(t *testing.T) {
	deleteCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			deleteCount++
			w.WriteHeader(204)
			return
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"placeholder-1","channel_id":"chan-1","content":""}`))
	}))
	defer server.Close()

	ch := newTestChannel(t, server)
	ch.placeholders.Store("inbound-1", "placeholder-1")

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:   "chan-1",
		Content:  "",
		Metadata: map[string]string{"placeholder_key": "inbound-1"},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if _, ok := ch.placeholders.Load("inbound-1"); ok {
		t.Error("placeholder should have been removed from map on empty content")
	}
	if deleteCount == 0 {
		t.Error("expected DELETE request to remove placeholder message")
	}
}

// --- Send: placeholder_update edits without consuming placeholder ---

func TestSendPlaceholderUpdateKeepsPlaceholder(t *testing.T) {
	editCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPatch {
			editCount++
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"placeholder-1","channel_id":"chan-1","content":"updating"}`))
	}))
	defer server.Close()

	ch := newTestChannel(t, server)
	ch.placeholders.Store("inbound-1", "placeholder-1")

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "chan-1",
		Content: "updating...",
		Metadata: map[string]string{
			"placeholder_key":    "inbound-1",
			"placeholder_update": "true",
		},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if _, ok := ch.placeholders.Load("inbound-1"); !ok {
		t.Error("placeholder should remain in map after placeholder_update")
	}
	if editCount == 0 {
		t.Error("expected PATCH request for placeholder_update")
	}
}

// --- resolveMedia: skip oversized attachment ---

func TestResolveMediaSkipsOversizedAttachment(t *testing.T) {
	attachments := []*discordgo.MessageAttachment{
		{URL: "https://example.com/big.bin", Size: 100, Filename: "big.bin"},
	}
	// maxBytes=50 < att.Size=100 → should be skipped
	results := resolveMedia(attachments, 50)
	if len(results) != 0 {
		t.Errorf("resolveMedia should skip oversized attachment, got %d results", len(results))
	}
}

// --- resolveMedia: nil/empty attachments ---

func TestResolveMediaEmpty(t *testing.T) {
	if results := resolveMedia(nil, 1024); len(results) != 0 {
		t.Errorf("resolveMedia(nil) should return empty, got %d", len(results))
	}
	if results := resolveMedia([]*discordgo.MessageAttachment{}, 1024); len(results) != 0 {
		t.Errorf("resolveMedia([]) should return empty, got %d", len(results))
	}
}
