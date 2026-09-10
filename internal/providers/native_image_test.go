package providers

import (
	"strings"
	"testing"
)

func TestValidateImageModel(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty falls back to default", input: "", want: DefaultImageModel},
		{name: "flare", input: "gpt-image-2.5-flare", want: "gpt-image-2.5-flare"},
		{name: "sunburst", input: "gpt-image-2.5-sunburst", want: "gpt-image-2.5-sunburst"},
		{name: "previous generation", input: "gpt-image-2", want: "gpt-image-2"},
		{name: "legacy", input: "gpt-image-1.5", want: "gpt-image-1.5"},
		{name: "unversioned 2.5 is not a real model id", input: "gpt-image-2.5", wantErr: true},
		{name: "unrelated model", input: "dall-e-3", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateImageModel(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateImageModel(%q) = %q, want error", tc.input, got)
				}
				// The error must list the allowed models so the caller can self-correct.
				if !strings.Contains(err.Error(), DefaultImageModel) {
					t.Errorf("error %q does not mention the default model %q", err, DefaultImageModel)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateImageModel(%q) returned error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ValidateImageModel(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestDefaultImageModelIsAllowed guards against the default drifting out of the
// whitelist, which would make every create_image call with no explicit model fail.
func TestDefaultImageModelIsAllowed(t *testing.T) {
	if !allowedImageModels[DefaultImageModel] {
		t.Fatalf("DefaultImageModel %q is missing from allowedImageModels", DefaultImageModel)
	}
}
