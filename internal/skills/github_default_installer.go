package skills

import "sync"

// defaultGitHubInstaller is the process-wide installer used by free functions
// (InstallSingleDep, UninstallPackage, ListInstalledPackages). It is set once
// at startup via SetDefaultGitHubInstaller and read without locking thereafter.
var (
	defaultGitHubInstallerMu sync.RWMutex
	defaultGitHubInstaller   *GitHubInstaller
)

// SetDefaultGitHubInstaller registers the installer used by prefix dispatch in
// the top-level install/uninstall/list helpers. Safe to call multiple times
// (replaces the existing installer).
func SetDefaultGitHubInstaller(i *GitHubInstaller) {
	defaultGitHubInstallerMu.Lock()
	defer defaultGitHubInstallerMu.Unlock()
	defaultGitHubInstaller = i
}

// DefaultGitHubInstaller returns the registered installer, or nil if unset.
func DefaultGitHubInstaller() *GitHubInstaller {
	defaultGitHubInstallerMu.RLock()
	defer defaultGitHubInstallerMu.RUnlock()
	return defaultGitHubInstaller
}
