// Package bootstrap loads workspace persona/context files and injects them
// into the agent's system prompt. Matching TS agents/workspace.ts + bootstrap-files.ts.
//
// Bootstrap files are loaded from the workspace directory at startup:
//
//	AGENTS.md  — operating instructions (every session)
//	SOUL.md    — persona, tone, boundaries
//	USER.md    — user profile
//	IDENTITY.md— agent name, emoji, creature, vibe
//	TOOLS.md   — local tool notes
//	BOOTSTRAP.md— first-run ritual (deleted after completion)
//	MEMORY.md  — long-term curated memory
package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
)

// Bootstrap filenames (matching TS workspace.ts constants).
const (
	AgentsFile         = "AGENTS.md"
	SoulFile           = "SOUL.md"
	ToolsFile          = "TOOLS.md"
	IdentityFile       = "IDENTITY.md"
	UserFile           = "USER.md"
	UserPredefinedFile = "USER_PREDEFINED.md"
	BootstrapFile      = "BOOTSTRAP.md"
	DelegationFile   = "DELEGATION.md"
	TeamFile         = "TEAM.md"
	AvailabilityFile = "AVAILABILITY.md"
	MemoryFile     = "MEMORY.md"
	MemoryAltFile  = "memory.md"
	MemoryJSONFile = "MEMORY.json"
)

// standardFiles is the ordered list of bootstrap files to load.
var standardFiles = []string{
	AgentsFile,
	SoulFile,
	ToolsFile,
	IdentityFile,
	UserFile,
	BootstrapFile,
}

// minimalAllowlist is the set of files loaded for subagent/cron sessions.
// Matching TS MINIMAL_BOOTSTRAP_ALLOWLIST.
var minimalAllowlist = map[string]bool{
	AgentsFile: true,
	ToolsFile:  true,
}

// File represents a workspace bootstrap file loaded from disk.
type File struct {
	Name    string // filename (e.g. "AGENTS.md")
	Path    string // absolute path
	Content string // file content (empty if missing)
	Missing bool   // true if file doesn't exist on disk
}

// ContextFile is the truncated version ready for system prompt injection.
// Matches TS EmbeddedContextFile type.
type ContextFile struct {
	Path    string // display path (e.g. "SOUL.md")
	Content string // truncated content
}

// LoadWorkspaceFiles reads all recognized bootstrap files from a workspace directory.
// Files are returned in a fixed order matching the TS implementation.
// Missing files are included with Missing=true and empty Content.
func LoadWorkspaceFiles(workspaceDir string) []File {
	var files []File

	// Load standard files
	for _, name := range standardFiles {
		f := loadFile(workspaceDir, name)
		files = append(files, f)
	}

	// Load MEMORY.md (try MEMORY.md first, then memory.md)
	memFile := loadFile(workspaceDir, MemoryFile)
	if memFile.Missing {
		memFile = loadFile(workspaceDir, MemoryAltFile)
	}
	files = append(files, memFile)

	return files
}

// FilterForSession filters bootstrap files based on session type.
// Normal sessions get all files. Subagent and cron sessions get only
// AGENTS.md and TOOLS.md (minimal mode), matching TS filterBootstrapFilesForSession().
func FilterForSession(files []File, sessionKey string) []File {
	if !IsSubagentSession(sessionKey) && !IsCronSession(sessionKey) {
		return files
	}

	var filtered []File
	for _, f := range files {
		if minimalAllowlist[f.Name] {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

// IsSubagentSession checks if a session key indicates a subagent session.
// Session key format: agent:{agentId}:{rest}
// Subagent sessions have "subagent:" in the rest part.
func IsSubagentSession(sessionKey string) bool {
	rest := sessionRest(sessionKey)
	return strings.HasPrefix(strings.ToLower(rest), "subagent:")
}

// IsCronSession checks if a session key indicates a cron session.
// Session key format: agent:{agentId}:{rest}
// Cron sessions have "cron:" in the rest part.
func IsCronSession(sessionKey string) bool {
	rest := sessionRest(sessionKey)
	return strings.HasPrefix(strings.ToLower(rest), "cron:")
}

// IsTeamSession checks if a session key indicates a team-dispatched task session.
// Session key format: agent:{agentId}:team:{teamID}:{chatID}
// Team sessions have "team:" in the rest part.
func IsTeamSession(sessionKey string) bool {
	rest := sessionRest(sessionKey)
	return strings.HasPrefix(strings.ToLower(rest), "team:")
}

// sessionRest extracts the rest part after "agent:{agentId}:" from a session key.
func sessionRest(sessionKey string) string {
	// Format: agent:{agentId}:{rest}
	parts := strings.SplitN(sessionKey, ":", 3)
	if len(parts) < 3 || parts[0] != "agent" {
		return ""
	}
	return parts[2]
}

func loadFile(dir, name string) File {
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return File{Name: name, Path: path, Missing: true}
	}
	return File{Name: name, Path: path, Content: string(data), Missing: false}
}
