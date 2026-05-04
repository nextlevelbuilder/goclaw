package common

import (
	"strings"
	"testing"
)

func TestValidateSlug(t *testing.T) {
	t.Parallel()
	long63 := "a" + strings.Repeat("b", 62)
	long64 := "a" + strings.Repeat("b", 63)
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid simple", "my-oa", false},
		{"valid digit-prefix", "oa1", false},
		{"valid hyphenated", "a-b-c", false},
		{"valid 63 chars", long63, false},
		{"empty", "", true},
		{"too long", long64, true},
		{"uppercase", "My-OA", true},
		{"leading hyphen", "-leading", true},
		{"trailing hyphen", "trailing-", true},
		{"single char", "a", true},
		{"slash", "with/slash", true},
		{"dot", "with.dot", true},
		{"space", "with space", true},
		{"underscore", "with_underscore", true},
		{"reserved zalo", "zalo", true},
		{"reserved webhook", "webhook", true},
		{"reserved _health", "_health", true},
		{"reserved _metrics", "_metrics", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSlug(tc.in)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateSlug(%q) err = %v, wantErr=%v", tc.in, err, tc.wantErr)
			}
		})
	}
}

func TestDeriveSlugFromName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"My OA", "my-oa"},
		{"Customer Support OA #1", "customer-support-oa-1"},
		{"Hello!!!World", "hello-world"},
		{"  spaced  ", "spaced"},
		{"---hyphens---", "hyphens"},
		{"UPPER", "upper"},
		{"a__b", "a-b"},
		{"a   b", "a-b"},
		{"123abc", "123abc"},
		{"", ""},
		{"!!!", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := DeriveSlugFromName(tc.in); got != tc.want {
				t.Errorf("DeriveSlugFromName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDeriveSlugFromName_ClampsTo63(t *testing.T) {
	t.Parallel()
	in := strings.Repeat("a", 100)
	got := DeriveSlugFromName(in)
	if len(got) > 63 {
		t.Errorf("DeriveSlugFromName clamped len = %d, want <= 63", len(got))
	}
}

func TestReservedSlugs_AllRejected(t *testing.T) {
	t.Parallel()
	for slug := range ReservedSlugs {
		if err := ValidateSlug(slug); err == nil {
			t.Errorf("ValidateSlug(%q) should reject reserved slug", slug)
		}
	}
}
