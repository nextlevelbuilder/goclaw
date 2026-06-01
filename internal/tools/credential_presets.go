package tools

import "sort"

// CLIPreset defines a built-in configuration template for a common CLI tool.
// Presets eliminate admin research friction by pre-filling env var names,
// deny patterns, timeout, and usage tips.
type CLIPreset struct {
	BinaryName  string      `json:"binary_name"`
	Description string      `json:"description"`
	EnvVars     []EnvVarDef `json:"env_vars"`
	DenyArgs    []string    `json:"deny_args"`
	DenyVerbose []string    `json:"deny_verbose"`
	Timeout     int         `json:"timeout"`
	Tips        string      `json:"tips"`
	// AdapterName defaults the binary row's adapter_name column at create
	// time. Empty (default) → passthrough adapter (legacy env injection).
	// Set to e.g. "git" by typed-credential presets (Phase 3+). Runtime
	// adapter lookup reads the DB column, not this field — so operator
	// overrides post-create take precedence.
	AdapterName string `json:"adapter_name,omitempty"`
}

// EnvVarDef describes an environment variable required by a CLI tool.
type EnvVarDef struct {
	Name     string `json:"name"`
	Desc     string `json:"desc"`
	IsFile   bool   `json:"is_file,omitempty"` // credential is a file path (e.g. GOOGLE_APPLICATION_CREDENTIALS)
	Optional bool   `json:"optional,omitempty"`
}

