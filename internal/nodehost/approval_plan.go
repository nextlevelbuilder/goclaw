package nodehost

import "regexp"

// ApprovedCwdSnapshot captures the identity of a working directory at approval time.
type ApprovedCwdSnapshot struct {
	Cwd  string          `json:"cwd"`
	Stat FileIdentityStat `json:"stat"`
}

// --- Interpreter detection tables ---

var mutableArgv1InterpreterPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^(?:node|nodejs)$`),
	regexp.MustCompile(`^perl$`),
	regexp.MustCompile(`^php$`),
	regexp.MustCompile(`^python(?:\d+(?:\.\d+)*)?$`),
	regexp.MustCompile(`^ruby$`),
}

var genericMutableScriptRunners = newSet(
	"esno", "jiti", "ts-node", "ts-node-esm", "tsx", "vite-node",
)

var interpreterLikeSafeBins = newSet(
	"ash", "awk", "bash", "busybox", "bun", "cmd", "cmd.exe", "cscript",
	"dash", "deno", "fish", "gawk", "gsed", "ksh", "lua", "mawk", "nawk",
	"node", "nodejs", "perl", "php", "powershell", "powershell.exe", "pypy",
	"pwsh", "pwsh.exe", "python", "python2", "python3", "ruby", "sed", "sh",
	"toybox", "wscript", "zsh",
)

var interpreterLikePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^python\d+(?:\.\d+)?$`),
	regexp.MustCompile(`^ruby\d+(?:\.\d+)?$`),
	regexp.MustCompile(`^perl\d+(?:\.\d+)?$`),
	regexp.MustCompile(`^php\d+(?:\.\d+)?$`),
	regexp.MustCompile(`^node\d+(?:\.\d+)?$`),
}

// IsInterpreterLikeSafeBin checks if a binary name is an interpreter-like safe bin.
func IsInterpreterLikeSafeBin(raw string) bool {
	normalized := NormalizeExecutableToken(raw)
	if normalized == "" {
		return false
	}
	if interpreterLikeSafeBins[normalized] {
		return true
	}
	for _, p := range interpreterLikePatterns {
		if p.MatchString(normalized) {
			return true
		}
	}
	return false
}

func isMutableScriptRunner(exe string) bool {
	return genericMutableScriptRunners[exe] || IsInterpreterLikeSafeBin(exe)
}

// --- Bun/Deno/Node option sets ---

var bunSubcommands = newSet("add", "audit", "completions", "create", "exec", "help", "init",
	"install", "link", "outdated", "patch", "pm", "publish", "remove", "repl", "run", "test",
	"unlink", "update", "upgrade", "x")
var bunOptionsWithValue = newSet("--backend", "--bunfig", "--conditions", "--config",
	"--console-depth", "--cwd", "--define", "--elide-lines", "--env-file", "--extension-order",
	"--filter", "--hot", "--inspect", "--inspect-brk", "--inspect-wait", "--install",
	"--jsx-factory", "--jsx-fragment", "--jsx-import-source", "--loader", "--origin", "--port",
	"--preload", "--smol", "--tsconfig-override", "-c", "-e", "-p", "-r")
var denoRunOptionsWithValue = newSet("--cached-only", "--cert", "--config", "--env-file", "--ext",
	"--harmony-import-attributes", "--import-map", "--inspect", "--inspect-brk", "--inspect-wait",
	"--location", "--log-level", "--lock", "--node-modules-dir", "--no-check", "--preload",
	"--reload", "--seed", "--strace-ops", "--unstable-bare-node-builtins", "--v8-flags",
	"--watch", "--watch-exclude", "-L")
var nodeOptionsWithFileValue = newSet("-r", "--experimental-loader", "--import", "--loader", "--require")
var rubyUnsafeFlags = newSet("-I", "-r", "--require")
var perlUnsafeFlags = newSet("-I", "-M", "-m")
var posixShellOptionsWithValue = newSet("--init-file", "--rcfile", "--startup-script", "-o")

// --- Package manager option sets ---

var npmExecOptionsWithValue = newSet("--cache", "--package", "--prefix", "--script-shell",
	"--userconfig", "--workspace", "-p", "-w")
var npmExecFlagOptions = newSet("--no", "--quiet", "--ws", "--workspaces", "--yes", "-q", "-y")
var pnpmOptionsWithValue = newSet("--config", "--dir", "--filter", "--reporter", "--stream",
	"--test-pattern", "--workspace-concurrency", "-C")
var pnpmFlagOptions = newSet("--aggregate-output", "--color", "--recursive", "--silent",
	"--workspace-root", "-r")
