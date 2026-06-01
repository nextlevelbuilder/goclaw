package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/net/idna"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// gitAdapter implements the CredentialAdapter contract for the `git` binary.
//
// PAT path (this file): host-scoped HTTP extraheader injected via
// GIT_CONFIG_COUNT/GIT_CONFIG_KEY_0/GIT_CONFIG_VALUE_0 env vars (git 2.31+).
// Keeps the token out of /proc/<pid>/cmdline and `ps` output — the typical
// `-c http.extraHeader=...` form would land the bearer token directly in argv
// where any process listing leaks it.
//
// SSH path: Phase 4 extends the same Prepare switch.
type gitAdapter struct{}

func (gitAdapter) Name() string { return "git" }

// ShouldInject returns true only for subcommands that talk to a remote.
// Status/log/diff/commit do not need credentials, and asking the adapter for
// them would waste a `git config --get` sub-exec and emit noisy audit events.
func (gitAdapter) ShouldInject(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	switch argv[0] {
	case "clone", "fetch", "pull", "push", "submodule":
		return true
	}
	return false
}

var (
	errEmptyToken          = errors.New("git adapter: token is empty")
	errTokenTooLong        = errors.New("git adapter: token exceeds 4 KiB")
	errTokenControlChar    = errors.New("git adapter: token contains control character (CR/LF/NUL)")
	errEmptyHost           = errors.New("git adapter: host is empty")
	errEmbeddedUserinfo    = errors.New("git adapter: URL has embedded userinfo (ambiguous; rejected)")
	errURLMissingHost      = errors.New("git adapter: URL has no host")
	errURLUnsupportedForm  = errors.New("git adapter: URL form not recognized")
	errCloneURLMissing     = errors.New("git adapter: clone command has no URL argument")
	errUnknownSubcommand   = errors.New("git adapter: unknown subcommand")
	errCredentialTypeBlank = errors.New("git adapter: credential_type required for git adapter routing")
)

// errCredentialHostMismatch is returned when the matched credential's host
// scope does not equal the target host extracted from argv (or remote URL).
// Carries both hosts in fields so callers can format an i18n message without
// re-parsing. Error.Error() intentionally omits any token-shaped data.
type errCredentialHostMismatch struct {
	credHost   string
	targetHost string
}

func (e *errCredentialHostMismatch) Error() string {
	return fmt.Sprintf("git adapter: credential host %q does not match target host %q", e.credHost, e.targetHost)
}