// CLIPresets contains built-in presets for common CLI tools.
var CLIPresets = map[string]CLIPreset{
	"gh": {
		BinaryName:  "gh",
		Description: "GitHub CLI",
		EnvVars:     []EnvVarDef{{Name: "GH_TOKEN", Desc: "GitHub PAT or App token"}},
		DenyArgs:    []string{`auth\s+`, `ssh-key`, `gpg-key`, `repo\s+delete`, `secret\s+`},
		DenyVerbose: []string{`--verbose`, `-v`},
		Timeout:     30,
		Tips:        "Use --json flag for structured output",
	},
	"gcloud": {
		BinaryName:  "gcloud",
		Description: "Google Cloud CLI",
		EnvVars: []EnvVarDef{
			{Name: "GOOGLE_APPLICATION_CREDENTIALS", Desc: "Service account JSON", IsFile: true},
		},
		DenyArgs:    []string{`iam\s+`, `auth\s+`, `projects\s+delete`, `services\s+disable`, `kms\s+`},
		DenyVerbose: []string{`--verbosity=debug`, `--log-http`},
		Timeout:     120,
		Tips:        "Use --format=json for structured output",
	},
	"gws": {
		BinaryName:  "gws",
		Description: "Google Workspace CLI",
		EnvVars: []EnvVarDef{
			{Name: "GOOGLE_WORKSPACE_CLI_CREDENTIALS_FILE", Desc: "Path to exported gws credentials or OAuth credentials JSON", IsFile: true},
			{Name: "GOOGLE_WORKSPACE_CLI_TOKEN", Desc: "Pre-obtained Google OAuth access token", Optional: true},
			{Name: "GOOGLE_WORKSPACE_CLI_CLIENT_ID", Desc: "OAuth client ID for manual auth flows", Optional: true},
			{Name: "GOOGLE_WORKSPACE_CLI_CLIENT_SECRET", Desc: "OAuth client secret for manual auth flows", Optional: true},
		},
		DenyArgs:    []string{`auth\s+(setup|login|export|logout)`},
		DenyVerbose: nil,
		Timeout:     120,
		Tips:        "Use --params JSON for query parameters, --json for request bodies, and --page-all for paginated reads. Prefer read/list/get commands unless an admin has approved write commands.",
	},
	"aws": {
		BinaryName:  "aws",
		Description: "AWS CLI",
		EnvVars: []EnvVarDef{
			{Name: "AWS_ACCESS_KEY_ID", Desc: "AWS access key"},
			{Name: "AWS_SECRET_ACCESS_KEY", Desc: "AWS secret key"},
			{Name: "AWS_DEFAULT_REGION", Desc: "AWS region", Optional: true},
		},
		DenyArgs:    []string{`iam\s+`, `organizations\s+`, `sts\s+assume`, `ec2\s+terminate`},
		DenyVerbose: []string{`--debug`},
		Timeout:     60,
		Tips:        "Use --output json for structured output",
	},
	"kubectl": {
		BinaryName:  "kubectl",
		Description: "Kubernetes CLI",
		EnvVars: []EnvVarDef{
			{Name: "KUBECONFIG", Desc: "Path to kubeconfig", IsFile: true},
		},
		DenyArgs:    []string{`delete\s+namespace`, `delete\s+node`, `drain\s+`, `cordon\s+`},
		DenyVerbose: nil,
		Timeout:     60,
		Tips:        "Use -o json for structured output",
	},
	"terraform": {
		BinaryName:  "terraform",
		Description: "Terraform CLI",
		EnvVars: []EnvVarDef{
			{Name: "TF_TOKEN_app_terraform_io", Desc: "Terraform Cloud token", Optional: true},
		},
		DenyArgs:    []string{`destroy`, `force-unlock`},
		DenyVerbose: nil,
		Timeout:     300,
		Tips:        "Use -json flag for structured output",
	},
	"git": {
		BinaryName:  "git",
		Description: "Git with credential adapter (PAT or SSH host-scoped credentials managed by goclaw)",
		// Credential storage is adapter-managed (encrypted_env carries the
		// typed blob), not env-paste. Keep EnvVars empty so the UI doesn't
		// offer a free-text PAT field that would land in plain env.
		EnvVars: nil,
		// Deny patterns block the agent from:
		//   - persisting tokens via `git config --global/--system`
		//   - installing a leaking credential helper
		//   - starting an unauthenticated git daemon
		//   - overriding the adapter's host-scoped `http.*` header via `-c`
		//   - shadowing core.sshCommand to bypass the adapter's SSH wrapper
		// Patterns are case-insensitive because git config keys themselves are.
		DenyArgs: []string{
			`(?i)config\s+(--global|--system)`,
			`(?i)credential-helper`,
			`(?i)\bdaemon\b`,
			`(?i)-c\s+http\.`,
			`(?i)-c\s+credential\.`,
			`(?i)-c\s+core\.sshcommand`,
		},
		DenyVerbose: nil,
		Timeout:     300,
		Tips:        "Adapter handles auth automatically for clone/fetch/pull/push/submodule based on stored credential type and host scope.",
		AdapterName: "git",
	},
	"psql": {
		BinaryName:  "psql",
		Description: "PostgreSQL CLI — framework-validation preset for the typed-credential adapter (Phase 2b). UI cred-type picker lands in v2; until then operators wire `pg_password_file` credentials via API.",
		EnvVars: []EnvVarDef{
			{Name: "PGPASSFILE", Desc: "Path to .pgpass file (auto-materialized by adapter when credential_type='pg_password_file')", IsFile: true, Optional: true},
		},
		DenyArgs:    []string{`-c\s+["']?(DROP|TRUNCATE)\b`, `\\!`, `\\copy\s+.*FROM\s+PROGRAM`},
		DenyVerbose: nil,
		Timeout:     60,
		Tips:        "Use -A -t for plain output suitable for piping",
		AdapterName: "psql",
	},
}

// GetPreset returns a preset by name, or nil if not found.
func GetPreset(name string) *CLIPreset {
	p, ok := CLIPresets[name]
	if !ok {
		return nil
	}
	return &p
}

// ListPresetNames returns all available preset names sorted alphabetically.
func ListPresetNames() []string {
	names := make([]string, 0, len(CLIPresets))
	for name := range CLIPresets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
