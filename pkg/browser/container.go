package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ContainerEngine runs Chrome in a Docker container per session.
// It delegates all Page/Engine operations to an inner ChromeEngine connected via CDP.
type ContainerEngine struct {
	image       string // e.g. "zenika/alpine-chrome:latest"
	containerID string
	cdpPort     int
	inner       Engine // ChromeEngine connected to the container
	mu          sync.Mutex
	logger      *slog.Logger

	// Resource limits (set via ContainerOpt)
	memoryMB int     // --memory flag (MB), 0 = no limit
	cpuLimit float64 // --cpus flag, 0 = no limit
	network  string  // --network flag, "" = default
}

// ContainerOpt configures a ContainerEngine.
type ContainerOpt func(*ContainerEngine)

// WithContainerMemory sets the memory limit per container in MB.
func WithContainerMemory(mb int) ContainerOpt {
	return func(e *ContainerEngine) { e.memoryMB = mb }
}

// WithContainerCPU sets the CPU core limit per container.
func WithContainerCPU(cpu float64) ContainerOpt {
	return func(e *ContainerEngine) { e.cpuLimit = cpu }
}

// WithContainerNetwork sets the Docker network name for the container.
func WithContainerNetwork(network string) ContainerOpt {
	return func(e *ContainerEngine) { e.network = network }
}

// DefaultContainerImage is the default Docker image for container engine.
// chromedp/headless-shell is lightweight and fast but uses --headless=old which
// limits stealth (navigator.webdriver stays true despite flags).
// For full stealth, use StealthContainerImage (goclaw/chromium) which runs
// --headless=new and properly supports --disable-blink-features.
const DefaultContainerImage = "chromedp/headless-shell:latest"

// StealthContainerImage is a full Chromium image with --headless=new support.
// Use this when anti-bot stealth is required (e.g. Google, Cloudflare sites).
const StealthContainerImage = "goclaw/chromium:latest"

