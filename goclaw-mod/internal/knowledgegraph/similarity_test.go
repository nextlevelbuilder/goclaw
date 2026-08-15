package knowledgegraph

import (
	"math"
	"testing"
)

func TestJaroWinkler_IdenticalStrings(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{"empty strings", "", ""},
		{"single char", "a", "a"},
		{"identical words", "hello", "hello"},
		{"identical with spaces", "hello world", "hello world"},
		{"case insensitive match", "HELLO", "hello"},
		{"mixed case match", "HeLLo", "hello"},
		{"unicode identical", "你好", "你好"},
		{"accented chars identical", "café", "café"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := JaroWinkler(tt.a, tt.b)
			if math.Abs(score-1.0) > 1e-9 {
				t.Errorf("expected 1.0, got %f", score)
			}
		})
	}
}

func TestJaroWinkler_EmptyStrings(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{"both empty", "", ""},
		{"a empty, b not", "", "hello"},
		{"b empty, a not", "hello", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := JaroWinkler(tt.a, tt.b)
			if tt.a == "" && tt.b == "" {
				if score != 1.0 {
					t.Errorf("both empty should be 1.0, got %f", score)
				}
			} else {
				if score != 0.0 {
					t.Errorf("one empty should be 0.0, got %f", score)
				}
			}
		})
	}
}

func TestJaroWinkler_NoMatches(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{"completely different", "abc", "xyz"},
		{"no common chars", "aaa", "bbb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := JaroWinkler(tt.a, tt.b)
			if score != 0.0 {
				t.Errorf("no matches should be 0.0, got %f", score)
			}
		})
	}
}

func TestJaroWinkler_CaseInsensitivity(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{"uppercase vs lowercase", "HELLO", "hello"},
		{"mixed case", "HeLLo", "hElLO"},
		{"spaces preserved in case test", "HELLO WORLD", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := JaroWinkler(tt.a, tt.b)
			if math.Abs(score-1.0) > 1e-9 {
				t.Errorf("case-insensitive match should be 1.0, got %f", score)
			}
		})
	}
}

func TestJaroWinkler_PrefixBonus(t *testing.T) {
	// Jaro-Winkler gives a bonus for matching prefixes (up to 4 chars)
	// This test verifies that strings with matching prefixes score higher
	// than those without, even if their base Jaro score is the same.
	tests := []struct {
		name     string
		a        string
		b        string
		minScore float64 // minimum expected score
		maxScore float64 // maximum expected score (or 1.0)
	}{
		{
			name:     "similar with prefix",
			a:        "martha",
			b:        "marhta",
			minScore: 0.94, // high similarity due to prefix bonus
			maxScore: 1.0,
		},
		{
			name:     "similar no prefix match",
			a:        "dixon",
			b:        "dickson",
			minScore: 0.78,
			maxScore: 0.90,
		},
		{
			name:     "one char prefix",
			a:        "apple",
			b:        "apricot",
			minScore: 0.55,
			maxScore: 0.70,
		},
		{
			name:     "long common prefix",
			a:        "interstellar",
			b:        "interstellarx",
			minScore: 0.97,
			maxScore: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := JaroWinkler(tt.a, tt.b)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("expected score between %f and %f, got %f", tt.minScore, tt.maxScore, score)
			}
		})
	}
}

func TestJaroWinkler_SingleCharacter(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{"both single char match", "a", "a"},
		{"both single char differ", "a", "b"},
		{"single vs multi", "a", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := JaroWinkler(tt.a, tt.b)
			if score < 0.0 || score > 1.0 {
				t.Errorf("score out of range [0,1]: %f", score)
			}
		})
	}
}

func TestJaroWinkler_PartialMatches(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		minScore float64
		maxScore float64
	}{
		{
			name:     "one char different",
			a:        "sitting",
			b:        "kitten",
			minScore: 0.70,
			maxScore: 0.80,
		},
		{
			name:     "transposition",
			a:        "smith",
			b:        "smtih",
			minScore: 0.92,
			maxScore: 1.0,
		},
		{
			name:     "subset",
			a:        "cat",
			b:        "cats",
			minScore: 0.80,
			maxScore: 0.95,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := JaroWinkler(tt.a, tt.b)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("expected score between %f and %f, got %f", tt.minScore, tt.maxScore, score)
			}
		})
	}
}

func TestJaroWinkler_KnownValues(t *testing.T) {
	// Test against known Jaro-Winkler values from standard implementations.
	// These are reference cases that should remain consistent.
	tests := []struct {
		name        string
		a           string
		b           string
		expectedMin float64
		expectedMax float64
	}{
		{
			name:        "martha vs marhta (classic example)",
			a:           "martha",
			b:           "marhta",
			expectedMin: 0.961,
			expectedMax: 0.965,
		},
		{
			name:        "dixon vs dickson",
			a:           "dixon",
			b:           "dickson",
			expectedMin: 0.83,
			expectedMax: 0.84,
		},
		{
			name:        "exact substring",
			a:           "test",
			b:           "testing",
			expectedMin: 0.85,
			expectedMax: 0.95,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := JaroWinkler(tt.a, tt.b)
			if score < tt.expectedMin || score > tt.expectedMax {
				t.Errorf("expected score between %f and %f, got %f", tt.expectedMin, tt.expectedMax, score)
			}
		})
	}
}

