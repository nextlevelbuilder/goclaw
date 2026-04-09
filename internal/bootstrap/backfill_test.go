package bootstrap

import (
	"testing"
)

func TestBackfillTemplateReadable(t *testing.T) {
	tpl, err := ReadTemplate(CapabilitiesFile)
	if err != nil {
		t.Fatal("ReadTemplate failed:", err)
	}
	if len(tpl) == 0 {
		t.Fatal("template is empty")
	}
	t.Logf("template OK: %d bytes", len(tpl))
}
