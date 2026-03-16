package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// BuildDockerImageTool builds a Docker image from a Dockerfile using Docker CLI.
// Supports pushing to registries with credentials from the secure CLI store.
type BuildDockerImageTool struct {
	secureCLIStore store.SecureCLIStore
	workspace      string
}

func NewBuildDockerImageTool(secureCLIStore store.SecureCLIStore, workspace string) *BuildDockerImageTool {
	return &BuildDockerImageTool{
		secureCLIStore: secureCLIStore,
		workspace:      workspace,
	}
}

func (t *BuildDockerImageTool) Name() string { return "build_docker_image" }

func (t *BuildDockerImageTool) Description() string {
	return "Build a Docker image from a Dockerfile and optionally push to a container registry. " +
		"Uses Docker CLI (docker build/push). Supports Docker Hub, GHCR, ECR, and other OCI registries. " +
		"Use credential_name to authenticate with the registry via pre-configured secure CLI credentials."
}

func (t *BuildDockerImageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"context_dir": map[string]any{
				"type":        "string",
				"description": "Build context directory (relative to workspace). Must contain a Dockerfile.",
			},
			"image_name": map[string]any{
				"type":        "string",
				"description": "Full image name with optional tag (e.g., 'ghcr.io/org/mcp-server:v1', 'myapp:latest')",
			},
			"dockerfile": map[string]any{
				"type":        "string",
				"description": "Path to Dockerfile relative to context_dir (default: 'Dockerfile')",
			},
			"push": map[string]any{
				"type":        "boolean",
				"description": "Push the image to the registry after building (default: false)",
			},
			"credential_name": map[string]any{
				"type":        "string",
				"description": "Name of secure CLI credential for registry auth (e.g., 'docker', 'docker-ghcr'). Required for push.",
			},
			"build_args": map[string]any{
				"type":        "object",
				"description": "Build arguments as key-value pairs (passed as --build-arg)",
			},
			"platform": map[string]any{
				"type":        "string",
				"description": "Target platform (e.g., 'linux/amd64', 'linux/arm64'). Default: current platform.",
			},
			"no_cache": map[string]any{
				"type":        "boolean",
				"description": "Disable build cache (default: false)",
			},
		},
		"required": []string{"context_dir", "image_name"},
	}
}

func (t *BuildDockerImageTool) Execute(ctx context.Context, args map[string]any) *Result {
	contextDir, _ := args["context_dir"].(string)
	imageName, _ := args["image_name"].(string)
	dockerfile, _ := args["dockerfile"].(string)
	push, _ := args["push"].(bool)
	credentialName, _ := args["credential_name"].(string)
	noCache, _ := args["no_cache"].(bool)
	platform, _ := args["platform"].(string)

	if contextDir == "" {
		return ErrorResult("context_dir is required")
	}
	if imageName == "" {
		return ErrorResult("image_name is required")
	}
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	// Resolve context directory relative to workspace
	if !filepath.IsAbs(contextDir) {
		contextDir = filepath.Join(t.workspace, contextDir)
	}

	// Security: prevent path traversal outside workspace
	absCtx, err := filepath.Abs(contextDir)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid context_dir: %v", err))
	}
	absWorkspace, _ := filepath.Abs(t.workspace)
	if !strings.HasPrefix(absCtx, absWorkspace) {
		return ErrorResult("context_dir must be within the workspace directory")
	}

	// Verify context dir and Dockerfile exist
	dockerfilePath := filepath.Join(absCtx, dockerfile)
	if _, err := os.Stat(absCtx); os.IsNotExist(err) {
		return ErrorResult(fmt.Sprintf("context directory does not exist: %s", contextDir))
	}
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		return ErrorResult(fmt.Sprintf("Dockerfile not found: %s", dockerfilePath))
	}

	// Check Docker availability
	if err := checkDockerCLI(ctx); err != nil {
		return ErrorResult(fmt.Sprintf("Docker is not available: %v", err))
	}

	// Handle registry login if credential_name is provided
	var loginCleanup func()
	if credentialName != "" && push {
		cleanup, loginErr := t.registryLogin(ctx, credentialName, imageName)
		if loginErr != nil {
			return ErrorResult(fmt.Sprintf("registry login failed: %v", loginErr))
		}
		loginCleanup = cleanup
	}
	if loginCleanup != nil {
		defer loginCleanup()
	}

	// Build docker build command
	cmdArgs := []string{"build"}
	cmdArgs = append(cmdArgs, "-t", imageName)
	cmdArgs = append(cmdArgs, "-f", dockerfilePath)

	if platform != "" {
		cmdArgs = append(cmdArgs, "--platform", platform)
	}
	if noCache {
		cmdArgs = append(cmdArgs, "--no-cache")
	}

	// Add build args
	if ba, ok := args["build_args"]; ok {
		if baMap, ok := ba.(map[string]any); ok {
			for k, v := range baMap {
				cmdArgs = append(cmdArgs, "--build-arg", fmt.Sprintf("%s=%v", k, v))
			}
		}
	}

	cmdArgs = append(cmdArgs, absCtx)

	slog.Info("building docker image", "image", imageName, "context", absCtx, "dockerfile", dockerfile)

	// Run docker build with timeout
	buildCtx, buildCancel := context.WithTimeout(ctx, 10*time.Minute)
	defer buildCancel()

	buildCmd := exec.CommandContext(buildCtx, "docker", cmdArgs...)
	var buildStdout, buildStderr bytes.Buffer
	buildCmd.Stdout = &buildStdout
	buildCmd.Stderr = &buildStderr

	if err := buildCmd.Run(); err != nil {
		errOutput := buildStderr.String()
		if len(errOutput) > 2000 {
			errOutput = errOutput[len(errOutput)-2000:]
		}
		return ErrorResult(fmt.Sprintf("docker build failed: %v\n\nOutput:\n%s", err, errOutput))
	}

	result := fmt.Sprintf("Docker image built successfully: %s\n", imageName)

	// Get image ID
	inspectCmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Id}}", imageName)
	if digestOut, err := inspectCmd.Output(); err == nil {
		result += fmt.Sprintf("Image ID: %s\n", strings.TrimSpace(string(digestOut)))
	}

	// Get image size
	sizeCmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Size}}", imageName)
	if sizeOut, err := sizeCmd.Output(); err == nil {
		result += fmt.Sprintf("Size: %s bytes\n", strings.TrimSpace(string(sizeOut)))
	}

	// Push if requested
	if push {
		slog.Info("pushing docker image", "image", imageName)

		pushCtx, pushCancel := context.WithTimeout(ctx, 10*time.Minute)
		defer pushCancel()

		pushCmd := exec.CommandContext(pushCtx, "docker", "push", imageName)
		var pushStdout, pushStderr bytes.Buffer
		pushCmd.Stdout = &pushStdout
		pushCmd.Stderr = &pushStderr

		if err := pushCmd.Run(); err != nil {
			errOutput := pushStderr.String()
			if len(errOutput) > 2000 {
				errOutput = errOutput[len(errOutput)-2000:]
			}
			result += fmt.Sprintf("\nPush FAILED: %v\n%s", err, errOutput)
			return ErrorResult(result)
		}

		result += fmt.Sprintf("\nImage pushed successfully to registry: %s\n", imageName)
		pushOutput := pushStdout.String()
		if idx := strings.Index(pushOutput, "digest:"); idx >= 0 {
			digestLine := pushOutput[idx:]
			if end := strings.IndexByte(digestLine, '\n'); end > 0 {
				result += digestLine[:end] + "\n"
			}
		}
	}

	slog.Info("docker image build completed", "image", imageName, "pushed", push)
	return NewResult(result)
}