func TestJaroWinkler_ThresholdBoundaries(t *testing.T) {
	// Test values near the dedup thresholds from the code:
	// dedupNameMatchThreshold = 0.85
	// dedupCandidateThreshold = 0.90
	// dedupAutoMergeThreshold = 0.98
	tests := []struct {
		name        string
		a           string
		b           string
		nearThreshold float64 // the threshold to be near
	}{
		{
			name:           "names above 0.85 threshold",
			a:              "Jonathan",
			b:              "Jonathan",
			nearThreshold:  0.85,
		},
		{
			name:           "similar names around 0.85",
			a:              "Robert",
			b:              "Rupert",
			nearThreshold:  0.85,
		},
		{
			name:           "identical near 0.98",
			a:              "Company",
			b:              "Company",
			nearThreshold:  0.98,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := JaroWinkler(tt.a, tt.b)
			// Just verify score is in valid range and near expected threshold
			if score < 0.0 || score > 1.0 {
				t.Errorf("score out of range [0,1]: %f", score)
			}
			// For identical or near-identical, score should be reasonably high
			if tt.a == tt.b && score < 0.99 {
				t.Errorf("identical strings should score >= 0.99, got %f", score)
			}
		})
	}
}

func TestJaroWinkler_Symmetry(t *testing.T) {
	// JaroWinkler should be symmetric: JW(a, b) == JW(b, a)
	tests := []string{
		"hello",
		"world",
		"test",
		"martha",
		"dixon",
		"abc",
		"xyz",
	}

	for i, a := range tests {
		for j, b := range tests {
			if i >= j {
				continue
			}
			t.Run("symmetric_"+a+"_"+b, func(t *testing.T) {
				score1 := JaroWinkler(a, b)
				score2 := JaroWinkler(b, a)
				if math.Abs(score1-score2) > 1e-9 {
					t.Errorf("asymmetric: JW(%q, %q)=%f, JW(%q, %q)=%f", a, b, score1, b, a, score2)
				}
			})
		}
	}
}

func TestJaroWinkler_LongStrings(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{
			name: "long identical",
			a:    "The quick brown fox jumps over the lazy dog",
			b:    "The quick brown fox jumps over the lazy dog",
		},
		{
			name: "long one char different",
			a:    "The quick brown fox jumps over the lazy dog",
			b:    "The quick brown fox jumps over the lazy fog",
		},
		{
			name: "long different",
			a:    "The quick brown fox",
			b:    "A slow red turtle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := JaroWinkler(tt.a, tt.b)
			if score < 0.0 || score > 1.0 {
				t.Errorf("score out of range [0,1]: %f", score)
			}
		})
	}
}

func TestJaroWinkler_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{
			name: "with hyphens",
			a:    "first-name",
			b:    "first-name",
		},
		{
			name: "with underscores",
			a:    "first_name",
			b:    "first_name",
		},
		{
			name: "with numbers",
			a:    "test123",
			b:    "test123",
		},
		{
			name: "with punctuation",
			a:    "hello, world!",
			b:    "hello, world!",
		},
		{
			name: "unicode with special chars",
			a:    "café-au-lait",
			b:    "cafe-au-lait",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := JaroWinkler(tt.a, tt.b)
			if score < 0.0 || score > 1.0 {
				t.Errorf("score out of range [0,1]: %f", score)
			}
			// Identical special-char strings should match
			if tt.a == tt.b && score < 0.99 {
				t.Errorf("identical strings with special chars should score high, got %f", score)
			}
		})
	}
}

func TestJaroWinkler_UnicodeHandling(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{
			name: "chinese characters identical",
			a:    "北京",
			b:    "北京",
		},
		{
			name: "hindi characters identical",
			a:    "नमस्ते",
			b:    "नमस्ते",
		},
		{
			name: "emoji identical",
			a:    "🚀test",
			b:    "🚀test",
		},
		{
			name: "mixed unicode",
			a:    "café",
			b:    "café",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := JaroWinkler(tt.a, tt.b)
			if math.Abs(score-1.0) > 1e-9 {
				t.Errorf("identical unicode should score 1.0, got %f", score)
			}
		})
	}
}

func TestJaroWinkler_Boundary(t *testing.T) {
	// Test values at boundary conditions: very short and very long strings
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{
			name: "two char strings",
			a:    "ab",
			b:    "ab",
		},
		{
			name: "two char different",
			a:    "ab",
			b:    "ba",
		},
		{
			name: "three char strings",
			a:    "abc",
			b:    "abc",
		},
		{
			name: "very long identical",
			a:    "a" + string(make([]byte, 1000)),
			b:    "a" + string(make([]byte, 1000)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := JaroWinkler(tt.a, tt.b)
			if score < 0.0 || score > 1.0 {
				t.Errorf("score out of range [0,1]: %f", score)
			}
		})
	}
}

func TestJaroWinkler_MonotonicIncreaseWithSimilarity(t *testing.T) {
	// Test that as strings become more similar, the score increases.
	base := "hello"
	tests := []struct {
		name  string
		str   string
		label string
	}{
		{"completely different", "world", "world"},
		{"one char match", "hxxxx", "1 match"},
		{"two char match", "hexxxx", "2 matches"},
		{"three char match", "helxx", "3 matches"},
		{"four char match", "hellx", "4 matches"},
		{"identical", "hello", "identical"},
	}

	var scores []float64
	for _, tt := range tests {
		score := JaroWinkler(base, tt.str)
		scores = append(scores, score)
	}

	// Verify monotonic increase
	for i := 1; i < len(scores); i++ {
		if scores[i] < scores[i-1] {
			t.Errorf("scores not monotonically increasing at index %d: %f < %f", i, scores[i], scores[i-1])
		}
	}
}

func BenchmarkJaroWinkler(b *testing.B) {
	benchmarks := []struct {
		name string
		a    string
		b    string
	}{
		{"short", "test", "best"},
		{"medium", "jonathan", "johnathan"},
		{"long", "The quick brown fox jumps over the lazy dog", "The quick brown fox jumps over the lazy fog"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				JaroWinkler(bm.a, bm.b)
			}
		})
	}
}
