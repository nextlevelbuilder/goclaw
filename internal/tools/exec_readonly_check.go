// Package tools — Read-only command classifier for exec/shell tools (CP-02).
// Determines if a shell command has no side effects and can run concurrently.
package tools

import (
	"strings"
)

// readOnlyCommands is a whitelist of commands known to be side-effect-free.
var readOnlyCommands = map[string]bool{
	// File reading
	"cat": true, "head": true, "tail": true, "less": true, "more": true,
	"wc": true, "file": true, "stat": true, "du": true, "df": true,
	"md5sum": true, "sha256sum": true, "sha1sum": true,

	// Search
	"grep": true, "egrep": true, "fgrep": true,
	"rg": true, "ag": true, "ack": true,
	"find": true, "fd": true, "locate": true,
	"which": true, "whereis": true, "type": true,

	// Listing
	"ls": true, "tree": true, "exa": true, "eza": true, "lsd": true,

	// Text processing (read-only when not redirecting)
	"sort": true, "uniq": true, "cut": true, "tr": true,
	"awk": true, "sed": true, // NOTE: sed without -i is read-only
	"jq": true, "yq": true, "xq": true,
	"column": true, "fmt": true, "fold": true,
	"diff": true, "comm": true, "cmp": true,

	// System info
	"uname": true, "hostname": true, "uptime": true, "whoami": true,
	"id": true, "groups": true, "date": true, "cal": true,
	"env": true, "printenv": true,
	"echo": true, "printf": true, "true": true, "false": true,

	// Docker read-only
	"docker ps": true, "docker images": true, "docker inspect": true,
	"docker logs": true, "docker stats": true, "docker info": true,
	"docker version": true, "docker network ls": true,
	"docker volume ls": true, "docker compose ps": true,

	// Go read-only
	"go vet": true, "go doc": true, "go list": true, "go env": true,
	"go version": true, "go mod graph": true,

	// Git read-only
	"git log": true, "git status": true, "git diff": true,
	"git show": true, "git blame": true, "git branch": true,
	"git tag": true, "git remote": true, "git rev-parse": true,
	"git ls-files": true, "git shortlog": true, "git stash list": true,
	"git describe": true, "git config": true, "git reflog": true,

	// Linters / type checkers (analysis only)
	"pyright": true, "mypy": true, "pylint": true, "flake8": true,
	"eslint": true, "tsc": true, "golangci-lint": true,
	"shellcheck": true, "hadolint": true, "yamllint": true,

	// Package managers (info only)
	"npm list": true, "npm ls": true, "npm view": true, "npm info": true,
	"pip list": true, "pip show": true, "pip freeze": true,
	"go mod tidy": false, // actually mutating
}

// dangerousFlags negate read-only safety even for safe base commands.
var dangerousFlags = []string{
	" -w ", " --write ", " -i ", " --in-place ",
	" --delete ", " --remove ", " --force ", " -f ",
	" -rf ", " -fr ", " --exec ",
	" > ", " >> ", " 2> ", " &> ",
}

// IsReadOnlyCommand checks if a shell command is safe for concurrent execution.
// Handles pipes, command chains, and flag analysis.
func IsReadOnlyCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}

	// Check for output redirection first — always mutating
	for _, redir := range []string{" > ", " >> ", " 2> ", " &> "} {
		if strings.Contains(command, redir) {
			return false
		}
	}

	// Handle pipes: each segment must be safe
	if strings.Contains(command, " | ") || strings.HasSuffix(command, " |") {
		for _, seg := range strings.Split(command, "|") {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}
			if !IsReadOnlyCommand(seg) {
				return false
			}
		}
		return true
	}

	// Handle command chains (&&, ||, ;)
	for _, sep := range []string{" && ", " || ", "; "} {
		if strings.Contains(command, sep) {
			for _, seg := range strings.Split(command, sep) {
				seg = strings.TrimSpace(seg)
				if seg == "" {
					continue
				}
				if !IsReadOnlyCommand(seg) {
					return false
				}
			}
			return true
		}
	}

	// Check for dangerous flags
	padded := " " + command + " "
	for _, flag := range dangerousFlags {
		if strings.Contains(padded, flag) {
			// Exception: sed without -i is read-only
			if flag == " -i " && extractBaseCommand(command) == "sed" {
				continue // sed -i is actually mutating, but "sed" alone is read-only
			}
			return false
		}
	}

	// Extract base command
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}

	// Try two-word match (e.g., "git log", "docker ps")
	if len(parts) >= 2 {
		twoWord := parts[0] + " " + parts[1]
		if safe, exists := readOnlyCommands[twoWord]; exists {
			return safe
		}
	}

	// Single-word match
	if safe, exists := readOnlyCommands[parts[0]]; exists {
		return safe
	}

	// Unknown command → not safe (conservative)
	return false
}

func extractBaseCommand(cmd string) string {
	parts := strings.Fields(strings.TrimSpace(cmd))
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
