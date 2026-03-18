package skills

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// RuntimeInfo describes a single runtime binary's availability and version.
type RuntimeInfo struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
}

// RuntimeStatus holds the availability of all prerequisite runtimes.
type RuntimeStatus struct {
	Runtimes []RuntimeInfo `json:"runtimes"`
	Ready    bool          `json:"ready"` // true if all critical runtimes (python3, pip3) are available
}

// CheckRuntimes probes the system for prerequisite binaries and returns their status.
func CheckRuntimes() *RuntimeStatus {
	checks := []struct {
		name     string
		bin      string
		vFlag    string
		critical bool // if missing, Ready=false
	}{
		{"python3", "python3", "--version", true},
		{"pip3", "pip3", "--version", true},
		{"node", "node", "--version", false},
		{"npm", "npm", "--version", false},
	}

	status := &RuntimeStatus{Ready: true}

	for _, c := range checks {
		info := RuntimeInfo{Name: c.name}

		if _, err := exec.LookPath(c.bin); err != nil {
			info.Available = false
			if c.critical {
				status.Ready = false
			}
		} else {
			info.Available = true
			if c.vFlag != "" {
				info.Version = getVersion(c.bin, c.vFlag)
			}
		}

		status.Runtimes = append(status.Runtimes, info)
	}

	// Check pkg-helper socket availability (not a binary, but a Unix socket).
	pkgInfo := RuntimeInfo{Name: "pkg-helper"}
	if fi, err := os.Stat(pkgHelperSocket); err == nil && fi.Mode().Type()&os.ModeSocket != 0 {
		pkgInfo.Available = true
		pkgInfo.Version = "socket"
	}
	status.Runtimes = append(status.Runtimes, pkgInfo)

	return status
}

// getVersion runs "bin flag" with a timeout and returns the first line of output.
func getVersion(bin, flag string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, bin, flag).CombinedOutput()
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(out))
	if idx := strings.IndexByte(s, '\n'); idx > 0 {
		s = s[:idx]
	}
	// Strip path info from outputs like "pip 23.x from /usr/lib/... (python 3.x)"
	if idx := strings.Index(s, " from "); idx > 0 {
		s = s[:idx]
	}
	return s
}
