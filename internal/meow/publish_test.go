package meow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakePubStore implements just the MeowStore methods PublishDue calls.
type fakePubStore struct {
	store.MeowStore
	ch          *store.MpChannel
	claim       *store.MpContentPost
	claimErr    error
	updateCalls int
	lastStatus  string
	lastMsgID   *int64
	lastLink    string
}

func (f *fakePubStore) GetChannel(_ context.Context, _, _ uuid.UUID) (*store.MpChannel, error) {
	return f.ch, nil
}
func (f *fakePubStore) ClaimPostForPublish(_ context.Context, _, _ uuid.UUID, _ time.Time, _ bool) (*store.MpContentPost, error) {
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	return f.claim, nil
}
func (f *fakePubStore) UpdatePostStatus(_ context.Context, _, _ uuid.UUID, status string, msgID *int64, link string) error {
	f.updateCalls++
	f.lastStatus, f.lastMsgID, f.lastLink = status, msgID, link
	return nil
}

type fakeSender struct {
	msgID int64
	err   error
	calls int
}

func (s *fakeSender) SendChannelPost(_ context.Context, _, _, _ string, _ []Button) (int64, error) {
	s.calls++
	return s.msgID, s.err
}

func strptr(s string) *string { return &s }

// validWebP returns the minimal bytes ValidateWebP accepts: a RIFF container
// tagged WEBP (12-byte header).
func validWebP() []byte { return []byte("RIFF\x00\x00\x00\x00WEBP") }

