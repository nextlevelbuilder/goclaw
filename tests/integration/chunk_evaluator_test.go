//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/memory"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestChunkEvaluator_Integration tests the full evaluation → chunk → store pipeline
// with real PostgreSQL. Inserts raw messages, evaluates chunk params, chunks text,
// stores chunks with dummy embeddings, verifies DB state, then marks messages embedded.
func TestChunkEvaluator_Integration(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	rawStore := pg.NewPGListenRawMessageStore(db)
	chunkStore := pg.NewPGRawMessageChunkStore(db)

	graphID := "test-graph-" + uuid.New().String()[:8]
	chatID := "test-chat-" + uuid.New().String()[:8]

	// Seed 25 raw messages (enough for meaningful P95 stats).
	msgs := make([]store.ListenRawMessage, 25)
	for i := range msgs {
		msgs[i] = store.ListenRawMessage{
			ChannelName:  "whatsapp",
			ChatID:       chatID,
			ChatName:     "Test Group",
			GraphID:      graphID,
			Sender:       fmt.Sprintf("User %d", i%3),
			SenderID:     fmt.Sprintf("sender_%d", i%3),
			Body:         fmt.Sprintf("Pesan test nomor %d dari percakapan grup. Ini adalah pesan yang cukup panjang untuk evaluasi chunk.", i+1),
			MsgTimestamp: time.Now().Add(time.Duration(i) * time.Minute),
			AgentID:      agentID.String(),
			TenantID:     tenantID,
		}
	}

	if err := rawStore.AppendBatch(ctx, msgs); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}

	// Verify messages are fetchable as pending embeddings.
	pending, err := rawStore.ListPendingEmbeddings(ctx, agentID.String(), graphID, 50)
	if err != nil {
		t.Fatalf("ListPendingEmbeddings: %v", err)
	}
	if len(pending) != 25 {
		t.Fatalf("pending count = %d, want 25", len(pending))
	}

	// Evaluate chunk params from actual message content.
	bodies := make([]string, len(pending))
	for i, m := range pending {
		bodies[i] = m.Body
	}
	stats := memory.EvaluateRawMessages(bodies)

	if stats.TotalMsgs != 25 {
		t.Errorf("TotalMsgs = %d, want 25", stats.TotalMsgs)
	}
	if stats.AvgBodyLen == 0 {
		t.Error("AvgBodyLen = 0, want > 0")
	}
	if stats.P95BodyLen == 0 {
		t.Error("P95BodyLen = 0, want > 0")
	}
	t.Logf("stats: avg=%d max=%d p95=%d total=%d",
		stats.AvgBodyLen, stats.MaxBodyLen, stats.P95BodyLen, stats.TotalMsgs)

	// Evaluate chunk params for embeddinggemma model.
	maxChunkLen, chunkOverlap := memory.EvaluateChunkParams("embeddinggemma-300m", stats)

	// embeddinggemma: 8192 * 0.85 * 4 = 27852
	if maxChunkLen != 27852 {
		t.Errorf("maxChunkLen = %d, want 27852", maxChunkLen)
	}
	if chunkOverlap != 27852/5 {
		t.Errorf("chunkOverlap = %d, want %d", chunkOverlap, 27852/5)
	}
	t.Logf("chunk params: maxChunkLen=%d chunkOverlap=%d", maxChunkLen, chunkOverlap)

	// Build text from messages and chunk.
	chatText := buildTestEmbeddingText(pending)
	chunks := memory.ChunkText(chatText, maxChunkLen, chunkOverlap)

	if len(chunks) == 0 {
		t.Fatal("ChunkText returned 0 chunks")
	}
	// With 25 short messages (~100 chars each = ~2500 total), all should fit in one chunk
	// since maxChunkLen is 27852.
	t.Logf("produced %d chunk(s) from %d messages", len(chunks), len(pending))

	// Create dummy embeddings (768 dims) and store chunks.
	dummyEmb := make([]float32, 768)
	for i := range dummyEmb {
		dummyEmb[i] = 0.01
	}

	storeChunks := make([]store.RawMessageChunk, len(chunks))
	embeddings := make([][]float32, len(chunks))
	timeFrom := pending[0].MsgTimestamp
	timeTo := pending[len(pending)-1].MsgTimestamp

	for i, chunk := range chunks {
		storeChunks[i] = store.RawMessageChunk{
			AgentID:      agentID.String(),
			GraphID:      graphID,
			ChatID:       chatID,
			ChatName:     "Test Group",
			Sender:       "User 0, User 1, User 2",
			SenderID:     "sender_0",
			MsgTimeFrom:  timeFrom,
			MsgTimeTo:    timeTo,
			ChunkIndex:   i,
			Text:         chunk.Text,
			ContentHash:  memory.ContentHash(chunk.Text),
			SourceMsgIDs: msgIDs(pending),
		}
		embeddings[i] = dummyEmb
	}

	if err := chunkStore.StoreChunks(ctx, storeChunks, embeddings); err != nil {
		t.Fatalf("StoreChunks: %v", err)
	}

	// Verify chunks stored.
	listOpts := store.RawMessageChunkListOpts{
		AgentID: agentID.String(),
		GraphID: graphID,
		Limit:   50,
	}
	stored, total, err := chunkStore.List(ctx, listOpts)
	if err != nil {
		t.Fatalf("List chunks: %v", err)
	}
	if total != len(chunks) {
		t.Errorf("stored chunk count = %d, want %d", total, len(chunks))
	}
	for _, c := range stored {
		if !c.HasEmbedding {
			t.Errorf("chunk %s has no embedding", c.ID)
		}
	}
	t.Logf("verified %d chunks stored with embeddings", len(stored))

	// Mark messages embedded.
	ids := make([]uuid.UUID, len(pending))
	for i, m := range pending {
		ids[i] = m.ID
	}
	if err := rawStore.MarkEmbedded(ctx, ids); err != nil {
		t.Fatalf("MarkEmbedded: %v", err)
	}

	// Verify no more pending.
	pendingAfter, err := rawStore.ListPendingEmbeddings(ctx, agentID.String(), graphID, 50)
	if err != nil {
		t.Fatalf("ListPendingEmbeddings after mark: %v", err)
	}
	if len(pendingAfter) != 0 {
		t.Errorf("pending after mark = %d, want 0", len(pendingAfter))
	}

	// Verify embedding stats.
	pendingCount, embeddedCount, err := rawStore.EmbeddingStats(ctx)
	if err != nil {
		t.Fatalf("EmbeddingStats: %v", err)
	}
	if pendingCount != 0 {
		t.Errorf("pending = %d, want 0", pendingCount)
	}
	if embeddedCount != 25 {
		t.Errorf("embedded = %d, want 25", embeddedCount)
	}
	t.Logf("final stats: pending=%d embedded=%d", pendingCount, embeddedCount)
}

