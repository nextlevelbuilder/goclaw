package skills

import "testing"

func TestValidateVisibility(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty ok (caller defaults)", "", false},
		{"private", "private", false},
		{"internal", "internal", false},
		{"public", "public", false},
		{"uppercase normalized", "PRIVATE", false},
		{"whitespace normalized", "  internal  ", false},
		{"team rejected (v1 scope)", "team", true},
		{"garbage rejected", "nope", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVisibility(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateVisibility(%q) err=%v, wantErr=%v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeVisibility(t *testing.T) {
	cases := map[string]string{
		"":          DefaultVisibility,
		"private":   "private",
		"INTERNAL":  "internal",
		"PUBLIC":    "public",
		"  public ": "public",
	}
	for in, want := range cases {
		if got := NormalizeVisibility(in); got != want {
			t.Errorf("NormalizeVisibility(%q) = %q, want %q", in, got, want)
		}
	}
}
