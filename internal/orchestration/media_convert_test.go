package orchestration

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

func TestMediaResultToBusFiles_Empty(t *testing.T) {
	if got := MediaResultToBusFiles(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestMediaResultToBusFiles_Converts(t *testing.T) {
	input := []agent.MediaResult{
		{Path: "/a.png", ContentType: "image/png", Size: 1024},
		{Path: "/b.pdf", ContentType: "application/pdf"},
	}
	got := MediaResultToBusFiles(input)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Path != "/a.png" || got[0].MimeType != "image/png" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Path != "/b.pdf" || got[1].MimeType != "application/pdf" {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestBusFilesToMediaResult_Empty(t *testing.T) {
	if got := BusFilesToMediaResult(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestBusFilesToMediaResult_Converts(t *testing.T) {
	input := []bus.MediaFile{
		{Path: "/x.jpg", MimeType: "image/jpeg"},
	}
	got := BusFilesToMediaResult(input)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Path != "/x.jpg" || got[0].ContentType != "image/jpeg" {
		t.Errorf("got[0] = %+v", got[0])
	}
}

func TestRoundTrip_MediaResult(t *testing.T) {
	original := []agent.MediaResult{
		{Path: "/round.wav", ContentType: "audio/wav", Size: 2048, AsVoice: true},
	}
	busFiles := MediaResultToBusFiles(original)
	backToMedia := BusFilesToMediaResult(busFiles)
	// Path and ContentType should survive round-trip
	if backToMedia[0].Path != "/round.wav" || backToMedia[0].ContentType != "audio/wav" {
		t.Errorf("round-trip failed: %+v", backToMedia[0])
	}
	// Size and AsVoice are NOT preserved in bus.MediaFile (by design)
	if backToMedia[0].Size != 0 || backToMedia[0].AsVoice {
		t.Errorf("non-preserved fields should be zero: %+v", backToMedia[0])
	}
}
