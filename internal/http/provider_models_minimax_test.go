package http

import "testing"

func TestCurrentMiniMaxTextModels(t *testing.T) {
	input := []ModelInfo{
		{ID: "gpt-4o"},
		{ID: "minimax/minimax-m3"},
		{ID: "minimax/minimax-m2.7"},
		{ID: "MiniMax-Text-01"},
		{ID: "MiniMax-M1"},
		{ID: "MiniMax-M2.5-highspeed"},
		{ID: "minimax/minimax-01"},
		{ID: "claude-sonnet-4-6"},
		{ID: "minimax/minimax-m3.1-flash:free"},
		{ID: "minimax/minimax-m3.1-flash"},
		{ID: "minimax/music-2.5"},
		{ID: "MiniMax-Music-2.5"},
		{ID: "minimax/hailuo-02"},
		{ID: "custom-model"},
	}
	want := []string{"gpt-4o", "minimax/minimax-m3.1-flash:free", "claude-sonnet-4-6", "minimax/minimax-m3.1-flash", "minimax/minimax-m3", "minimax/music-2.5", "MiniMax-Music-2.5", "minimax/hailuo-02", "custom-model"}
	got := currentMiniMaxTextModels(input)
	if len(got) != len(want) {
		t.Fatalf("models = %#v, want %v", got, want)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("models[%d] = %q, want %q", i, got[i].ID, id)
		}
	}
	if input[1].ID != "minimax/minimax-m3" {
		t.Fatal("discovery mutated its input")
	}
	if got := currentMiniMaxTextModels([]ModelInfo{{ID: "MiniMax-M2.5"}}); len(got) != 0 {
		t.Fatalf("invented a replacement gateway route: %#v", got)
	}
}
