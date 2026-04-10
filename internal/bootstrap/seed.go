package bootstrap

import (
	"embed"
	"log/slog"
	"os"
	"path/filepath"
)

//go:embed templates/*.md
var templateFS embed.FS

// templateFiles lists the templates to seed, in order.
// BOOTSTRAP.md is handled separately (only seeded for brand-new workspaces).
var templateFiles = []string{
	AgentsFile,
	SoulFile,
	ToolsFile,
	IdentityFile,
	UserFile,
	CapabilitiesFile,
	AgentsCoreFile,
	AgentsTaskFile,
}

// ReadTemplate returns the content of an embedded template file.
func ReadTemplate(name string) (string, error) {
	content, err := templateFS.ReadFile(filepath.Join("templates", name))
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// EnsureWorkspaceFiles seeds template files into a workspace directory.
// Only writes files that don't already exist (will not overwrite).
// BOOTSTRAP.md is only seeded if the workspace is brand new (no AGENTS.md exists).
// Returns the list of files that were created.
func EnsureWorkspaceFiles(workspaceDir string) ([]string, error) {
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return nil, err
	}

	var created []string

	// Check if this is a brand-new workspace (no AGENTS.md yet)
	_, agentsErr := os.Stat(filepath.Join(workspaceDir, AgentsFile))
	isBrandNew := os.IsNotExist(agentsErr)

	// Seed standard template files
	for _, name := range templateFiles {
		ok, err := seedTemplate(workspaceDir, name)
		if err != nil {
			slog.Warn("bootstrap: failed to seed template", "file", name, "error", err)
			continue
		}
		if ok {
			created = append(created, name)
		}
	}

	// Seed BOOTSTRAP.md only for brand-new workspaces
	if isBrandNew {
		ok, err := seedTemplate(workspaceDir, BootstrapFile)
		if err != nil {
			slog.Warn("bootstrap: failed to seed BOOTSTRAP.md", "error", err)
		} else if ok {
			created = append(created, BootstrapFile)
		}
	}

	return created, nil
}

// seedTemplate writes a template file to the workspace if it doesn't exist.
// Returns true if the file was created, false if it already exists.
func seedTemplate(workspaceDir, name string) (bool, error) {
	dstPath := filepath.Join(workspaceDir, name)

	// Only create if file doesn't exist (O_EXCL)
	f, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return false, nil // already exists, skip
		}
		return false, err
	}
	defer f.Close()

	// Read embedded template
	content, err := templateFS.ReadFile(filepath.Join("templates", name))
	if err != nil {
		os.Remove(dstPath) // clean up empty file
		return false, err
	}

	if _, err := f.Write(content); err != nil {
		return false, err
	}

	return true, nil
}
