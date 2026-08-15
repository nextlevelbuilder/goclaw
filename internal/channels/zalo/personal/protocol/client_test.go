package protocol

import (
	"strings"
	"testing"
)

func TestMakeURL(t *testing.T) {
	sess := &Session{Language: "vi", IMEI: "test-imei", UserAgent: DefaultUserAgent}

	t.Run("with defaults", func(t *testing.T) {
		u := makeURL(sess, "https://api.zalo.me/path", map[string]any{"foo": "bar"}, true)
		if !strings.Contains(u, "foo=bar") {
			t.Error("missing param foo=bar")
		}
		if !strings.Contains(u, "zpw_ver=") {
			t.Error("missing zpw_ver default")
		}
		if !strings.Contains(u, "zpw_type=") {
			t.Error("missing zpw_type default")
		}
	})

	t.Run("without defaults", func(t *testing.T) {
		u := makeURL(sess, "https://api.zalo.me/path", map[string]any{"key": "val"}, false)
		if !strings.Contains(u, "key=val") {
			t.Error("missing param")
		}
		if strings.Contains(u, "zpw_ver") {
			t.Error("should not have zpw_ver without defaults")
		}
	})

	t.Run("does not overwrite existing params", func(t *testing.T) {
		u := makeURL(sess, "https://api.zalo.me/path?foo=existing", map[string]any{"foo": "new"}, false)
		if strings.Contains(u, "foo=new") {
			t.Error("should not overwrite existing param")
		}
		if !strings.Contains(u, "foo=existing") {
			t.Error("should keep existing param")
		}
	})

	t.Run("invalid URL returns empty", func(t *testing.T) {
		u := makeURL(sess, "://invalid", nil, false)
		if u != "" {
			t.Errorf("expected empty for invalid URL, got %q", u)
		}
	})
}

func TestConvertToString(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want string
	}{
		{"string", "hello", "hello"},
		{"int", 42, "42"},
		{"int64", int64(100), "100"},
		{"uint", uint(7), "7"},
		{"float64", 3.14, "3.14"},
		{"bool", true, "true"},
		{"bytes", []byte("abc"), "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertToString(tt.val)
			if got != tt.want {
				t.Errorf("convertToString(%v) = %q, want %q", tt.val, got, tt.want)
			}
		})
	}
}

func TestGenerateSignKey(t *testing.T) {
	// Verify determinism: same inputs = same output
	params := map[string]any{
		"imei":           "test-imei",
		"type":           30,
		"client_version": 665,
	}

	key1 := generateSignKey("getserverinfo", params)
	key2 := generateSignKey("getserverinfo", params)

	if key1 != key2 {
		t.Error("generateSignKey should be deterministic")
	}
	if len(key1) != 32 {
		t.Errorf("expected 32-char md5 hex, got len %d", len(key1))
	}

	// Different type string = different key
	key3 := generateSignKey("getlogininfo", params)
	if key1 == key3 {
		t.Error("different type strings should produce different keys")
	}
}

func TestProcessStr(t *testing.T) {
	even, odd := processStr("ABCDEF")

	if strings.Join(even, "") != "ACE" {
		t.Errorf("even = %v, want [A C E]", even)
	}
	if strings.Join(odd, "") != "BDF" {
		t.Errorf("odd = %v, want [B D F]", odd)
	}
}

func TestProcessStr_Empty(t *testing.T) {
	even, odd := processStr("")
	if len(even) != 0 || len(odd) != 0 {
		t.Error("expected empty slices for empty string")
	}
}

func TestJoinFirst(t *testing.T) {
	parts := []string{"A", "B", "C", "D", "E"}

	if got := joinFirst(parts, 3); got != "ABC" {
		t.Errorf("joinFirst(5, 3) = %q, want 'ABC'", got)
	}
	if got := joinFirst(parts, 10); got != "ABCDE" {
		t.Errorf("joinFirst(5, 10) = %q, want 'ABCDE'", got)
	}
	if got := joinFirst(parts, 0); got != "" {
		t.Errorf("joinFirst(5, 0) = %q, want ''", got)
	}
}

func TestReverseCopy(t *testing.T) {
	in := []string{"A", "B", "C"}
	out := reverseCopy(in)

	if strings.Join(out, "") != "CBA" {
		t.Errorf("reverseCopy = %v, want [C B A]", out)
	}
	// Verify original is not mutated
	if strings.Join(in, "") != "ABC" {
		t.Error("original was mutated")
	}
}

func TestRandomHexString(t *testing.T) {
	for range 10 {
		s := randomHexString(6, 12)
		if len(s) < 6 || len(s) > 12 {
			t.Errorf("randomHexString(6,12) = len %d, want 6-12", len(s))
		}
	}

	// Fixed length
	s := randomHexString(8, 8)
	if len(s) != 8 {
		t.Errorf("randomHexString(8,8) = len %d, want 8", len(s))
	}
}

func TestGenerateIMEI(t *testing.T) {
	imei := GenerateIMEI("test-agent")
	parts := strings.Split(imei, "-")

	// UUID has 5 parts, then a dash, then md5 hash (32 chars)
	// Format: uuid-md5hash = 5 uuid parts + 1 md5 part = at least 6 parts
	if len(parts) < 6 {
		t.Errorf("IMEI has %d parts, expected at least 6", len(parts))
	}

	// Last part should be 32-char hex (md5)
	lastPart := parts[len(parts)-1]
	if len(lastPart) != 32 {
		t.Errorf("md5 part len = %d, want 32", len(lastPart))
	}

	// Deterministic md5 for same user agent
	imei2 := GenerateIMEI("test-agent")
	md5_1 := imei[strings.LastIndex(imei, "-")+1:]
	md5_2 := imei2[strings.LastIndex(imei2, "-")+1:]
	if md5_1 != md5_2 {
		t.Error("md5 hash should be deterministic for same user agent")
	}

	// Different UUID each time
	if imei == imei2 {
		t.Error("full IMEI should differ (random UUID)")
	}
}

func TestNewSession(t *testing.T) {
	sess := NewSession()
	if sess.UserAgent != DefaultUserAgent {
		t.Errorf("UserAgent = %q", sess.UserAgent)
	}
	if sess.Language != DefaultLanguage {
		t.Errorf("Language = %q", sess.Language)
	}
	if sess.CookieJar == nil {
		t.Error("CookieJar is nil")
	}
	if sess.Client == nil {
		t.Error("Client is nil")
	}
}

func TestBuildFormBody(t *testing.T) {
	body := buildFormBody(map[string]string{"key": "value", "foo": "bar"})
	content := body.Len()
	if content == 0 {
		t.Error("form body is empty")
	}

	s := make([]byte, content)
	body.Read(s)
	str := string(s)
	if !strings.Contains(str, "key=value") {
		t.Error("missing key=value")
	}
	if !strings.Contains(str, "foo=bar") {
		t.Error("missing foo=bar")
	}
}
