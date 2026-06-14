package meow

import "testing"

const validBundle = `{
  "handle": "@kingboardgamesofficial",
  "scheduled_date": "2026-06-16",
  "ko_text": "오늘의 대진표가 열립니다.",
  "en_text": "Today's bracket opens.",
  "image": "2026-06-16.webp",
  "buttons": [{"label": "Play now", "url": "https://t.me/holdemblitz_bot"}]
}`

func TestParseAndValidateDraftBundle_Valid(t *testing.T) {
	b, err := ParseDraftBundle([]byte(validBundle))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if d, _ := b.Date(); d.Year() != 2026 || d.Month() != 6 || d.Day() != 16 {
		t.Fatalf("date parse wrong: %v", d)
	}
}

func TestParseDraftBundle_UnknownFieldRejected(t *testing.T) {
	if _, err := ParseDraftBundle([]byte(`{"handle":"@x","typo_field":1}`)); err == nil {
		t.Fatal("expected unknown-field rejection")
	}
}

func TestValidateDraftBundle_Invalid(t *testing.T) {
	cases := map[string]DraftBundle{
		"no @handle":      {Handle: "kbg", ScheduledDate: "2026-06-16", EnText: "x", Image: "a.webp"},
		"bad date":        {Handle: "@k", ScheduledDate: "16/06/2026", EnText: "x", Image: "a.webp"},
		"no text":         {Handle: "@k", ScheduledDate: "2026-06-16", Image: "a.webp"},
		"no image":        {Handle: "@k", ScheduledDate: "2026-06-16", EnText: "x"},
		"image is path":   {Handle: "@k", ScheduledDate: "2026-06-16", EnText: "x", Image: "../../etc/passwd"},
		"image subdir":    {Handle: "@k", ScheduledDate: "2026-06-16", EnText: "x", Image: "sub/a.webp"},
		"image not webp":  {Handle: "@k", ScheduledDate: "2026-06-16", EnText: "x", Image: "a.png"},
		"button no label": {Handle: "@k", ScheduledDate: "2026-06-16", EnText: "x", Image: "a.webp", Buttons: []Button{{URL: "https://t.me/x"}}},
		"button not https": {Handle: "@k", ScheduledDate: "2026-06-16", EnText: "x", Image: "a.webp", Buttons: []Button{{Label: "Go", URL: "tg://x"}}},
	}
	for name, b := range cases {
		b := b
		if err := b.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}
