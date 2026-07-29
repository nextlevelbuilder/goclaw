package agent

import "testing"

// generate_image / generate_video are EXEMPT from the same-result breaker
// (#434): document-mcp already rate-limits them with a per-user cooldown that
// returns an identical "already generated" notice by design, so counting those
// repeats as a runaway loop appended a scary CRITICAL notice to an otherwise
// successful turn. The cooldown — not the breaker — is the bound here. (This
// supersedes the earlier #220 attempt to bound it via the breaker.)
func TestGenerateImageExemptFromSameResultBreaker(t *testing.T) {
	const tool = "mcp_document_mcp__generate_image"
	const result = "Image generation already completed for this request; the generated image is attached to the chat. No additional image was produced."

	var s toolLoopState
	rh := hashResult(result)

	// Even at 3+ identical results (different args each time), the breaker must
	// stay silent for generate_image — no warning, no critical.
	for i := range 4 {
		h := s.record(tool, map[string]any{"prompt": "green elephant", "variant": i})
		s.recordResult(h, result)
		if level, _ := s.detectSameResult(tool, rh); level != "" {
			t.Fatalf("generate_image must stay exempt from the same-result breaker, got level=%q after %d calls", level, i+1)
		}
	}
}

// refresh_page_content legitimately returns the same snapshot; it must remain
// exempt from the same-result breaker.
func TestRefreshPageContentStaysExempt(t *testing.T) {
	const tool = "refresh_page_content"
	const result = "<html>unchanged</html>"

	var s toolLoopState
	for i := range 5 {
		h := s.record(tool, map[string]any{"selector": i})
		s.recordResult(h, result)
	}
	if level, _ := s.detectSameResult(tool, hashResult(result)); level != "" {
		t.Fatalf("refresh_page_content must stay exempt, got level=%q", level)
	}
}
