package telegram

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

func TestChunkTelegramMediaItems_GroupsCompatibleRuns(t *testing.T) {
	items := []telegramMediaSendItem{
		{media: bus.MediaAttachment{URL: "a.jpg"}, kind: telegramMediaPhoto, groupable: true},
		{media: bus.MediaAttachment{URL: "b.mp4"}, kind: telegramMediaVideo, groupable: true},
		{media: bus.MediaAttachment{URL: "c.pdf"}, kind: telegramMediaDocument, groupable: true},
		{media: bus.MediaAttachment{URL: "d.pdf"}, kind: telegramMediaDocument, groupable: true},
		{media: bus.MediaAttachment{URL: "e.mp3"}, kind: telegramMediaAudio, groupable: true},
		{media: bus.MediaAttachment{URL: "f.jpg"}, kind: telegramMediaPhoto, groupable: true},
	}

	chunks := chunkTelegramMediaItems(items)
	if len(chunks) != 4 {
		t.Fatalf("chunks len = %d, want 4", len(chunks))
	}
	assertTelegramChunk(t, chunks[0], true, 2)
	assertTelegramChunk(t, chunks[1], true, 2)
	assertTelegramChunk(t, chunks[2], false, 1)
	assertTelegramChunk(t, chunks[3], false, 1)
}

func TestChunkTelegramMediaItems_SplitsAtTelegramMax(t *testing.T) {
	items := make([]telegramMediaSendItem, 12)
	for i := range items {
		items[i] = telegramMediaSendItem{
			media:     bus.MediaAttachment{URL: "photo.jpg"},
			kind:      telegramMediaPhoto,
			groupable: true,
		}
	}

	chunks := chunkTelegramMediaItems(items)
	if len(chunks) != 2 {
		t.Fatalf("chunks len = %d, want 2", len(chunks))
	}
	assertTelegramChunk(t, chunks[0], true, telegramMediaGroupMaxItems)
	assertTelegramChunk(t, chunks[1], true, 2)
}

func TestChunkTelegramMediaItems_KeepsNonGroupableItemsSeparate(t *testing.T) {
	items := []telegramMediaSendItem{
		{media: bus.MediaAttachment{URL: "voice.ogg"}, kind: telegramMediaAudio, groupable: false},
		{media: bus.MediaAttachment{URL: "a.jpg"}, kind: telegramMediaPhoto, groupable: true},
		{media: bus.MediaAttachment{URL: "b.jpg"}, kind: telegramMediaPhoto, groupable: true},
	}

	chunks := chunkTelegramMediaItems(items)
	if len(chunks) != 2 {
		t.Fatalf("chunks len = %d, want 2", len(chunks))
	}
	assertTelegramChunk(t, chunks[0], false, 1)
	assertTelegramChunk(t, chunks[1], true, 2)
}

func assertTelegramChunk(t *testing.T, chunk telegramMediaChunk, grouped bool, size int) {
	t.Helper()
	if chunk.grouped != grouped {
		t.Fatalf("chunk.grouped = %v, want %v", chunk.grouped, grouped)
	}
	if len(chunk.items) != size {
		t.Fatalf("chunk size = %d, want %d", len(chunk.items), size)
	}
}
