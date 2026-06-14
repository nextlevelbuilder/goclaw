package meow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateButtonURL(t *testing.T) {
	allowed := DefaultButtonHostAllowlist()
	// The channel's registered (pre-vetted) button URLs.
	ok := []string{
		"https://t.me/TonSlotGameBot?startapp=VGtF0UV3Z3HW",
		"https://play.tonslot.site",
		"https://jackpot.1dollar.day/post/event/new-player-bonus",
		"https://www.youtube.com/@TONSLOT",
		"https://x.com/1_DollarJackpot",
	}
	registered := NewURLSet(ok)
	for _, u := range ok {
		if err := ValidateButtonURL(u, registered, allowed); err != nil {
			t.Errorf("expected %q allowed, got %v", u, err)
		}
	}
	bad := []string{
		"tg://resolve?domain=evil",          // tg scheme
		"javascript:alert(1)",               // js scheme
		"http://t.me/x",                     // not https
		"https://t.me.attacker.com/x",       // lookalike host
		"https://evil.com/phish",            // off-allowlist host
		"https://1.2.3.4/x",                 // IP host
		"https://tonslot.site.attacker.com", // suffix trick
		"https://t.me/attacker_phish_bot",   // allowed host, NOT registered
		"https://www.youtube.com/@someoneelse",
	}
	for _, u := range bad {
		if err := ValidateButtonURL(u, registered, allowed); err == nil {
			t.Errorf("expected %q rejected, got nil", u)
		}
	}
}

func TestValidateImagePath(t *testing.T) {
	root := t.TempDir()
	img := filepath.Join(root, "post.png")
	if err := os.WriteFile(img, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Inside root → ok.
	if _, err := ValidateImagePath(img, []string{root}); err != nil {
		t.Fatalf("expected in-root path allowed: %v", err)
	}

	// Outside root → rejected.
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("x"), 0o600)
	if _, err := ValidateImagePath(outside, []string{root}); err == nil {
		t.Error("expected outside-root path rejected")
	}

	// Symlink inside root pointing outside → resolves out, rejected.
	link := filepath.Join(root, "link.png")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := ValidateImagePath(link, []string{root}); err == nil {
			t.Error("expected symlink-escaping path rejected")
		}
	}

	// Traversal + empty + missing → rejected.
	for _, p := range []string{"", filepath.Join(root, "../etc/passwd"), filepath.Join(root, "nope.png")} {
		if _, err := ValidateImagePath(p, []string{root}); err == nil {
			t.Errorf("expected %q rejected", p)
		}
	}
}
