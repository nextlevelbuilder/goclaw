package media

import "testing"

func TestBuildMediaTags_ImageWithSourceURL(t *testing.T) {
	tags := BuildMediaTags([]MediaInfo{{
		Type:      TypeImage,
		SourceURL: "https://cdn.discordapp.com/attachments/1/2/photo.jpg",
	}})

	want := `<media:image url="https://cdn.discordapp.com/attachments/1/2/photo.jpg">`
	if tags != want {
		t.Fatalf("BuildMediaTags() = %q, want %q", tags, want)
	}
}
