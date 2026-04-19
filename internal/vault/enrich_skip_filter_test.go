package vault

import "testing"

// TestShouldSkipEnrichment_FilenamePreservation verifies the skip filter's
// contract against disk-name patterns produced by agent.persistMedia:
//
//	`{slug}-{8hex}{ext}` — preserved filenames; MUST NOT skip (carries meaning).
//	`{uuid}{ext}`        — voice-note / clipboard fallback; MUST skip (noise).
//
// This pairs with internal/agent/media_persist_test.go which validates the
// disk-write side, closing the round-trip: sanitize → persist → enrich passes.
func TestShouldSkipEnrichment_FilenamePreservation(t *testing.T) {
	cases := []struct {
		name     string
		basename string
		want     bool // true = skip
	}{
		// Preserved names — must pass through to enrichment.
		{"vietnamese_slug", "bao-cao-q4-a1b2c3d4.pdf", false},
		{"cjk_preserved", "猫の写真-a1b2c3d4.png", false},
		{"collision_stem_a", "report-11112222.pdf", false},
		{"collision_stem_b", "report-33334444.pdf", false},
		{"short_word_with_hex", "cat-a1b2c3d4.jpg", false},
		{"arabic_stem", "تقرير-a1b2c3d4.pdf", false},
		// Fallbacks — intentionally skipped.
		{"voice_note_uuid", "550e8400-e29b-41d4-a716-446655440000.ogg", true},
		{"clipboard_uuid_png", "00000000-0000-0000-0000-000000000001.png", true},
		// Other pre-existing skip rules must still fire.
		{"goclaw_gen", "goclaw_gen_12345.png", true},
		{"too_short", "ab.pdf", true},
		{"digits_only", "12345.pdf", true},
		{"pure_hex_hash", "a1b2c3d4e5f67890.bin", true},
		{"img_mixed_junk", "IMG_20240101.jpg", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldSkipEnrichment(c.basename)
			if got != c.want {
				t.Fatalf("shouldSkipEnrichment(%q) = %v, want %v", c.basename, got, c.want)
			}
		})
	}
}
