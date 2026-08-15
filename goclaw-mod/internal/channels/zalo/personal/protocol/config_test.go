package protocol

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
	"time"
)

func TestCredentials_IsValid(t *testing.T) {
	tests := []struct {
		name  string
		cred  Credentials
		valid bool
	}{
		{"valid with cookies", Credentials{IMEI: "abc", UserAgent: "ua", Cookie: &CookieUnion{cookies: []Cookie{{Name: "a"}}}}, true},
		{"valid without cookies", Credentials{IMEI: "abc", UserAgent: "ua"}, true},
		{"missing IMEI", Credentials{UserAgent: "ua"}, false},
		{"missing UserAgent", Credentials{IMEI: "abc"}, false},
		{"empty", Credentials{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cred.IsValid(); got != tt.valid {
				t.Errorf("IsValid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestCookieUnion_MarshalUnmarshal_Array(t *testing.T) {
	cookies := []Cookie{
		{Name: "zpw_sek", Value: "abc123", Domain: "chat.zalo.me", Path: "/"},
		{Name: "zpw_enk", Value: "def456", Domain: "chat.zalo.me", Path: "/"},
	}
	cu := CookieUnion{cookies: cookies}

	b, err := cu.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	var cu2 CookieUnion
	if err := cu2.UnmarshalJSON(b); err != nil {
		t.Fatal(err)
	}

	got := cu2.GetCookies()
	if len(got) != 2 {
		t.Fatalf("got %d cookies, want 2", len(got))
	}
	if got[0].Name != "zpw_sek" || got[1].Name != "zpw_enk" {
		t.Errorf("cookie names mismatch: %q, %q", got[0].Name, got[1].Name)
	}
}

func TestCookieUnion_MarshalUnmarshal_J2Cookie(t *testing.T) {
	j2 := &J2Cookie{
		URL:     "https://chat.zalo.me",
		Cookies: []Cookie{{Name: "test", Value: "val"}},
	}
	cu := CookieUnion{j2cookie: j2}

	b, err := cu.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	var cu2 CookieUnion
	if err := cu2.UnmarshalJSON(b); err != nil {
		t.Fatal(err)
	}

	got := cu2.GetCookies()
	if len(got) != 1 || got[0].Name != "test" {
		t.Errorf("j2cookie roundtrip failed: %+v", got)
	}
}

func TestCookieUnion_Null(t *testing.T) {
	var cu CookieUnion
	if err := cu.UnmarshalJSON([]byte("null")); err != nil {
		t.Fatal(err)
	}
	if cu.IsValid() {
		t.Error("null CookieUnion should not be valid")
	}

	b, err := cu.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "null" {
		t.Errorf("got %s, want null", b)
	}
}

func TestCookieUnion_MarshalJSON_BothSet(t *testing.T) {
	cu := CookieUnion{
		cookies:  []Cookie{{Name: "a"}},
		j2cookie: &J2Cookie{URL: "test"},
	}
	_, err := cu.MarshalJSON()
	if err == nil {
		t.Fatal("expected error when both cookies and j2cookie are set")
	}
}

func TestCookie_ToHTTPCookie(t *testing.T) {
	c := Cookie{
		Domain:         "chat.zalo.me",
		Name:           "test",
		Value:          "val",
		Path:           "/",
		HTTPOnly:       true,
		Secure:         true,
		SameSite:       SameSiteNone,
		ExpirationDate: float64(time.Now().Add(time.Hour).Unix()),
	}

	hc := c.ToHTTPCookie()
	if hc.Name != "test" || hc.Value != "val" {
		t.Error("name/value mismatch")
	}
	if !hc.HttpOnly || !hc.Secure {
		t.Error("HttpOnly/Secure not set")
	}
	if hc.SameSite != http.SameSiteNoneMode {
		t.Errorf("SameSite = %d, want %d", hc.SameSite, http.SameSiteNoneMode)
	}
	if hc.Expires.IsZero() {
		t.Error("Expires should be set for non-session cookie")
	}
}

func TestCookie_FromHTTPCookie(t *testing.T) {
	hc := &http.Cookie{
		Domain:   "chat.zalo.me",
		Name:     "zpw_sek",
		Value:    "secret",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   3600,
	}

	var c Cookie
	c.FromHTTPCookie(hc)

	if c.Name != "zpw_sek" || c.Value != "secret" {
		t.Error("name/value mismatch")
	}
	if !c.HTTPOnly {
		t.Error("HTTPOnly should be true (from hc.HttpOnly)")
	}
	if c.SameSite != SameSiteLax {
		t.Errorf("SameSite = %q, want %q", c.SameSite, SameSiteLax)
	}
	if c.Session {
		t.Error("Session should be false when MaxAge > 0")
	}
	if c.ExpirationDate == 0 {
		t.Error("ExpirationDate should be set when MaxAge > 0")
	}
}

func TestBuildCookieJar_DoesNotMutateOriginal(t *testing.T) {
	cookies := []Cookie{
		{Domain: ".chat.zalo.me", Name: "test", Value: "val", Path: "/"},
	}
	cu := CookieUnion{cookies: cookies}

	u, _ := url.Parse("https://chat.zalo.me")
	jar, _ := cookiejar.New(nil)

	// Save original domain
	origDomain := cu.GetCookies()[0].Domain

	cu.BuildCookieJar(u, jar)

	// Original should not be mutated
	if cu.GetCookies()[0].Domain != origDomain {
		t.Errorf("BuildCookieJar mutated original: got %q, want %q", cu.GetCookies()[0].Domain, origDomain)
	}
}

func TestBuildCookieJar_Idempotent(t *testing.T) {
	cookies := []Cookie{
		{Domain: ".chat.zalo.me", Name: "test", Value: "val", Path: "/"},
	}
	cu := CookieUnion{cookies: cookies}
	u, _ := url.Parse("https://chat.zalo.me")

	jar1, _ := cookiejar.New(nil)
	jar2, _ := cookiejar.New(nil)

	cu.BuildCookieJar(u, jar1)
	cu.BuildCookieJar(u, jar2)

	c1 := jar1.Cookies(u)
	c2 := jar2.Cookies(u)
	if len(c1) != len(c2) {
		t.Errorf("non-idempotent: first call got %d cookies, second got %d", len(c1), len(c2))
	}
}

func TestSameSite_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		val  SameSite
		json string
	}{
		{SameSiteDefault, "null"},
		{SameSiteLax, `"lax"`},
		{SameSiteStrict, `"strict"`},
		{SameSiteNone, `"none"`},
	}

	for _, tt := range tests {
		b, err := tt.val.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != tt.json {
			t.Errorf("MarshalJSON(%q) = %s, want %s", tt.val, b, tt.json)
		}

		var got SameSite
		if err := got.UnmarshalJSON(b); err != nil {
			t.Fatal(err)
		}
		if got != tt.val {
			t.Errorf("UnmarshalJSON(%s) = %q, want %q", tt.json, got, tt.val)
		}
	}
}

func TestCredentials_JSON_Roundtrip(t *testing.T) {
	lang := "vi"
	cred := Credentials{
		IMEI:      "test-imei-123",
		UserAgent: "Mozilla/5.0",
		Language:  &lang,
		Cookie: &CookieUnion{cookies: []Cookie{
			{Name: "zpw_sek", Value: "abc", Domain: "chat.zalo.me"},
		}},
	}

	b, err := json.Marshal(cred)
	if err != nil {
		t.Fatal(err)
	}

	var got Credentials
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}

	if got.IMEI != cred.IMEI || got.UserAgent != cred.UserAgent {
		t.Error("IMEI/UserAgent mismatch")
	}
	if got.Language == nil || *got.Language != "vi" {
		t.Error("Language mismatch")
	}
	if got.Cookie == nil || len(got.Cookie.GetCookies()) != 1 {
		t.Error("Cookie mismatch")
	}
}
