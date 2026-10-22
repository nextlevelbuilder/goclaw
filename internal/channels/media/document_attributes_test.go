package media

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractDocumentContentEscapesFilename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(path, []byte("body <with> & symbols"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ordinary.txt", "draft & notes.txt", `quoted "file".txt`, `folder\file.txt`} {
		t.Run(name, func(t *testing.T) {
			content, err := ExtractDocumentContent(path, name)
			if err != nil {
				t.Fatal(err)
			}
			var parsed struct {
				Name string `xml:"name,attr"`
				Body string `xml:",chardata"`
			}
			if err := xml.Unmarshal([]byte(content), &parsed); err != nil {
				t.Fatalf("invalid file context: %v", err)
			}
			if parsed.Name != name {
				t.Errorf("filename = %q, want %q", parsed.Name, name)
			}
			if parsed.Body != "\nbody <with> & symbols\n" {
				t.Errorf("document body = %q", parsed.Body)
			}
		})
	}
}