func (gitAdapter) Prepare(ctx context.Context, _ *store.SecureCLIBinary, cred *store.SecureCLIUserCredential, argv []string) (*Injection, error) {
	if cred == nil {
		return &Injection{}, nil
	}
	typ := ""
	if cred.CredentialType != nil {
		typ = *cred.CredentialType
	}
	// Legacy env-vars rows: behave exactly like passthrough so existing operators
	// who wired `git` via env-paste before the adapter existed keep working.
	if typ == "" || typ == "env" {
		return &Injection{}, nil
	}

	// cwd from ctx so `git config --get remote.X.url` runs inside the repo
	// the caller is about to operate on. Empty when caller did not set it
	// (e.g. clone, where the URL is in argv and cwd is irrelevant).
	host, err := resolveTargetHost(ctx, argv, ExecCwdFromContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("resolve target host: %w", err)
	}
	credHostRaw := ""
	if cred.HostScope != nil {
		credHostRaw = *cred.HostScope
	}
	credHost, err := normalizeHost(credHostRaw)
	if err != nil {
		return nil, fmt.Errorf("normalize credential host: %w", err)
	}
	targetHost, err := normalizeHost(host)
	if err != nil {
		return nil, fmt.Errorf("normalize target host: %w", err)
	}
	if credHost != targetHost {
		return nil, &errCredentialHostMismatch{credHost: credHost, targetHost: targetHost}
	}

	switch typ {
	case "pat":
		token, err := decodePATToken(cred.EncryptedEnv)
		if err != nil {
			return nil, err
		}
		if err := validateTokenShape(token); err != nil {
			return nil, err
		}
		// GIT_CONFIG_COUNT env approach (git 2.31+) — same effect as
		// `-c http.https://host/.extraheader=Authorization: Bearer <tok>` but
		// the token stays out of argv. Single-entry header keeps host-scoping
		// strict; the matching url prefix ensures git skips its credential
		// helpers for this URL automatically.
		configKey := fmt.Sprintf("http.https://%s/.extraheader", targetHost)
		configVal := fmt.Sprintf("Authorization: Bearer %s", token)
		return &Injection{
			Env: map[string]string{
				"GIT_CONFIG_COUNT":   "1",
				"GIT_CONFIG_KEY_0":   configKey,
				"GIT_CONFIG_VALUE_0": configVal,
			},
			ScrubValues: []string{token},
		}, nil
	case "ssh_key":
		keyPEM, err := decodeSSHKeyBlob(cred.EncryptedEnv)
		if err != nil {
			return nil, err
		}
		// Defense-in-depth: ValidateSSHKey is called at save; running it again
		// here catches keys that bypassed validation (e.g. raw DB write or a
		// future store mutation that skips the handler path).
		if err := ValidateSSHKey(keyPEM); err != nil {
			return nil, err
		}
		keyPath, cleanup, err := materializeEphemeral(ctx, keyPEM, "gitkey")
		if err != nil {
			return nil, err
		}
		// -o flags pinned for safety:
		//   IdentitiesOnly=yes      — ssh must not try other keys in ~/.ssh
		//   BatchMode=yes           — never prompt; fail fast in agent context
		//   StrictHostKeyChecking=accept-new — first contact TOFU; known hosts
		//                                       still enforced after
		//   UserKnownHostsFile=~/.ssh/known_hosts is left at default so
		//   operators can pre-seed pinned host keys (documented in Phase 6).
		sshCmd := fmt.Sprintf(
			"ssh -i %s -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=accept-new",
			keyPath,
		)
		return &Injection{
			Env:         map[string]string{"GIT_SSH_COMMAND": sshCmd},
			Cleanup:     cleanup,
			ScrubValues: []string{keyPath},
		}, nil
	default:
		return nil, fmt.Errorf("git adapter: unsupported credential_type %q", typ)
	}
}

// decodeSSHKeyBlob extracts the private-key PEM from the credential blob.
// v1 wire shape: `{"key":"-----BEGIN OPENSSH PRIVATE KEY-----\n..."}`.
// No legacy fallback — SSH support is new in Phase 4, so any existing
// row with credential_type='ssh_key' was written by the new wire.
func decodeSSHKeyBlob(blob []byte) ([]byte, error) {
	if len(blob) == 0 {
		return nil, errors.New("git adapter: empty ssh key blob")
	}
	var m map[string]string
	if err := json.Unmarshal(blob, &m); err != nil {
		return nil, fmt.Errorf("decode ssh key blob: %w", err)
	}
	if k, ok := m["key"]; ok && k != "" {
		return []byte(k), nil
	}
	return nil, errors.New("git adapter: ssh key blob missing 'key' field")
}

// decodePATToken extracts the token from the credential blob. v1 wire shape
// is `{"token": "..."}`. Falls back to legacy env-style `{"GIT_TOKEN": "..."}`
// only when the typed key is missing, so operators migrating from env-paste
// rows do not need to re-enter their PAT.
func decodePATToken(blob []byte) (string, error) {
	if len(blob) == 0 {
		return "", errEmptyToken
	}
	var m map[string]string
	if err := json.Unmarshal(blob, &m); err != nil {
		return "", fmt.Errorf("decode pat blob: %w", err)
	}
	if tok, ok := m["token"]; ok {
		return tok, nil
	}
	if tok, ok := m["GIT_TOKEN"]; ok {
		return tok, nil
	}
	return "", errEmptyToken
}

