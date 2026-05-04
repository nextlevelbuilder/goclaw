package i18n

import (
	"testing"
)

func TestNormalizePOSIXLocale(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ko_KR.UTF-8", "ko"},
		{"ko_KR", "ko"},
		{"ko", "ko"},
		{"vi_VN.UTF-8", "vi"},
		{"zh_CN.UTF-8", "zh"},
		{"zh_TW.Big5", "zh"},
		{"en_US.UTF-8", "en"},
		{"en_US@something", "en"},
		{"C", ""},
		{"POSIX", ""},
		{"", ""},
		{"jp_JP.UTF-8", ""}, // unsupported language
		{"ja", ""},          // unsupported language
		{"xx_YY", ""},       // garbage
	}
	for _, tc := range cases {
		got := normalizePOSIXLocale(tc.in)
		if got != tc.want {
			t.Errorf("normalizePOSIXLocale(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Note: Default()'s env-var/system-locale resolution is exercised end-to-end
// by setting envs + ResetDefaultForTest. Done in a separate test to keep
// each scenario hermetic.
func TestDefaultPriority(t *testing.T) {
	cases := []struct {
		name        string
		goclawEnv   string
		lcAll       string
		lcMessages  string
		lang        string
		want        string
	}{
		{name: "no env → fallback en", want: LocaleEN},
		{name: "GOCLAW_DEFAULT_LOCALE wins", goclawEnv: "ko", lang: "vi_VN", want: "ko"},
		{name: "LC_ALL fallback when no GOCLAW", lcAll: "vi_VN.UTF-8", lang: "zh_CN", want: "vi"},
		{name: "LC_MESSAGES used when LC_ALL empty", lcMessages: "zh_CN.UTF-8", lang: "en_US", want: "zh"},
		{name: "LANG used when LC_ALL/LC_MESSAGES empty", lang: "ko_KR.UTF-8", want: "ko"},
		{name: "GOCLAW_DEFAULT_LOCALE invalid → falls through", goclawEnv: "klingon", lang: "ko_KR", want: "ko"},
		{name: "system locale unsupported → en", lang: "ja_JP", want: LocaleEN},
		{name: "C locale skipped", lang: "C", want: LocaleEN},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOCLAW_DEFAULT_LOCALE", tc.goclawEnv)
			t.Setenv("LC_ALL", tc.lcAll)
			t.Setenv("LC_MESSAGES", tc.lcMessages)
			t.Setenv("LANG", tc.lang)
			ResetDefaultForTest()
			got := Default()
			if got != tc.want {
				t.Errorf("Default() = %q, want %q", got, tc.want)
			}
		})
	}
}