// NewContainerEngine creates a ContainerEngine that will use the given Docker image.
func NewContainerEngine(image string, logger *slog.Logger, opts ...ContainerOpt) *ContainerEngine {
	if logger == nil {
		logger = slog.Default()
	}
	if image == "" {
		image = DefaultContainerImage
	}
	e := &ContainerEngine{
		image:  image,
		logger: logger,
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *ContainerEngine) Launch(opts LaunchOpts) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// If container is still running, try to reconnect instead of recreating
	if e.containerID != "" && e.cdpPort > 0 && isContainerRunning(e.containerID) {
		if e.inner != nil {
			e.inner.Close()
		}
		inner := NewChromeEngine(e.logger)
		if err := inner.Launch(LaunchOpts{
			RemoteURL: fmt.Sprintf("ws://127.0.0.1:%d", e.cdpPort),
		}); err == nil {
			e.inner = inner
			e.logger.Info("reconnected to existing container", "id", e.containerID[:min(12, len(e.containerID))], "port", e.cdpPort)
			return nil
		}
		e.logger.Warn("existing container unreachable, recreating")
	}

	// Cleanup any previous container
	if e.inner != nil {
		e.inner.Close()
		e.inner = nil
	}
	e.stopContainer()

	// Build docker run command.
	// Let Docker pick the host port (-p 127.0.0.1::9222) to avoid TOCTOU race
	// with freePort(). We read the actual assigned port via `docker port` after start.
	hasSocatEntrypoint := strings.Contains(e.image, "goclaw/") || strings.Contains(e.image, "headless-shell")
	isZenika := strings.Contains(e.image, "zenika/alpine-chrome")

	args := []string{
		"run", "-d",
		"-p", "127.0.0.1::9222",
		"--shm-size=512m", // Chrome needs shared memory for rendering
	}

	// Resource limits
	if e.memoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", e.memoryMB))
	}
	if e.cpuLimit > 0 {
		args = append(args, "--cpus", fmt.Sprintf("%.2f", e.cpuLimit))
	}
	if e.network != "" {
		args = append(args, "--network", e.network)
	}

	// Volume mount for profile persistence
	if opts.ProfileDir != "" {
		args = append(args, "-v", opts.ProfileDir+":/data/profile")
		args = append(args, "--label", "goclaw.profile="+opts.ProfileDir)
	}

	// Force English locale inside container — prevents leaking host locale
	args = append(args, "-e", "LANG=en_US.UTF-8", "-e", "LANGUAGE=en_US:en")

	// Pass proxy if configured (strip credentials — Chrome doesn't use them)
	if opts.ProxyURL != "" {
		args = append(args, "-e", "CHROME_PROXY="+stripUserinfo(opts.ProxyURL))
	}

	// Chrome user-data-dir flag when profile is mounted
	userDataDirFlag := ""
	if opts.ProfileDir != "" {
		userDataDirFlag = "--user-data-dir=/data/profile"
		// Remove Chromium singleton lock files from previous container.
		// Each container has a different hostname, so Chromium sees stale locks
		// as "profile in use by another computer" and exits with code 21.
		for _, lockFile := range []string{"SingletonLock", "SingletonCookie", "SingletonSocket"} {
			os.Remove(opts.ProfileDir + "/" + lockFile)
		}
	}

	// Stealth flags — passed as Chrome command-line args inside the container.
	containerStealthFlags := []string{
		"--disable-blink-features=AutomationControlled",
		"--disable-features=AutomationControlled,TranslateUI,EnableAutomation",
		"--disable-infobars",
		"--disable-background-networking",
		"--disable-client-side-phishing-detection",
		"--disable-default-apps",
		"--disable-hang-monitor",
		"--disable-popup-blocking",
		"--disable-prompt-on-repost",
		"--disable-sync",
		"--disable-extensions",
		"--disable-component-extensions-with-background-pages",
		"--metrics-recording-only",
		"--no-first-run",
		"--password-store=basic",
		"--use-mock-keychain",
		"--enforce-webrtc-ip-permission-check",
		"--disable-webrtc-hw-decoding",
		"--disable-webrtc-hw-encoding",
		"--window-size=1920,1080",
		"--lang=en-US",
		"--accept-lang=en-US,en",
	}

	switch {
	case hasSocatEntrypoint:
		// goclaw/chromium and headless-shell: entrypoint runs socat proxy
		// (0.0.0.0:9222 → 127.0.0.1:9223) then exec's Chrome with $@ appended.
		args = append(args, e.image)
		args = append(args, containerStealthFlags...)
		if userDataDirFlag != "" {
			args = append(args, userDataDirFlag)
		}
	case isZenika:
		// Override entrypoint to bind debugger to 0.0.0.0
		chromeArgs := []string{
			"--no-sandbox",
			"--disable-dev-shm-usage",
			"--disable-gpu",
			"--headless=new",
			"--remote-debugging-address=0.0.0.0",
			"--remote-debugging-port=9222",
		}
		chromeArgs = append(chromeArgs, containerStealthFlags...)
		if userDataDirFlag != "" {
			chromeArgs = append(chromeArgs, userDataDirFlag)
		}
		args = append(args, "--entrypoint", "chromium-browser", e.image)
		args = append(args, chromeArgs...)
	default:
		// Generic image: pass all Chrome flags explicitly
		chromeArgs := []string{
			"--no-sandbox",
			"--disable-dev-shm-usage",
			"--disable-gpu",
			"--headless=new",
			"--remote-debugging-address=0.0.0.0",
			"--remote-debugging-port=9222",
		}
		chromeArgs = append(chromeArgs, containerStealthFlags...)
		if userDataDirFlag != "" {
			chromeArgs = append(chromeArgs, userDataDirFlag)
		}
		args = append(args, e.image)
		args = append(args, chromeArgs...)
	}
	// Strip credentials from proxy URL — Chrome --proxy-server doesn't support user:pass@
	proxyForChrome := stripUserinfo(opts.ProxyURL)
	if proxyForChrome != "" {
		args = append(args, "--proxy-server="+proxyForChrome)
	}

	// Start container (redact proxy credentials from log)
	cmd := exec.Command("docker", args...)
	logArgs := make([]string, len(args))
	copy(logArgs, args)
	for i, a := range logArgs {
		if strings.HasPrefix(a, "CHROME_PROXY=") || strings.HasPrefix(a, "--proxy-server=") {
			logArgs[i] = "[PROXY_REDACTED]"
		}
	}
	e.logger.Info("docker run command", "cmd", "docker "+strings.Join(logArgs, " "))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	e.containerID = strings.TrimSpace(string(out))

	// Read the actual host port Docker assigned via `docker port`.
	port, err := e.readAssignedPort()
	if err != nil {
		e.logger.Error("failed to read Docker-assigned port", "error", err,
			"container", e.containerID[:min(12, len(e.containerID))])
		e.dumpContainerLogs()
		e.stopContainer()
		return fmt.Errorf("read assigned port: %w", err)
	}
	e.cdpPort = port

	if len(e.containerID) > 12 {
		e.logger.Info("container started", "id", e.containerID[:12], "port", port, "image", e.image)
	}

	// Wait for CDP to be ready
	cdpURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := waitForCDP(cdpURL, 60*time.Second, e.logger, e.containerID); err != nil {
		e.logger.Error("CDP not ready after 60s — killing container",
			"port", port, "image", e.image, "container", e.containerID[:min(12, len(e.containerID))])
		e.dumpContainerLogs()
		e.stopContainer()
		return fmt.Errorf("wait for CDP: %w", err)
	}

	// Create inner ChromeEngine connected via remote CDP
	inner := NewChromeEngine(e.logger)
	remoteWS := fmt.Sprintf("ws://127.0.0.1:%d", port)
	if err := inner.Launch(LaunchOpts{
		RemoteURL: remoteWS,
	}); err != nil {
		e.logger.Error("rod WebSocket connection to container failed — killing container",
			"port", port, "remoteURL", remoteWS, "error", err,
			"container", e.containerID[:min(12, len(e.containerID))])
		e.dumpContainerLogs()
		e.stopContainer()
		return fmt.Errorf("connect to container Chrome: %w", err)
	}
	e.inner = inner
	return nil
}