// validateTokenShape rejects empty, oversized, and control-char tokens. The
// CR/LF check defends against header injection if the UI validator ever
// regresses — a token like "ghp_x\r\nX-Evil: y" would otherwise smuggle a
// second HTTP header through GIT_CONFIG_VALUE_0.
func validateTokenShape(tok string) error {
	if tok == "" {
		return errEmptyToken
	}
	if len(tok) > 4096 {
		return errTokenTooLong
	}
	for _, r := range tok {
		if r < 0x20 || r == 0x7f {
			return errTokenControlChar
		}
	}
	return nil
}

// normalizeHost canonicalizes a hostname for apples-to-apples comparison:
// trim whitespace + trailing dot, lowercase, idna.Lookup.ToASCII for IDN/
// punycode equivalence. Preserves explicit ports (`:8443`) verbatim.
func normalizeHost(h string) (string, error) {
	h = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
	if h == "" {
		return "", errEmptyHost
	}
	hostPart, port, hasPort := splitHostPortOptional(h)
	ascii, err := idna.Lookup.ToASCII(hostPart)
	if err != nil {
		return "", fmt.Errorf("idna: %w", err)
	}
	if hasPort {
		return ascii + ":" + port, nil
	}
	return ascii, nil
}

// splitHostPortOptional splits "host:port" when port is all digits; otherwise
// returns host unchanged. Avoids net.SplitHostPort because that errors on
// portless inputs and on IPv6 forms without brackets, which we don't accept.
func splitHostPortOptional(h string) (host, port string, ok bool) {
	idx := strings.LastIndex(h, ":")
	if idx < 0 {
		return h, "", false
	}
	port = h[idx+1:]
	if port == "" {
		return h, "", false
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			return h, "", false
		}
	}
	return h[:idx], port, true
}

// resolveTargetHost finds the host this git invocation will contact. For
// clone, the URL is in argv. For fetch/pull/push/submodule, we sub-exec a
// hardened `git config --get remote.<name>.url` to read it from .git/config.
//
// cwd is the working directory for the sub-exec. Empty means current process
// dir (used by adapter Prepare in production where the exec hasn't chdir'd
// yet — git -C . is the implicit default).
func resolveTargetHost(ctx context.Context, argv []string, cwd string) (string, error) {
	if len(argv) == 0 {
		return "", errUnknownSubcommand
	}
	switch argv[0] {
	case "clone":
		u, err := firstNonFlagArg(argv[1:])
		if err != nil {
			return "", err
		}
		return parseHostFromGitURL(u)
	case "fetch", "pull":
		remote := pickRemoteFromArgv(argv[1:], "origin")
		return remoteURLViaConfigGet(ctx, cwd, remote)
	case "push":
		remote := pickPushRemoteFromArgv(argv[1:], "origin")
		return remoteURLViaConfigGet(ctx, cwd, remote)
	case "submodule":
		// `git submodule update` uses the parent repo's origin URL as the
		// reachability base for relative submodule paths, and the submodule
		// fetch reuses the parent's auth header when the host matches.
		return remoteURLViaConfigGet(ctx, cwd, "origin")
	}
	return "", errUnknownSubcommand
}

// firstNonFlagArg returns the first argv element that does not start with `-`.
// Used to find the clone URL past option flags like --depth, --branch, etc.
func firstNonFlagArg(args []string) (string, error) {
	skipValueFor := map[string]bool{
		// Long options that take a separate value as the next arg.
		"--branch": true, "--depth": true, "--origin": true, "--config": true,
		"--reference": true, "--reference-if-able": true, "--separate-git-dir": true,
		"--template": true, "--upload-pack": true, "--jobs": true, "--shallow-since": true,
		"--shallow-exclude": true, "--filter": true, "--server-option": true,
		"-b": true, "-o": true, "-c": true, "-j": true, "-u": true,
	}
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(a, "-") {
			// `--depth=1` style — value embedded; no skip.
			if strings.Contains(a, "=") {
				continue
			}
			if skipValueFor[a] {
				skipNext = true
			}
			continue
		}
		return a, nil
	}
	return "", errCloneURLMissing
}

