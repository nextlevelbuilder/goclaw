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
}

// EnvVarDef describes an environment variable required by a CLI tool.
type EnvVarDef struct {
	Name     string `json:"name"`
	Desc     string `json:"desc"`
	IsFile   bool   `json:"is_file,omitempty"`   // credential is a file path (e.g. GOOGLE_APPLICATION_CREDENTIALS)
	Optional bool   `json:"optional,omitempty"`
}

// CLIPresets contains built-in presets for common CLI tools.
var CLIPresets = map[string]CLIPreset{
	"gh": {
		BinaryName:  "gh",
		Description: "GitHub CLI",
		EnvVars:     []EnvVarDef{{Name: "GH_TOKEN", Desc: "GitHub Personal Access Token (classic or fine-grained)"}},
		DenyArgs:    []string{`auth\s+`, `ssh-key`, `gpg-key`, `repo\s+delete`, `secret\s+`},
		DenyVerbose: []string{`--verbose`, `-v`},
		Timeout:     30,
		Tips: "How to get GH_TOKEN: Go to github.com → Settings → Developer settings → Personal access tokens → Generate new token. " +
			"Select scopes: repo, read:org, workflow. Copy the token (ghp_...). " +
			"Use --json flag for structured output.",
	},
	"gcloud": {
		BinaryName:  "gcloud",
		Description: "Google Cloud CLI",
		EnvVars: []EnvVarDef{
			{Name: "GOOGLE_APPLICATION_CREDENTIALS", Desc: "Path to service account JSON key file", IsFile: true},
		},
		DenyArgs:    []string{`iam\s+`, `auth\s+`, `projects\s+delete`, `services\s+disable`, `kms\s+`},
		DenyVerbose: []string{`--verbosity=debug`, `--log-http`},
		Timeout:     120,
		Tips: "How to get credentials: Go to GCP Console → IAM & Admin → Service Accounts → Create/select account → Keys → Add Key → JSON. " +
			"Download the JSON file and upload it here. " +
			"Use --format=json for structured output.",
	},
	"aws": {
		BinaryName:  "aws",
		Description: "AWS CLI",
		EnvVars: []EnvVarDef{
			{Name: "AWS_ACCESS_KEY_ID", Desc: "AWS access key ID (starts with AKIA...)"},
			{Name: "AWS_SECRET_ACCESS_KEY", Desc: "AWS secret access key"},
			{Name: "AWS_DEFAULT_REGION", Desc: "AWS region (e.g. us-east-1, ap-southeast-1)", Optional: true},
		},
		DenyArgs:    []string{`iam\s+`, `organizations\s+`, `sts\s+assume`, `ec2\s+terminate`},
		DenyVerbose: []string{`--debug`},
		Timeout:     60,
		Tips: "How to get credentials: Go to AWS Console → IAM → Users → select user → Security credentials → Create access key. " +
			"Copy Access key ID and Secret access key. Recommended: create a dedicated IAM user with least-privilege policy. " +
			"Use --output json for structured output.",
	},
	"kubectl": {
		BinaryName:  "kubectl",
		Description: "Kubernetes CLI",
		EnvVars: []EnvVarDef{
			{Name: "KUBECONFIG", Desc: "Path to kubeconfig file (usually ~/.kube/config)", IsFile: true},
		},
		DenyArgs:    []string{`delete\s+namespace`, `delete\s+node`, `drain\s+`, `cordon\s+`},
		DenyVerbose: nil,
		Timeout:     60,
		Tips: "How to get kubeconfig: Run 'kubectl config view --raw > kubeconfig.yaml' on a machine with cluster access, then upload the file. " +
			"For cloud: AWS EKS → 'aws eks update-kubeconfig', GKE → 'gcloud container clusters get-credentials', AKS → 'az aks get-credentials'. " +
			"Use -o json for structured output.",
	},
	"terraform": {
		BinaryName:  "terraform",
		Description: "Terraform CLI",
		EnvVars: []EnvVarDef{
			{Name: "TF_TOKEN_app_terraform_io", Desc: "Terraform Cloud / HCP Terraform API token", Optional: true},
		},
		DenyArgs:    []string{`destroy`, `force-unlock`},
		DenyVerbose: nil,
		Timeout:     300,
		Tips: "How to get token: Go to app.terraform.io → User Settings → Tokens → Create an API token. " +
			"Or run 'terraform login' locally and copy the token from ~/.terraform.d/credentials.tfrc.json. " +
			"Use -json flag for structured output.",
	},
	"docker": {
		BinaryName:  "docker",
		Description: "Docker CLI (Docker Hub)",
		EnvVars: []EnvVarDef{
			{Name: "DOCKER_USERNAME", Desc: "Docker Hub username (hub.docker.com)"},
			{Name: "DOCKER_PASSWORD", Desc: "Docker Hub access token (recommended) or password"},
		},
		DenyArgs:    []string{`system\s+prune`, `volume\s+prune`, `network\s+prune`, `container\s+prune`, `--privileged`},
		DenyVerbose: []string{`--debug`, `-D`},
		Timeout:     300,
		Tips: "How to get credentials: Go to hub.docker.com → Account Settings → Security → New Access Token. " +
			"Set permissions: Read/Write/Delete as needed. Copy the token (dckr_pat_...). " +
			"DOCKER_USERNAME is your Docker Hub username (not email). " +
			"Login: echo $DOCKER_PASSWORD | docker login -u $DOCKER_USERNAME --password-stdin. " +
			"Push: docker push <username>/<image>:<tag>. Use 'docker buildx' for multi-arch builds.",
	},
	"docker-ghcr": {
		BinaryName:  "docker",
		Description: "Docker CLI (GitHub Container Registry)",
		EnvVars: []EnvVarDef{
			{Name: "GHCR_USERNAME", Desc: "GitHub username (not email)"},
			{Name: "GHCR_TOKEN", Desc: "GitHub PAT with write:packages and read:packages scopes"},
		},
		DenyArgs:    []string{`system\s+prune`, `volume\s+prune`, `network\s+prune`, `container\s+prune`, `--privileged`},
		DenyVerbose: []string{`--debug`, `-D`},
		Timeout:     300,
		Tips: "How to get GHCR_TOKEN: Go to github.com → Settings → Developer settings → Personal access tokens → Generate new token (classic). " +
			"Select scopes: write:packages, read:packages, delete:packages (optional). Copy the token (ghp_...). " +
			"GHCR_USERNAME is your GitHub username. " +
			"Login: echo $GHCR_TOKEN | docker login ghcr.io -u $GHCR_USERNAME --password-stdin. " +
			"Push: docker tag <image> ghcr.io/<owner>/<image>:<tag> && docker push ghcr.io/<owner>/<image>:<tag>.",
	},
	"helm": {
		BinaryName:  "helm",
		Description: "Helm (Kubernetes package manager)",
		EnvVars: []EnvVarDef{
			{Name: "KUBECONFIG", Desc: "Path to kubeconfig file (same as kubectl)", IsFile: true},
			{Name: "HELM_REGISTRY_USERNAME", Desc: "OCI registry username (Docker Hub, GHCR, ECR, etc.)", Optional: true},
			{Name: "HELM_REGISTRY_PASSWORD", Desc: "OCI registry password or token", Optional: true},
		},
		DenyArgs:    []string{`repo\s+remove`, `uninstall.*--no-hooks`},
		DenyVerbose: []string{`--debug`},
		Timeout:     120,
		Tips: "KUBECONFIG: same as kubectl — export from existing cluster access (see kubectl preset tips). " +
			"HELM_REGISTRY_USERNAME/PASSWORD: only needed for pushing charts to OCI registries. " +
			"For Docker Hub: use Docker Hub username + access token (dckr_pat_...). " +
			"For GHCR: use GitHub username + PAT with write:packages scope (ghp_...). " +
			"For AWS ECR: use 'AWS' as username + token from 'aws ecr get-login-password'. " +
			"Login: echo $HELM_REGISTRY_PASSWORD | helm registry login <registry> -u $HELM_REGISTRY_USERNAME --password-stdin. " +
			"Use -o json for structured output.",
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