func (e *ContainerEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var innerErr error
	if e.inner != nil {
		innerErr = e.inner.Close()
		e.inner = nil
	}
	e.stopContainer()
	return innerErr
}

// isContainerRunning checks if a Docker container is still running.
func isContainerRunning(containerID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", containerID).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// readAssignedPort reads the host port Docker assigned for container port 9222.
func (e *ContainerEngine) readAssignedPort() (int, error) {
	if e.containerID == "" {
		return 0, fmt.Errorf("no container ID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "port", e.containerID, "9222").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker port: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	// Output format: "127.0.0.1:12345\n" or "0.0.0.0:12345\n[::]:12345\n"
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	_, portStr, splitErr := net.SplitHostPort(line)
	if splitErr != nil {
		return 0, fmt.Errorf("parse docker port output %q: %w", line, splitErr)
	}
	port, err := net.LookupPort("tcp", portStr)
	if err != nil {
		return 0, fmt.Errorf("parse port %q: %w", portStr, err)
	}
	return port, nil
}

// dumpContainerLogs fetches and logs the last 30 lines of container stderr/stdout.
func (e *ContainerEngine) dumpContainerLogs() {
	if e.containerID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	logOut, err := exec.CommandContext(ctx, "docker", "logs", "--tail", "30", e.containerID).CombinedOutput()
	if err != nil {
		e.logger.Warn("failed to fetch container logs", "error", err)
		return
	}
	e.logger.Error("container logs (last 30 lines)", "logs", strings.TrimSpace(string(logOut)))
}

func (e *ContainerEngine) stopContainer() {
	if e.containerID == "" {
		return
	}
	id := e.containerID
	if len(id) > 12 {
		id = id[:12]
	}
	// Use a timeout to prevent docker rm -f from blocking shutdown indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", e.containerID)
	if err := cmd.Run(); err != nil {
		e.logger.Warn("failed to remove container", "id", id, "error", err)
	} else {
		e.logger.Info("container removed", "id", id)
	}
	e.containerID = ""
}

func (e *ContainerEngine) NewPage(ctx context.Context, url string) (Page, error) {
	if e.inner == nil {
		return nil, fmt.Errorf("container engine not running")
	}
	return e.inner.NewPage(ctx, url)
}

func (e *ContainerEngine) Pages() ([]Page, error) {
	if e.inner == nil {
		return nil, fmt.Errorf("container engine not running")
	}
	return e.inner.Pages()
}

func (e *ContainerEngine) Incognito() (Engine, error) {
	if e.inner == nil {
		return nil, fmt.Errorf("container engine not running")
	}
	return e.inner.Incognito()
}

func (e *ContainerEngine) IsConnected() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.inner != nil && e.inner.IsConnected() {
		return true
	}

	// Inner connection lost but container may still be running — try to reconnect
	if e.containerID != "" && e.cdpPort > 0 {
		if isContainerRunning(e.containerID) {
			e.logger.Info("container still running, reconnecting inner engine", "port", e.cdpPort)
			if e.inner != nil {
				e.inner.Close()
			}
			inner := NewChromeEngine(e.logger)
			if err := inner.Launch(LaunchOpts{
				RemoteURL: fmt.Sprintf("ws://127.0.0.1:%d", e.cdpPort),
			}); err == nil {
				e.inner = inner
				return true
			}
			e.logger.Warn("reconnect to container failed", "port", e.cdpPort)
		}
	}
	return false
}

func (e *ContainerEngine) Name() string { return "container" }

// freePort finds an available TCP port.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

// waitForCDP polls the Chrome /json/version endpoint until ready or timeout.
// containerID is optional — if provided, enables mid-poll container health checks.
func waitForCDP(baseURL string, timeout time.Duration, logger *slog.Logger, containerID ...string) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	url := baseURL + "/json/version"

	var lastErr error
	attempts := 0
	connRefusedCount := 0
	cid := ""
	if len(containerID) > 0 {
		cid = containerID[0]
	}
	for time.Now().Before(deadline) {
		attempts++
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "connection refused") {
				connRefusedCount++
			}
			// Log first 3 attempts + every 10th at WARN level so it's visible at default log level
			if attempts <= 3 || attempts%10 == 0 {
				if logger != nil {
					logger.Warn("CDP poll failed", "attempt", attempts, "url", url, "error", err, "connRefused", connRefusedCount)
				}
			}
			// After 10s of connection refused, check if container is still alive
			if connRefusedCount == 20 && cid != "" {
				alive := isContainerRunning(cid)
				if logger != nil {
					logger.Warn("CDP connection refused for 10s — container health check",
						"containerID", cid[:min(12, len(cid))], "alive", alive)
				}
				if !alive {
					// Container dead — dump logs before returning (no --rm so container stays)
					ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
					logOut, logErr := exec.CommandContext(ctx2, "docker", "logs", "--tail", "50", cid).CombinedOutput()
					cancel2()
					if logErr == nil {
						logger.Error("DEAD container logs", "logs", strings.TrimSpace(string(logOut)))
					} else {
						logger.Error("failed to get dead container logs", "error", logErr)
					}
					// Also get exit code
					ctx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Second)
					exitOut, _ := exec.CommandContext(ctx3, "docker", "inspect", "-f", "{{.State.ExitCode}} {{.State.Error}}", cid).CombinedOutput()
					cancel3()
					logger.Error("container exit info", "info", strings.TrimSpace(string(exitOut)))
					return fmt.Errorf("container %s died during CDP wait (connection refused x%d)", cid[:min(12, len(cid))], connRefusedCount)
				}
				// Container alive but port not reachable — check port mapping from Docker side
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				portOut, portErr := exec.CommandContext(ctx, "docker", "port", cid, "9222").CombinedOutput()
				cancel()
				if logger != nil {
					logger.Warn("CDP mid-poll port check", "dockerPort", strings.TrimSpace(string(portOut)), "error", portErr)
				}
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var result map[string]any
		if jsonErr := json.Unmarshal(body, &result); jsonErr != nil {
			lastErr = fmt.Errorf("invalid JSON from CDP: %s (body: %s)", jsonErr, string(body[:min(200, len(body))]))
			if logger != nil {
				logger.Warn("CDP poll: invalid JSON", "attempt", attempts, "body", string(body[:min(200, len(body))]))
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if result["webSocketDebuggerUrl"] != nil {
			if logger != nil {
				logger.Info("CDP ready", "url", baseURL, "attempts", attempts)
			}
			return nil
		}
		lastErr = fmt.Errorf("missing webSocketDebuggerUrl in response: %s", string(body[:min(200, len(body))]))
		if logger != nil {
			logger.Warn("CDP poll: missing webSocketDebuggerUrl", "attempt", attempts, "response", string(body[:min(200, len(body))]))
		}
		time.Sleep(500 * time.Millisecond)
	}
	if connRefusedCount == attempts {
		return fmt.Errorf("CDP not ready after %s (%d attempts, ALL connection refused — port %s likely not mapped or socat not running)", timeout, attempts, url)
	}
	return fmt.Errorf("CDP not ready after %s (%d attempts, %d connection refused, last error: %v)", timeout, attempts, connRefusedCount, lastErr)
}

// ensureImage checks if the Docker image exists locally and auto-builds it if
// it's a goclaw/* image. Called once at pool startup so individual containers
// don't race to build the same image.
func ensureImage(image string, logger *slog.Logger) error {
	if imageExists(image) {
		return nil
	}
	if !strings.HasPrefix(image, "goclaw/") {
		return fmt.Errorf("image %s not found locally (pull it first)", image)
	}
	logger.Info("building container image (first run, may take a few minutes)", "image", image)
	if err := buildGoclawChromium(image); err != nil {
		return fmt.Errorf("auto-build %s: %w", image, err)
	}
	logger.Info("container image built successfully", "image", image)
	return nil
}

// imageExists checks if a Docker image is available locally.
func imageExists(image string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "image", "inspect", image).Run() == nil
}

// goclawChromiumDockerfile is the embedded Dockerfile for goclaw/chromium.
// Full Chromium with --headless=new + socat proxy on 0.0.0.0:9222.
const goclawChromiumDockerfile = `FROM debian:bookworm-slim
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      chromium socat fonts-liberation fonts-noto-color-emoji \
      libegl1-mesa libgl1-mesa-dri libgles2-mesa \
      locales && \
    sed -i 's/# en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen && locale-gen && \
    rm -rf /var/lib/apt/lists/*
ENV LANG=en_US.UTF-8 LANGUAGE=en_US:en
EXPOSE 9222
ENTRYPOINT ["sh", "-c", "\
  socat TCP4-LISTEN:9222,fork,reuseaddr TCP4:127.0.0.1:9223 & \
  exec chromium \
    --headless=new \
    --no-sandbox \
    --disable-dev-shm-usage \
    --disable-gpu \
    --disable-software-rasterizer \
    --disable-dbus \
    --disable-background-networking \
    --disable-component-update \
    --remote-debugging-port=9223 \
    \"$@\"", "--"]
`

// buildGoclawChromium builds the goclaw/chromium image from the embedded Dockerfile.
func buildGoclawChromium(image string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", image, "-f", "-", ".")
	cmd.Stdin = strings.NewReader(goclawChromiumDockerfile)
	cmd.Dir = os.TempDir() // context dir doesn't matter — Dockerfile uses no COPY/ADD
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