// pickRemoteFromArgv finds the explicit remote name in `git fetch/pull` argv,
// or returns `fallback` when none is given. Flags are skipped.
func pickRemoteFromArgv(args []string, fallback string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		return a
	}
	return fallback
}

// pickPushRemoteFromArgv handles `git push`'s extra `--repo=<remote>` shape.
func pickPushRemoteFromArgv(args []string, fallback string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "--repo=") {
			return strings.TrimPrefix(a, "--repo=")
		}
	}
	return pickRemoteFromArgv(args, fallback)
}

// remoteURLViaConfigGet runs a hardened `git config --get remote.<name>.url`
// sub-exec to read the remote URL from .git/config without triggering any
// url-rewriting (insteadOf), credential helpers, or protocol-handler exec.
//
// We use `git config --get` instead of `git remote get-url` because the
// latter routes through git_handle_repo, which is the CVE-2018-17456 attack
// surface (malicious `[remote] url = ext::sh -c <evil>` in .git/config would
// execute arbitrary shell). The CVE has been patched in modern git, but the
// defense-in-depth pattern keeps this immune to any future regression.
//
// GIT_ALLOW_PROTOCOL allowlists what git considers valid; GIT_TERMINAL_PROMPT=0
// prevents interactive auth prompts; GIT_CONFIG_NOSYSTEM=1 ignores /etc/gitconfig
// so a host-wide rewrite cannot influence the lookup.
func remoteURLViaConfigGet(ctx context.Context, cwd, remote string) (string, error) {
	args := []string{"-C", cwd, "config", "--get", "remote." + remote + ".url"}
	if cwd == "" {
		args = []string{"config", "--get", "remote." + remote + ".url"}
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_ALLOW_PROTOCOL=https:http:ssh:git",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("read remote url for %q: %w", remote, err)
	}
	return parseHostFromGitURL(strings.TrimSpace(string(out)))
}

// parseHostFromGitURL extracts the host[:port] from a git remote URL.
// Three forms are accepted:
//
//	https://host[:port]/...      (HTTPS — embedded userinfo REJECTED)
//	git@host:owner/repo.git      (scp-form SSH — bare user, no userinfo collision)
//	ssh://git@host[:port]/...    (full SSH — user MUST be "git" or absent)
//
// Embedded `https://user@host/...` is rejected as ambiguous: git treats `host`
// as the target but operators reading the URL often think `user` is the host.
// An attacker who controls `host` could trick a credential-host check into
// matching the wrong side. Refuse rather than guess.
func parseHostFromGitURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errURLMissingHost
	}

	// scp-form: `user@host:path`. Match BEFORE generic URL parsing because
	// `git@github.com:owner/repo` looks like `git@github.com:owner` host:port
	// to url.Parse, mis-bucketing the path into the port.
	if !strings.Contains(raw, "://") && strings.Contains(raw, ":") && strings.Contains(raw, "@") {
		at := strings.Index(raw, "@")
		colon := strings.Index(raw[at:], ":")
		if colon > 0 {
			host := raw[at+1 : at+colon]
			if host == "" {
				return "", errURLMissingHost
			}
			return host, nil
		}
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", errURLUnsupportedForm
	}
	// Reject embedded userinfo for HTTPS/HTTP.
	// Exception: ssh://git@host is the conventional and unambiguous SSH form;
	// we allow it only when user is empty or exactly "git" (no password).
	if u.User != nil {
		switch u.Scheme {
		case "https", "http":
			return "", errEmbeddedUserinfo
		case "ssh":
			if _, hasPass := u.User.Password(); hasPass {
				return "", errEmbeddedUserinfo
			}
			if name := u.User.Username(); name != "" && name != "git" {
				return "", errEmbeddedUserinfo
			}
		default:
			return "", errEmbeddedUserinfo
		}
	}
	return u.Host, nil
}

func init() {
	RegisterAdapter(gitAdapter{})
}