// buildPublisher wires a Publisher with a temp image dir + a launched channel
// whose button set registers the given button URL.
func buildPublisher(t *testing.T, sender *fakeSender, st *fakePubStore) (*Publisher, string) {
	t.Helper()
	root := t.TempDir()
	img := filepath.Join(root, "post.webp")
	if err := os.WriteFile(img, validWebP(), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Publisher{
		Store:        st,
		Sender:       sender,
		AllowedRoots: []string{root},
		AllowedHosts: DefaultButtonHostAllowlist(),
	}, img
}

func launchedChannel(buttonURL string) *store.MpChannel {
	bs, _ := json.Marshal([]Button{{Label: "Play now", URL: buttonURL}})
	return &store.MpChannel{
		ID: uuid.New(), TenantID: uuid.New(), Handle: "@TestChan",
		Launched: true, ChatID: strptr("-1001234567"), ButtonSet: bs,
	}
}

func draftPost(img, buttonURL string) *store.MpContentPost {
	bs, _ := json.Marshal([]Button{{Label: "Play now", URL: buttonURL}})
	return &store.MpContentPost{
		ID: uuid.New(), Status: store.MpPostPublishing,
		KoText: "안녕", EnText: "hi", ImagePath: img, Buttons: bs,
	}
}

func TestPublishDue_HappyPath(t *testing.T) {
	url := "https://t.me/TestBot?startapp=x"
	sender := &fakeSender{msgID: 999}
	st := &fakePubStore{ch: launchedChannel(url)}
	pub, img := buildPublisher(t, sender, st)
	st.claim = draftPost(img, url)

	res, err := pub.PublishDue(context.Background(), uuid.New(), uuid.New(), time.Now(), false)
	if err != nil {
		t.Fatalf("PublishDue: %v", err)
	}
	if sender.calls != 1 {
		t.Fatalf("sender should be called once, got %d", sender.calls)
	}
	if res == nil || res.MessageID != 999 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if st.lastStatus != store.MpPostPublished || st.lastMsgID == nil || *st.lastMsgID != 999 {
		t.Fatalf("expected published+msgid persisted, got status=%s msgid=%v", st.lastStatus, st.lastMsgID)
	}
	if res.Link != "https://t.me/TestChan/999" {
		t.Fatalf("bad link: %s", res.Link)
	}
}

func TestPublishDue_PreLaunchRejected(t *testing.T) {
	sender := &fakeSender{}
	ch := launchedChannel("https://t.me/TestBot")
	ch.Launched = false
	st := &fakePubStore{ch: ch}
	pub, _ := buildPublisher(t, sender, st)

	_, err := pub.PublishDue(context.Background(), uuid.New(), uuid.New(), time.Now(), false)
	if !errors.Is(err, ErrChannelNotLaunched) {
		t.Fatalf("expected ErrChannelNotLaunched, got %v", err)
	}
	if sender.calls != 0 {
		t.Fatal("must not send to a pre-launch channel")
	}
}

func TestPublishDue_NoClaimableIsNoOp(t *testing.T) {
	sender := &fakeSender{}
	st := &fakePubStore{ch: launchedChannel("https://t.me/TestBot"), claimErr: store.ErrMeowNoClaimablePost}
	pub, _ := buildPublisher(t, sender, st)

	res, err := pub.PublishDue(context.Background(), uuid.New(), uuid.New(), time.Now(), false)
	if err != nil || res != nil {
		t.Fatalf("expected no-op (nil,nil), got res=%v err=%v", res, err)
	}
	if sender.calls != 0 {
		t.Fatal("no claim → no send")
	}
}

func TestPublishDue_BadButtonRejectedBeforeSend(t *testing.T) {
	registeredURL := "https://t.me/TestBot"
	sender := &fakeSender{}
	st := &fakePubStore{ch: launchedChannel(registeredURL)}
	pub, img := buildPublisher(t, sender, st)
	// Claimed post carries an off-allowlist button not in the channel set.
	st.claim = draftPost(img, "https://evil.com/phish")

	_, err := pub.PublishDue(context.Background(), uuid.New(), uuid.New(), time.Now(), false)
	if err == nil {
		t.Fatal("expected bad-button rejection")
	}
	if sender.calls != 0 {
		t.Fatal("must validate before sending")
	}
	if st.updateCalls != 0 {
		t.Fatal("must not mark published on validation failure")
	}
}

func TestPublishDue_SendErrorLeavesPublishing(t *testing.T) {
	url := "https://t.me/TestBot"
	sender := &fakeSender{err: errors.New("telegram 500")}
	st := &fakePubStore{ch: launchedChannel(url)}
	pub, img := buildPublisher(t, sender, st)
	st.claim = draftPost(img, url)

	_, err := pub.PublishDue(context.Background(), uuid.New(), uuid.New(), time.Now(), false)
	if err == nil {
		t.Fatal("expected send error")
	}
	// Row must stay 'publishing' (no published write) for reconciliation.
	if st.updateCalls != 0 {
		t.Fatalf("must not persist published after send failure (got %d update calls)", st.updateCalls)
	}
}

func TestPublishDue_CorruptImageRejectedBeforeSend(t *testing.T) {
	url := "https://t.me/TestBot"
	sender := &fakeSender{}
	st := &fakePubStore{ch: launchedChannel(url)}
	// Build a publisher, then overwrite the claimed post's image with bytes that
	// pass path containment but are NOT a WebP (no RIFF/WEBP signature).
	pub, img := buildPublisher(t, sender, st)
	if err := os.WriteFile(img, []byte("not-a-webp-file-at-all"), 0o600); err != nil {
		t.Fatal(err)
	}
	st.claim = draftPost(img, url)

	_, err := pub.PublishDue(context.Background(), uuid.New(), uuid.New(), time.Now(), false)
	if err == nil {
		t.Fatal("expected corrupt-image rejection")
	}
	if sender.calls != 0 {
		t.Fatal("must validate image bytes before sending")
	}
	if st.updateCalls != 0 {
		t.Fatal("must not mark published on a corrupt image")
	}
}

func TestPublishDue_ChatIDUnresolved(t *testing.T) {
	sender := &fakeSender{}
	ch := launchedChannel("https://t.me/TestBot")
	ch.ChatID = nil
	st := &fakePubStore{ch: ch}
	pub, _ := buildPublisher(t, sender, st)

	_, err := pub.PublishDue(context.Background(), uuid.New(), uuid.New(), time.Now(), false)
	if !errors.Is(err, ErrChatIDUnresolved) {
		t.Fatalf("expected ErrChatIDUnresolved, got %v", err)
	}
	if sender.calls != 0 {
		t.Fatal("no chat_id → no send")
	}
}