// checkDockerCLI verifies docker CLI is available.
func checkDockerCLI(ctx context.Context) error {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(checkCtx, "docker", "version", "--format", "{{.Server.Version}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker not available: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// registryLogin performs docker login using stored credentials.
// Returns a cleanup function to logout after the operation.
func (t *BuildDockerImageTool) registryLogin(ctx context.Context, credentialName, imageName string) (func(), error) {
	if t.secureCLIStore == nil {
		return nil, fmt.Errorf("secure CLI store not available — cannot use credential_name")
	}

	// LookupByBinary uses binary_name field to find credential.
	// The "docker" and "docker-ghcr" presets use "docker" as binary_name.
	binaryName := "docker"
	cred, err := t.secureCLIStore.LookupByBinary(ctx, binaryName, nil)
	if err != nil || cred == nil {
		return nil, fmt.Errorf("credential for binary %q not found (credential_name=%q): %v", binaryName, credentialName, err)
	}

	// EncryptedEnv is already decrypted by the PG store layer
	envMap := make(map[string]string)
	if len(cred.EncryptedEnv) > 0 {
		if err := json.Unmarshal(cred.EncryptedEnv, &envMap); err != nil {
			return nil, fmt.Errorf("failed to parse credential env vars: %w", err)
		}
	}

	// Determine registry and credentials based on credential name
	var username, password, registry string

	switch credentialName {
	case "docker-ghcr":
		username = envMap["GHCR_USERNAME"]
		password = envMap["GHCR_TOKEN"]
		registry = "ghcr.io"
	case "docker":
		username = envMap["DOCKER_USERNAME"]
		password = envMap["DOCKER_PASSWORD"]
		registry = "docker.io"
	default:
		// Try generic patterns
		for k, v := range envMap {
			kUpper := strings.ToUpper(k)
			if strings.Contains(kUpper, "USERNAME") || strings.Contains(kUpper, "USER") {
				username = v
			}
			if strings.Contains(kUpper, "PASSWORD") || strings.Contains(kUpper, "TOKEN") || strings.Contains(kUpper, "PAT") {
				password = v
			}
		}
		registry = extractRegistry(imageName)
	}

	if username == "" || password == "" {
		return nil, fmt.Errorf("credential %q does not contain username/password env vars", credentialName)
	}

	// docker login via stdin (secure, no password in process list)
	loginCmd := exec.CommandContext(ctx, "docker", "login", registry, "-u", username, "--password-stdin")
	loginCmd.Stdin = strings.NewReader(password)
	var loginStderr bytes.Buffer
	loginCmd.Stderr = &loginStderr

	if err := loginCmd.Run(); err != nil {
		return nil, fmt.Errorf("docker login to %s failed: %v (%s)", registry, err, loginStderr.String())
	}

	slog.Info("docker registry login successful", "registry", registry, "user", username)

	cleanup := func() {
		logoutCmd := exec.CommandContext(context.Background(), "docker", "logout", registry)
		if err := logoutCmd.Run(); err != nil {
			slog.Warn("docker logout failed", "registry", registry, "error", err)
		}
	}

	return cleanup, nil
}

// extractRegistry extracts the registry hostname from an image name.
// e.g. "ghcr.io/org/image:tag" → "ghcr.io", "myimage:tag" → "docker.io"
func extractRegistry(imageName string) string {
	name := imageName
	if idx := strings.LastIndex(name, ":"); idx > 0 {
		afterColon := name[idx+1:]
		if !strings.Contains(afterColon, "/") {
			name = name[:idx]
		}
	}

	parts := strings.SplitN(name, "/", 2)
	if len(parts) == 1 {
		return "docker.io"
	}
	if strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") {
		return parts[0]
	}
	return "docker.io"
}
