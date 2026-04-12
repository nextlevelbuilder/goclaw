//go:build sqlite || sqliteonly

package sqlitestore

import "testing"

func TestComputeAttachmentBaseName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"file.md", "file.md"},
		{"path/to/file.md", "file.md"},
		{"/abs/path/to/IMAGE.PNG", "image.png"},
		{"path/to/Mixed-Case.TXT", "mixed-case.txt"},
		{"no-slash-just-name.pdf", "no-slash-just-name.pdf"},
		{"deep/nested/folder/doc.docx", "doc.docx"},
	}
	for _, c := range cases {
		if got := ComputeAttachmentBaseName(c.in); got != c.want {
			t.Errorf("ComputeAttachmentBaseName(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
