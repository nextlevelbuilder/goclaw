package personal

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

func TestCookieObserverNotifiesOnSet(t *testing.T) {
	base, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	observer := &cookieObserver{base: base, onSet: func() { calls++ }}
	observer.SetCookies(&url.URL{Scheme: "https", Host: "chat.zalo.me"}, []*http.Cookie{{Name: "a", Value: "1"}})
	if calls != 1 {
		t.Fatalf("onSet calls = %d, want 1", calls)
	}
	observer.SetCookies(&url.URL{Scheme: "https", Host: "chat.zalo.me"}, nil)
	if calls != 1 {
		t.Fatalf("empty SetCookies notified: calls = %d, want 1", calls)
	}
}

func TestSnapshotCredentialsIncludesServiceMapHosts(t *testing.T) {
	sess := protocol.NewSession()
	sess.IMEI = "imei-1"
	sess.UserAgent = "test-ua"
	lang := "vi"
	sess.Language = lang
	sess.LoginInfo = &protocol.LoginInfo{
		ZpwServiceMapV3: protocol.ZpwServiceMapV3{
			GroupPoll: []string{"https://tt-group-poll-wpa.chat.zalo.me"},
		},
	}

	set := func(host, name, value string) {
		sess.CookieJar.SetCookies(&url.URL{Scheme: "https", Host: host}, []*http.Cookie{{Name: name, Value: value, Path: "/"}})
	}
	set("chat.zalo.me", "chat_cookie", "one")
	set("tt-group-poll-wpa.chat.zalo.me", "poll_cookie", "two")

	cred := snapshotCredentials(sess)
	if cred == nil {
		t.Fatal("snapshotCredentials returned nil")
	}
	if cred.IMEI != "imei-1" || cred.UserAgent != "test-ua" {
		t.Fatalf("identity fields not preserved: %+v", cred)
	}
	if cred.Language == nil || *cred.Language != "vi" {
		t.Fatalf("language not preserved: %+v", cred.Language)
	}
	names := map[string]bool{}
	for _, c := range cred.Cookie.GetCookies() {
		names[c.Name] = true
	}
	if !names["chat_cookie"] || !names["poll_cookie"] {
		t.Fatalf("snapshot missing service-map cookies: %v", names)
	}
}

func TestCookiePersisterSkipsUnchangedAndWritesOnChange(t *testing.T) {
	cred := &protocol.Credentials{IMEI: "imei-1", UserAgent: "ua"}
	var saved []*protocol.Credentials
	persister := newCookiePersister(time.Hour, func(c *protocol.Credentials) error {
		saved = append(saved, c)
		return nil
	})
	persister.Bind(func() *protocol.Credentials { return cred })

	persister.MarkDirty()
	if err := persister.Flush(context.Background()); err != nil {
		t.Fatalf("Flush unchanged: %v", err)
	}
	if len(saved) != 0 {
		t.Fatalf("unchanged snapshot was written %d times", len(saved))
	}

	union := protocol.NewHTTPCookies([]*http.Cookie{{Name: "a", Value: "1"}})
	cred.Cookie = &union
	persister.MarkDirty()
	if err := persister.Flush(context.Background()); err != nil {
		t.Fatalf("Flush changed: %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("changed snapshot written %d times, want 1", len(saved))
	}
}

func TestCookiePersisterCloseFlushesDirty(t *testing.T) {
	cred := &protocol.Credentials{IMEI: "imei-1", UserAgent: "ua"}
	var saved int
	persister := newCookiePersister(time.Hour, func(*protocol.Credentials) error {
		saved++
		return nil
	})
	persister.Bind(func() *protocol.Credentials { return cred })
	union := protocol.NewHTTPCookies([]*http.Cookie{{Name: "a", Value: "1"}})
	cred.Cookie = &union
	persister.MarkDirty()

	if err := persister.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if saved != 1 {
		t.Fatalf("saved = %d, want 1", saved)
	}
	persister.MarkDirty()
	if err := persister.Flush(context.Background()); err != nil {
		t.Fatalf("Flush after close: %v", err)
	}
	if saved != 1 {
		t.Fatalf("post-close dirty write: saved = %d, want 1", saved)
	}
}

func TestSaveCredentialsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.json")
	cred := &protocol.Credentials{IMEI: "imei-1", UserAgent: "ua"}
	if err := saveCredentials(path, cred); err != nil {
		t.Fatalf("saveCredentials: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read credentials: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("credentials file is empty")
	}
}
