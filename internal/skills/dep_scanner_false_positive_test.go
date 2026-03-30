package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanFile_JSImportInsidePythonFString(t *testing.T) {
	dir := t.TempDir()
	pyFile := filepath.Join(dir, "render.py")

	// Python file with JS ES module import inside f-string (issue #544)
	content := `#!/usr/bin/env python3
import sys
import json

def render_html(text):
    mermaid_init = f"""
<script type="module">
    import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs';
    mermaid.initialize({{ startOnLoad: true }});
</script>
"""
    return f"<html>{text}{mermaid_init}</html>"
`
	if err := os.WriteFile(pyFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pyImports := make(map[string]bool)
	nodeImports := make(map[string]bool)
	binaries := make(map[string]bool)

	scanFile(pyFile, pyImports, nodeImports, binaries)

	// sys and json are real Python imports — should be detected
	if !pyImports["sys"] {
		t.Error("expected sys to be detected as Python import")
	}
	if !pyImports["json"] {
		t.Error("expected json to be detected as Python import")
	}

	// mermaid is a JS import inside f-string — should NOT be detected
	if pyImports["mermaid"] {
		t.Error("FALSE POSITIVE: mermaid detected as Python import — it's a JS import inside f-string")
	}
}

func TestScanFile_MultipleJSImportsInsidePythonString(t *testing.T) {
	dir := t.TempDir()
	pyFile := filepath.Join(dir, "template.py")

	// Multiple JS imports inside a Python string + real Python imports
	content := `import os
import subprocess

TEMPLATE = """
<script type="module">
    import React from 'https://cdn.example.com/react.js';
    import lodash from 'https://cdn.example.com/lodash.js';
</script>
"""
`
	if err := os.WriteFile(pyFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pyImports := make(map[string]bool)
	nodeImports := make(map[string]bool)
	binaries := make(map[string]bool)

	scanFile(pyFile, pyImports, nodeImports, binaries)

	// Real Python imports
	if !pyImports["os"] {
		t.Error("expected os to be detected as Python import")
	}
	if !pyImports["subprocess"] {
		t.Error("expected subprocess to be detected as Python import")
	}

	// JS imports inside string — should NOT be detected
	if pyImports["React"] {
		t.Error("FALSE POSITIVE: React detected as Python import")
	}
	if pyImports["lodash"] {
		t.Error("FALSE POSITIVE: lodash detected as Python import")
	}
}