// TestChunkEvaluator_SmallBatch tests that small batches still produce valid chunks
// using model-only defaults.
func TestChunkEvaluator_SmallBatch(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	rawStore := pg.NewPGListenRawMessageStore(db)
	chunkStore := pg.NewPGRawMessageChunkStore(db)

	graphID := "small-graph-" + uuid.New().String()[:8]
	chatID := "small-chat-" + uuid.New().String()[:8]

	// Only 3 messages — below typical batch thresholds.
	msgs := []store.ListenRawMessage{
		{ChannelName: "whatsapp", ChatID: chatID, ChatName: "Small", GraphID: graphID,
			Sender: "Alice", SenderID: "s1", Body: "Halo, apa kabar?",
			MsgTimestamp: time.Now(), AgentID: agentID.String(), TenantID: tenantID},
		{ChannelName: "whatsapp", ChatID: chatID, ChatName: "Small", GraphID: graphID,
			Sender: "Bob", SenderID: "s2", Body: "Baik, terima kasih!",
			MsgTimestamp: time.Now().Add(time.Minute), AgentID: agentID.String(), TenantID: tenantID},
		{ChannelName: "whatsapp", ChatID: chatID, ChatName: "Small", GraphID: graphID,
			Sender: "Alice", SenderID: "s1", Body: "Sampai jumpa besok ya",
			MsgTimestamp: time.Now().Add(2 * time.Minute), AgentID: agentID.String(), TenantID: tenantID},
	}

	if err := rawStore.AppendBatch(ctx, msgs); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}

	pending, err := rawStore.ListPendingEmbeddings(ctx, agentID.String(), graphID, 50)
	if err != nil {
		t.Fatalf("ListPendingEmbeddings: %v", err)
	}
	if len(pending) != 3 {
		t.Fatalf("pending = %d, want 3", len(pending))
	}

	// Evaluate — even 3 messages should produce valid stats.
	bodies := make([]string, len(pending))
	for i, m := range pending {
		bodies[i] = m.Body
	}
	stats := memory.EvaluateRawMessages(bodies)
	if stats.TotalMsgs != 3 {
		t.Errorf("TotalMsgs = %d, want 3", stats.TotalMsgs)
	}

	maxChunkLen, chunkOverlap := memory.EvaluateChunkParams("embeddinggemma-300m", stats)
	if maxChunkLen <= 0 || chunkOverlap <= 0 {
		t.Errorf("params = (%d, %d), want both > 0", maxChunkLen, chunkOverlap)
	}

	chatText := buildTestEmbeddingText(pending)
	chunks := memory.ChunkText(chatText, maxChunkLen, chunkOverlap)
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk from small batch")
	}

	// Store with dummy embeddings.
	dummyEmb := make([][]float32, len(chunks))
	for i := range dummyEmb {
		dummyEmb[i] = make([]float32, 768)
	}
	storeChunks := make([]store.RawMessageChunk, len(chunks))
	timeFrom := pending[0].MsgTimestamp
	timeTo := pending[len(pending)-1].MsgTimestamp
	for i, c := range chunks {
		storeChunks[i] = store.RawMessageChunk{
			AgentID: agentID.String(), GraphID: graphID, ChatID: chatID,
			ChatName: "Small", Sender: "Alice, Bob", SenderID: "s1",
			MsgTimeFrom: timeFrom, MsgTimeTo: timeTo, ChunkIndex: i,
			Text: c.Text, ContentHash: memory.ContentHash(c.Text),
			SourceMsgIDs: msgIDs(pending),
		}
	}
	if err := chunkStore.StoreChunks(ctx, storeChunks, dummyEmb); err != nil {
		t.Fatalf("StoreChunks: %v", err)
	}

	// Verify.
	_, total, err := chunkStore.List(ctx, store.RawMessageChunkListOpts{
		AgentID: agentID.String(), GraphID: graphID, Limit: 10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != len(chunks) {
		t.Errorf("total chunks = %d, want %d", total, len(chunks))
	}
	t.Logf("small batch: %d messages → %d chunk(s), maxChunkLen=%d", len(pending), len(chunks), maxChunkLen)
}

// TestChunkEvaluator_MultiBatch runs several batches with different message patterns
// to verify the evaluator produces appropriate chunk params across scenarios.
func TestChunkEvaluator_MultiBatch(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	rawStore := pg.NewPGListenRawMessageStore(db)
	chunkStore := pg.NewPGRawMessageChunkStore(db)

	scenarios := []struct {
		name       string
		genBody    func(i int) string
		count      int
		wantMin    int // minimum expected chunks
		wantMax    int // maximum expected chunks
	}{
		{
			name: "short_indonesian",
			genBody: func(i int) string {
				shortcuts := []string{
					"oke siap",
					"iya betul",
					"nanti ya",
					"sip",
					"udah liat belum?",
					"belom",
					"mantap",
					"wah keren",
					"besok aja",
					"gas",
				}
				return shortcuts[i%len(shortcuts)]
			},
			count:   50,
			wantMin: 1,
			wantMax: 1,
		},
		{
			name: "medium_indonesian",
			genBody: func(i int) string {
				return fmt.Sprintf("Halo semua, ini pesan nomor %d dari percakapan grup. Mohon dibaca ya, karena penting untuk koordinasi besok. Terima kasih.", i+1)
			},
			count:   100,
			wantMin: 1,
			wantMax: 2,
		},
		{
			name: "long_paragraphs",
			genBody: func(i int) string {
				return fmt.Sprintf("Pesan panjang nomor %d. Ini adalah contoh pesan yang sangat panjang seperti yang sering ditemukan dalam grup WhatsApp ketika seseorang meneruskan pesan panjang atau menulis paragraf lengkap. Pesan ini sengaja dibuat panjang untuk menguji bagaimana evaluator menangani teks yang lebih panjang dari rata-rata. Dalam praktiknya, pesan seperti ini muncul ketika seseorang membagikan pengumuman, berita, atau instruksi detail kepada anggota grup. Kami ingin memastikan bahwa chunking tetap optimal bahkan ketika pesan individual sudah cukup panjang.", i+1)
			},
			count:   30,
			wantMin: 1,
			wantMax: 5,
		},
		{
			name: "mixed_short_long",
			genBody: func(i int) string {
				if i%5 == 0 {
					return "Pengumuman penting untuk semua anggota grup. Mohon perhatikan bahwa mulai besok akan ada perubahan jadwal meeting yang biasa kita lakukan setiap hari Senin. Silakan cek kalender yang sudah diupdate untuk detail lengkapnya. Terima kasih atas perhatiannya."
				}
				return "siap"
			},
			count:   80,
			wantMin: 1,
			wantMax: 2,
		},
		{
			name: "high_volume_short",
			genBody: func(i int) string {
				return fmt.Sprintf("msg %d", i+1)
			},
			count:   200,
			wantMin: 1,
			wantMax: 1,
		},
		{
			name: "single_long_text",
			genBody: func(i int) string {
				// ~3000 chars per message
				base := "Ini adalah teks yang sangat panjang yang dirancang untuk menguji batas atas dari evaluator chunk. "
				repeat := 3000 / len(base)
				body := strings.Repeat(base, repeat+1)
				return fmt.Sprintf("[%d] %s", i+1, body[:3000])
			},
			count:   10,
			wantMin: 1,
			wantMax: 5,
		},
	}

	models := []string{"embeddinggemma-300m", "text-embedding-3-small"}

	for _, model := range models {
		t.Run("model_"+model, func(t *testing.T) {
			for _, sc := range scenarios {
				t.Run(sc.name, func(t *testing.T) {
					graphID := "mb-" + sc.name + "-" + uuid.New().String()[:8]
					chatID := "chat-" + sc.name + "-" + uuid.New().String()[:8]

					// Generate messages.
					msgs := make([]store.ListenRawMessage, sc.count)
					for i := range msgs {
						msgs[i] = store.ListenRawMessage{
							ChannelName:  "whatsapp",
							ChatID:       chatID,
							ChatName:     "Batch Test " + sc.name,
							GraphID:      graphID,
							Sender:       fmt.Sprintf("Sender %d", i%4),
							SenderID:     fmt.Sprintf("s_%d", i%4),
							Body:         sc.genBody(i),
							MsgTimestamp: time.Now().Add(time.Duration(i) * time.Second),
							AgentID:      agentID.String(),
							TenantID:     tenantID,
						}
					}

					// Insert.
					if err := rawStore.AppendBatch(ctx, msgs); err != nil {
						t.Fatalf("AppendBatch: %v", err)
					}

					// Fetch pending.
					pending, err := rawStore.ListPendingEmbeddings(ctx, agentID.String(), graphID, 500)
					if err != nil {
						t.Fatalf("ListPendingEmbeddings: %v", err)
					}

					// Evaluate.
					bodies := make([]string, len(pending))
					for i, m := range pending {
						bodies[i] = m.Body
					}
					stats := memory.EvaluateRawMessages(bodies)
					maxChunkLen, chunkOverlap := memory.EvaluateChunkParams(model, stats)

					// Chunk.
					chatText := buildTestEmbeddingText(pending)
					chunks := memory.ChunkText(chatText, maxChunkLen, chunkOverlap)
					totalChars := len(chatText)

					t.Logf("scenario=%s model=%s msgs=%d totalChars=%d avgLen=%d p95=%d maxLen=%d → maxChunkLen=%d overlap=%d → chunks=%d",
						sc.name, model, stats.TotalMsgs, totalChars,
						stats.AvgBodyLen, stats.P95BodyLen, stats.MaxBodyLen,
						maxChunkLen, chunkOverlap, len(chunks))

					if len(chunks) < sc.wantMin {
						t.Errorf("chunks=%d, want >= %d", len(chunks), sc.wantMin)
					}
					if len(chunks) > sc.wantMax {
						t.Errorf("chunks=%d, want <= %d", len(chunks), sc.wantMax)
					}
					if len(chunks) == 0 {
						t.Error("expected at least 1 chunk")
					}

					// Store with dummy embeddings.
					dummyEmb := make([][]float32, len(chunks))
					for i := range dummyEmb {
						dummyEmb[i] = make([]float32, 768)
					}
					storeChunks := make([]store.RawMessageChunk, len(chunks))
					timeFrom := pending[0].MsgTimestamp
					timeTo := pending[len(pending)-1].MsgTimestamp
					for i, c := range chunks {
						storeChunks[i] = store.RawMessageChunk{
							AgentID: agentID.String(), GraphID: graphID, ChatID: chatID,
							ChatName: "Batch Test " + sc.name, Sender: "Sender 0",
							SenderID: "s_0", MsgTimeFrom: timeFrom, MsgTimeTo: timeTo,
							ChunkIndex: i, Text: c.Text,
							ContentHash:  memory.ContentHash(c.Text),
							SourceMsgIDs: msgIDs(pending),
						}
					}
					if err := chunkStore.StoreChunks(ctx, storeChunks, dummyEmb); err != nil {
						t.Fatalf("StoreChunks: %v", err)
					}

					// Mark embedded.
					ids := make([]uuid.UUID, len(pending))
					for i, m := range pending {
						ids[i] = m.ID
					}
					if err := rawStore.MarkEmbedded(ctx, ids); err != nil {
						t.Fatalf("MarkEmbedded: %v", err)
					}

					// Verify no pending remain for this graph.
					pendingAfter, err := rawStore.ListPendingEmbeddings(ctx, agentID.String(), graphID, 500)
					if err != nil {
						t.Fatalf("ListPendingEmbeddings after mark: %v", err)
					}
					if len(pendingAfter) != 0 {
						t.Errorf("pending after mark=%d, want 0", len(pendingAfter))
					}

					// Verify chunks stored for this graph.
					_, chunkTotal, err := chunkStore.List(ctx, store.RawMessageChunkListOpts{
						AgentID: agentID.String(), GraphID: graphID, Limit: 500,
					})
					if err != nil {
						t.Fatalf("List chunks: %v", err)
					}
					if chunkTotal != len(chunks) {
						t.Errorf("stored chunks=%d, want %d", chunkTotal, len(chunks))
					}
				})
			}
		})
	}
}

func buildTestEmbeddingText(msgs []store.ListenRawMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "[%s] %s: %s\n", m.MsgTimestamp.Format("2006-01-02 15:04:05"), m.Sender, m.Body)
	}
	return b.String()
}

func msgIDs(msgs []store.ListenRawMessage) []string {
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID.String()
	}
	return ids
}
