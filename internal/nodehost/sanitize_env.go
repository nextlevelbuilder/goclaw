package nodehost

import (
	"os"
	"regexp"
	"strings"
)

// Blocked env keys — never allowed in inherited or overridden environments.
var blockedEnvKeys = newSet(
	"NODE_OPTIONS", "NODE_PATH",
	"PYTHONHOME", "PYTHONPATH",
	"PERL5LIB", "PERL5OPT",
	"RUBYLIB", "RUBYOPT",
	"BASH_ENV", "ENV", "BROWSER",
	"GIT_EDITOR", "GIT_EXTERNAL_DIFF", "GIT_EXEC_PATH",
	"GIT_SEQUENCE_EDITOR", "GIT_TEMPLATE_DIR",
	"GIT_SSL_NO_VERIFY", "GIT_SSL_CAINFO", "GIT_SSL_CAPATH",
	"CC", "CXX", "CARGO_BUILD_RUSTC", "CMAKE_C_COMPILER", "CMAKE_CXX_COMPILER",
	"SHELL", "SHELLOPTS", "PS4", "GCONV_PATH", "IFS", "SSLKEYLOGFILE",
	"JAVA_TOOL_OPTIONS", "_JAVA_OPTIONS", "JDK_JAVA_OPTIONS",
	"PYTHONBREAKPOINT", "DOTNET_STARTUP_HOOKS", "DOTNET_ADDITIONAL_DEPS",
	"GLIBC_TUNABLES", "MAVEN_OPTS", "SBT_OPTS", "GRADLE_OPTS", "ANT_OPTS",
)

// Blocked env prefixes — any key starting with these is blocked.
var blockedEnvPrefixes = []string{"DYLD_", "LD_", "BASH_FUNC_"}

// Blocked override-only keys — blocked when provided as overrides but allowed when inherited.
var blockedOverrideKeys = newSet(
	"HOME", "GRADLE_USER_HOME", "ZDOTDIR",
	"GIT_SSH_COMMAND", "GIT_SSH", "GIT_PROXY_COMMAND", "GIT_ASKPASS",
	"GIT_SSL_NO_VERIFY", "GIT_SSL_CAINFO", "GIT_SSL_CAPATH",
	"SSH_ASKPASS", "LESSOPEN", "LESSCLOSE", "PAGER", "MANPAGER", "GIT_PAGER",
	"EDITOR", "VISUAL", "FCEDIT", "SUDO_EDITOR",
	"PROMPT_COMMAND", "HISTFILE",
	"PERL5DB", "PERL5DBCMD",
	"OPENSSL_CONF", "OPENSSL_ENGINES",
	"PYTHONSTARTUP", "WGETRC", "CURL_HOME",
	"CLASSPATH", "CGO_CFLAGS", "CGO_LDFLAGS", "GOFLAGS",
	"CORECLR_PROFILER_PATH", "PHPRC", "PHP_INI_SCAN_DIR",
	"DENO_DIR", "BUN_CONFIG_REGISTRY",
	"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
	"NODE_TLS_REJECT_UNAUTHORIZED", "NODE_EXTRA_CA_CERTS",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE",
	"DOCKER_HOST", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "DOCKER_CONTEXT",
	"PIP_INDEX_URL", "PIP_PYPI_URL", "PIP_EXTRA_INDEX_URL",
	"PIP_CONFIG_FILE", "PIP_FIND_LINKS", "PIP_TRUSTED_HOST",
	"UV_INDEX", "UV_INDEX_URL", "UV_EXTRA_INDEX_URL", "UV_DEFAULT_INDEX",
	"LIBRARY_PATH", "CPATH", "C_INCLUDE_PATH", "CPLUS_INCLUDE_PATH", "OBJC_INCLUDE_PATH",
	"GOPROXY", "GONOSUMCHECK", "GONOSUMDB", "GONOPROXY", "GOPRIVATE", "GOENV", "GOPATH",
	"PYTHONUSERBASE", "VIRTUAL_ENV",
	"LUA_PATH", "LUA_CPATH", "GEM_HOME", "GEM_PATH", "BUNDLE_GEMFILE",
	"COMPOSER_HOME", "XDG_CONFIG_HOME", "AWS_CONFIG_FILE",
)

// Blocked override prefixes.
var blockedOverridePrefixes = []string{"GIT_CONFIG_", "NPM_CONFIG_"}

// Shell wrapper allowed override keys — only these are passed through when shell wrapping.
var shellWrapperAllowedOverrideKeys = newSet(
	"TERM", "LANG", "LC_ALL", "LC_CTYPE", "LC_MESSAGES",
	"COLORTERM", "NO_COLOR", "FORCE_COLOR",
)

var portableEnvVarKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// isDangerousEnvKey checks if a key is in the blocked keys or has a blocked prefix.
func isDangerousEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	if blockedEnvKeys[upper] {
		return true
	}
	for _, prefix := range blockedEnvPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// isDangerousOverrideKey checks if a key is blocked for overrides.
func isDangerousOverrideKey(key string) bool {
	upper := strings.ToUpper(key)
	if blockedOverrideKeys[upper] {
		return true
	}
	for _, prefix := range blockedOverridePrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// SanitizeEnv builds a sanitized environment by merging the process env with optional overrides.
// Blocks dangerous keys, PATH overrides, and override-only restricted keys.
func SanitizeEnv(overrides map[string]string) map[string]string {
	merged := make(map[string]string)

	// Copy inherited env, dropping dangerous keys.
	for _, entry := range os.Environ() {
		k, v, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if isDangerousEnvKey(k) {
			continue
		}
		merged[k] = v
	}

	// Apply overrides, blocking dangerous and override-only keys.
	for k, v := range overrides {
		upper := strings.ToUpper(k)
		if upper == "PATH" {
			continue
		}
		if isDangerousEnvKey(k) || isDangerousOverrideKey(k) {
			continue
		}
		merged[k] = v
	}
	return merged
}

// SanitizeSystemRunEnvOverrides filters overrides for shell wrapper contexts.
// For shell wrappers, only terminal/locale vars are allowed.
func SanitizeSystemRunEnvOverrides(overrides map[string]string, shellWrapper bool) map[string]string {
	if overrides == nil {
		return nil
	}
	if !shellWrapper {
		return overrides
	}
	filtered := make(map[string]string)
	for k, v := range overrides {
		if !portableEnvVarKey.MatchString(k) {
			continue
		}
		if !shellWrapperAllowedOverrideKeys[strings.ToUpper(k)] {
			continue
		}
		filtered[k] = v
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}
