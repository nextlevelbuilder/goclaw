package skills

import (
	"os"
	"path/filepath"
	"slices"
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

func TestScanScriptsDir_FiltersStdlib(t *testing.T) {
	scriptsDir := filepath.Join(t.TempDir(), "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Script imports stdlib modules + one real pip dep
	content := `import sys
import os
import json
import argparse
import subprocess
from pathlib import Path
from datetime import datetime

import requests
from PIL import Image
`
	if err := os.WriteFile(filepath.Join(scriptsDir, "main.py"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	m := scanScriptsDir(scriptsDir)

	// Only real pip deps should appear in RequiresPython — NOT stdlib.
	for _, pkg := range m.RequiresPython {
		if pythonStdlib[pkg] {
			t.Errorf("stdlib module %q should have been filtered from RequiresPython", pkg)
		}
	}

	// Real deps must be present.
	if !slices.Contains(m.RequiresPython, "requests") {
		t.Error("expected 'requests' in RequiresPython")
	}
	if !slices.Contains(m.RequiresPython, "PIL") {
		t.Error("expected 'PIL' in RequiresPython")
	}

	// Stdlib must NOT be present.
	for _, stdlib := range []string{"sys", "os", "json", "argparse", "subprocess", "pathlib", "datetime"} {
		if slices.Contains(m.RequiresPython, stdlib) {
			t.Errorf("stdlib %q should NOT be in RequiresPython", stdlib)
		}
	}
}
