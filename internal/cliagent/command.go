package cliagent

import (
	"errors"
	"fmt"
	"strings"
)

// PermissionMode is how much the delegated CLI may do on its own.
type PermissionMode string

const (
	// PermissionAuto lets the CLI act without asking — safe only because the run
	// is confined to our sandbox container.
	PermissionAuto PermissionMode = "auto"
	// PermissionManual asks before acting. Not every CLI can do this; Command
	// fails rather than quietly downgrading to auto.
	PermissionManual PermissionMode = "manual"
)

// ErrManualApprovalUnsupported is returned by Command when manual approval is
// requested from a provider whose Spec has no ManualApproveArgs. Callers should
// surface it to the user instead of retrying in auto mode.
var ErrManualApprovalUnsupported = errors.New("manual approval not supported")

// ErrNoCredentialTarget is returned by ApplyCredential when neither the
// credential's inject descriptor nor the Spec's CredEnv says where the secret
// should go.
var ErrNoCredentialTarget = errors.New("no credential target")

// Command renders the full argv for one run: binary, the task template with
// TaskPlaceholder substituted, the fixed extra args, then the permission-mode
// args.
//
// The mode args come last so a per-connection override of ExtraArgs can never
// displace them.
func (s Spec) Command(task string, mode PermissionMode) ([]string, error) {
	if strings.TrimSpace(task) == "" {
		return nil, errors.New("task is empty — nothing to delegate")
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}

	who := s.Provider
	if who == "" {
		who = s.Binary
	}

	var modeArgs []string
	switch mode {
	case PermissionAuto:
		modeArgs = s.AutoApproveArgs
	case PermissionManual:
		if len(s.ManualApproveArgs) == 0 {
			return nil, fmt.Errorf("%w for %q: this CLI has no ask-before-acting flag, so it can only run in auto mode — run it with %q, or set %q on the connection if the CLI does support one",
				ErrManualApprovalUnsupported, who, PermissionAuto, "manual_approve_args")
		}
		modeArgs = s.ManualApproveArgs
	default:
		return nil, fmt.Errorf("unknown permission mode %q for %q — use %q or %q", string(mode), who, PermissionAuto, PermissionManual)
	}

	argv := make([]string, 0, 1+len(s.TaskArgs)+len(s.ExtraArgs)+len(modeArgs))
	argv = append(argv, s.Binary)
	for _, a := range s.TaskArgs {
		argv = append(argv, strings.ReplaceAll(a, TaskPlaceholder, task))
	}
	argv = append(argv, s.ExtraArgs...)
	argv = append(argv, modeArgs...)
	return argv, nil
}

// SupportsManualApproval reports whether this Spec can ask before acting, so a
// caller can offer the choice honestly (or explain why it can't).
func (s Spec) SupportsManualApproval() bool { return len(s.ManualApproveArgs) > 0 }

// ApplyCredential places a connection's secret into env.
//
// inject is the credential store's delivery descriptor
// (store.CLIConnectionCredential.Inject): "env:VAR" names the variable
// explicitly and wins, because it was recorded when the credential was captured
// (an OAuth token and an API key for the same provider go to different vars).
// When inject is empty, the first entry of the Spec's CredEnv is used — that is
// the provider's preferred variable.
//
// The secret is never logged, returned, or included in any error message.
func (s Spec) ApplyCredential(secret string, inject string, env map[string]string) error {
	if strings.TrimSpace(secret) == "" {
		return errors.New("credential secret is empty")
	}
	if env == nil {
		return errors.New("env map is nil — pass an initialised map to receive the credential")
	}

	who := s.Provider
	if who == "" {
		who = s.Binary
	}

	target := ""
	if d := strings.TrimSpace(inject); d != "" {
		kind, name, ok := strings.Cut(d, ":")
		kind = strings.ToLower(strings.TrimSpace(kind))
		name = strings.TrimSpace(name)
		switch {
		case ok && kind == "env" && name != "":
			target = name
		case kind == "file":
			// file:PATH injection (Codex/Gemini credential files) needs the runtime
			// to write into the sandbox home before exec; not wired yet. Fail loudly
			// rather than dropping the credential silently.
			return fmt.Errorf("credential injection %q is not supported yet for %q — use an env:VAR credential, or set %q on the connection", "file:PATH", who, "cred_env")
		default:
			return fmt.Errorf("credential injection descriptor is malformed for %q — expected %q", who, "env:VAR")
		}
	} else if len(s.CredEnv) > 0 {
		target = strings.TrimSpace(s.CredEnv[0])
	}

	if target == "" {
		return fmt.Errorf("%w for %q: the credential has no env:VAR descriptor and the connection config sets no %q — add e.g. {\"cred_env\":[\"MYCLI_API_KEY\"]}",
			ErrNoCredentialTarget, who, "cred_env")
	}
	env[target] = secret
	return nil
}
