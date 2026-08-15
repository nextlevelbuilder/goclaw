package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// The extractive memory fallback exists so a failed or NO_REPLY LLM flush still
// preserves context. Its patterns were English-only, so on a Vietnamese
// deployment it was inert: measured against 195k chars of real Vietnamese
// session text from this host, the English patterns produced 9 decision matches
// (all from stray English words), 0 preferences and 0 technical facts — 97% of
// everything it "saved" was incidental file paths. Live symptom: repeated
// `memory flush: extractive fallback produced no content`, and when it did fire,
// 102 and 161 chars extracted out of a 490k-char session.
func TestExtractiveMemoryFallbackExtractsVietnamese(t *testing.T) {
	t.Parallel()
	msgs := []providers.Message{
		{Role: "user", Content: "Mình cần chốt hạ tầng cho dịch vụ mới."},
		{Role: "assistant", Content: "Nhóm đã quyết định dùng PostgreSQL làm cơ sở dữ liệu chính cho toàn hệ thống."},
		{Role: "user", Content: "Nhớ là đừng deploy vào thứ sáu, để tránh rủi ro cuối tuần."},
		{Role: "assistant", Content: "Ghi nhận. Endpoint là https://api.example.com/v2/health cho việc kiểm tra."},
	}

	result := ExtractiveMemoryFallback(msgs)
	if result == "" {
		t.Fatal("Vietnamese history extracted nothing; the fallback is inert on this locale")
	}
	for _, want := range []string{"Decisions", "PostgreSQL", "User Preferences", "thứ sáu"} {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in extract:\n%s", want, result)
		}
	}
}

// English extraction must not regress: both pattern sets run on every session
// because a Vietnamese conversation routinely quotes English error text and
// config keys, and vice versa.
func TestExtractiveMemoryFallbackStillExtractsEnglish(t *testing.T) {
	t.Parallel()
	msgs := []providers.Message{
		{Role: "assistant", Content: "We decided to use PostgreSQL for the main database"},
		{Role: "user", Content: "I prefer short commit messages, always under 72 chars"},
	}
	result := ExtractiveMemoryFallback(msgs)
	for _, want := range []string{"Decisions", "PostgreSQL", "User Preferences"} {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in extract:\n%s", want, result)
		}
	}
}

// A mixed-language turn is the common real case here: Vietnamese prose around
// an English tool name or error string. Both must survive the same pass.
func TestExtractiveMemoryFallbackHandlesMixedLanguage(t *testing.T) {
	t.Parallel()
	msgs := []providers.Message{
		{Role: "assistant", Content: "Nhóm đã thống nhất chuyển sang pgvector cho phần embedding."},
		{Role: "user", Content: "We decided to use Qdrant for the staging environment only"},
	}
	result := ExtractiveMemoryFallback(msgs)
	if !strings.Contains(result, "pgvector") {
		t.Errorf("Vietnamese decision dropped:\n%s", result)
	}
	if !strings.Contains(result, "Qdrant") {
		t.Errorf("English decision dropped:\n%s", result)
	}
}

// The Vietnamese cues must be decision/preference markers, not bare keywords
// that fire on ordinary prose — otherwise every turn produces noise that
// crowds out the real facts inside the same bounded extract.
func TestExtractiveMemoryFallbackVietnameseCuesAreNotOverEager(t *testing.T) {
	t.Parallel()
	msgs := []providers.Message{
		// "chọn" and "muốn" appear, but as narration rather than a stated
		// decision or preference.
		{Role: "user", Content: "Khách hàng thường so sánh nhiều lựa chọn trước khi mua sản phẩm."},
		{Role: "assistant", Content: "Báo cáo cho thấy người dùng có nhu cầu tìm hiểu thêm về giá."},
	}
	result := ExtractiveMemoryFallback(msgs)
	if strings.Contains(result, "Decisions") {
		t.Errorf("narration was misread as a decision:\n%s", result)
	}
	if strings.Contains(result, "User Preferences") {
		t.Errorf("narration was misread as a preference:\n%s", result)
	}
}
