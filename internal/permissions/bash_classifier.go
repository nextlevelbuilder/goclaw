// Package permissions — Bash command classification (CP-06).
// Classifies shell commands by risk level for the permission pipeline.
package permissions

import "strings"

// CommandClass categorizes bash commands by risk level.
type CommandClass int

const (
	ClassReadOnly    CommandClass = iota // cat, ls, grep — no side effects
	ClassNetworkRead                     // curl GET, wget — network read
	ClassFileMutation                    // touch, cp, mv — filesystem mutation
	ClassNetworkWrite                    // curl POST, ssh — network + mutation
	ClassProcessCtrl                     // kill, pkill — process management
	ClassDestructive                     // rm -rf, dd — irreversible damage
	ClassInterpreter                     // python, node, eval — arbitrary code exec
)

// String returns a human-readable name for the command class.
func (c CommandClass) String() string {
	switch c {
	case ClassReadOnly:
		return "read-only"
	case ClassNetworkRead:
		return "network-read"
	case ClassFileMutation:
		return "file-mutation"
	case ClassNetworkWrite:
		return "network-write"
	case ClassProcessCtrl:
		return "process-control"
	case ClassDestructive:
		return "destructive"
	case ClassInterpreter:
		return "interpreter"
	}
	return "unknown"
}

// ClassifyCommand returns the risk class of a bash command.
// Examines command name, flags, arguments, and pipe targets.
// Highest risk segment determines the classification.
func ClassifyCommand(cmd string) CommandClass {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ClassReadOnly
	}

	// Handle pipes — classify each segment, return highest risk
	if strings.Contains(cmd, " | ") {
		highest := ClassReadOnly
		for _, seg := range strings.Split(cmd, "|") {
			c := ClassifyCommand(strings.TrimSpace(seg))
			if c > highest {
				highest = c
			}
		}
		return highest
	}

	// Handle command chains: &&, ;, ||
	for _, sep := range []string{" && ", " || ", "; "} {
		if strings.Contains(cmd, sep) {
			highest := ClassReadOnly
			for _, seg := range strings.Split(cmd, strings.TrimSpace(sep)) {
				c := ClassifyCommand(strings.TrimSpace(seg))
				if c > highest {
					highest = c
				}
			}
			return highest
		}
	}

	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return ClassReadOnly
	}
	base := parts[0]

	// Interpreters (highest risk)
	if interpreters[base] {
		return ClassInterpreter
	}

	// Destructive
	if isDestructiveCmd(base, cmd) {
		return ClassDestructive
	}

	// Process control
	if processCtrl[base] {
		return ClassProcessCtrl
	}

	// Network write
	if isNetworkWriteCmd(base, cmd) {
		return ClassNetworkWrite
	}

	// Network read
	if networkRead[base] {
		return ClassNetworkRead
	}

	// File mutation
	if fileMutation[base] {
		return ClassFileMutation
	}

	// Default: read-only
	return ClassReadOnly
}

var interpreters = map[string]bool{
	"python": true, "python3": true, "python2": true,
	"node": true, "deno": true, "bun": true, "tsx": true,
	"ruby": true, "perl": true, "php": true, "lua": true,
	"bash": true, "sh": true, "zsh": true, "fish": true,
	"eval": true, "exec": true, "xargs": true,
	"npx": true, "bunx": true, "pnpx": true,
	"npm run": true, "yarn run": true,
	"sudo": true, // privilege escalation
}

var processCtrl = map[string]bool{
	"kill": true, "pkill": true, "killall": true,
	"systemctl": true, "service": true, "launchctl": true,
	"reboot": true, "shutdown": true, "halt": true,
}

var networkRead = map[string]bool{
	"curl": true, "wget": true, "dig": true,
	"nslookup": true, "ping": true, "traceroute": true,
	"host": true, "whois": true, "nmap": true,
}

var fileMutation = map[string]bool{
	"touch": true, "cp": true, "mv": true, "rm": true,
	"mkdir": true, "rmdir": true, "chmod": true, "chown": true,
	"ln": true, "tee": true, "truncate": true, "install": true,
	"tar": true, "unzip": true, "zip": true, "gzip": true,
}

func isDestructiveCmd(base, cmd string) bool {
	if base == "rm" && (strings.Contains(cmd, "-rf") || strings.Contains(cmd, "-fr")) {
		return true
	}
	if base == "dd" || base == "mkfs" || base == "fdisk" || base == "parted" {
		return true
	}
	return false
}

func isNetworkWriteCmd(base, cmd string) bool {
	if base == "ssh" || base == "scp" || base == "rsync" || base == "sftp" {
		return true
	}
	if base == "curl" {
		lc := strings.ToLower(cmd)
		if strings.Contains(lc, "-x post") || strings.Contains(lc, "-x put") ||
			strings.Contains(lc, "-x delete") || strings.Contains(lc, "-x patch") ||
			strings.Contains(lc, "-d ") || strings.Contains(lc, "--data") ||
			strings.Contains(lc, "--upload-file") {
			return true
		}
	}
	return false
}
