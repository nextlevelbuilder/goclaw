package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	podReadyTimeout    = 120 * time.Second
	defaultK8sNS       = "goclaw-sandbox"
	k8sLabelSandbox    = "goclaw.sandbox"
	k8sContainerName   = "sandbox"
	k8sPodPrefix       = "goclaw-sbx-"
)

// CheckK8sAvailable verifies K8s cluster connectivity.
// Tries kubeconfigPath first, then KUBECONFIG env, then InClusterConfig.
func CheckK8sAvailable(ctx context.Context, kubeconfigPath string) (*kubernetes.Clientset, *rest.Config, error) {
	var restCfg *rest.Config
	var err error

	switch {
	case kubeconfigPath != "":
		restCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, nil, fmt.Errorf("build config from kubeconfig %q: %w", kubeconfigPath, err)
		}
	case os.Getenv("KUBECONFIG") != "":
		restCfg, err = clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
		if err != nil {
			return nil, nil, fmt.Errorf("build config from KUBECONFIG env: %w", err)
		}
	default:
		restCfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, nil, fmt.Errorf("in-cluster config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create clientset: %w", err)
	}

	// Verify connectivity
	_, err = clientset.Discovery().ServerVersion()
	if err != nil {
		return nil, nil, fmt.Errorf("k8s discovery failed: %w", err)
	}

	slog.Info("k8s cluster available")
	return clientset, restCfg, nil
}

// K8sSandbox is a sandbox backed by a Kubernetes Pod.
type K8sSandbox struct {
	podName    string
	namespace  string
	config     Config
	clientset  *kubernetes.Clientset
	restConfig *rest.Config
	createdAt  time.Time
	lastUsed   time.Time
	mu         sync.Mutex
}

// newK8sSandbox creates a Pod for sandboxed execution and waits for it to be ready.
func newK8sSandbox(ctx context.Context, name string, cfg Config, clientset *kubernetes.Clientset, restConfig *rest.Config, namespace string) (*K8sSandbox, error) {
	containerWorkdir := cfg.ContainerWorkdir()
	runAsNonRoot := true
	readOnlyRoot := cfg.ReadOnlyRoot
	allowPrivEsc := false

	autoMount := false
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				k8sLabelSandbox: "true",
				"goclaw.scope":  string(cfg.Scope),
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                corev1.RestartPolicyNever,
			AutomountServiceAccountToken: &autoMount,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &runAsNonRoot,
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{{
				Name:            k8sContainerName,
				Image:           cfg.Image,
				ImagePullPolicy: imagePullPolicy(cfg.ImagePullPolicy),
				Command:         []string{"sleep", "infinity"},
				WorkingDir:      containerWorkdir,
				SecurityContext: &corev1.SecurityContext{
					RunAsNonRoot:             &runAsNonRoot,
					ReadOnlyRootFilesystem:   &readOnlyRoot,
					AllowPrivilegeEscalation: &allowPrivEsc,
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
					SeccompProfile: &corev1.SeccompProfile{
						Type: corev1.SeccompProfileTypeRuntimeDefault,
					},
				},
				Resources: buildResourceRequirements(cfg),
				Env:       envVarsFromMap(cfg.Env),
				VolumeMounts: []corev1.VolumeMount{
					{Name: "tmp", MountPath: "/tmp"},
					{Name: "var-tmp", MountPath: "/var/tmp"},
					{Name: "workspace", MountPath: containerWorkdir},
				},
			}},
			Volumes: []corev1.Volume{
				{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				{Name: "var-tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				{Name: "workspace", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
		},
	}

	// Optional service account
	if cfg.ServiceAccount != "" {
		pod.Spec.ServiceAccountName = cfg.ServiceAccount
	}

	// Optional node selector
	if len(cfg.NodeSelector) > 0 {
		pod.Spec.NodeSelector = cfg.NodeSelector
	}

	slog.Debug("creating sandbox pod", "name", name, "namespace", namespace, "image", cfg.Image)

	created, err := clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create pod %q: %w", name, err)
	}

	slog.Info("sandbox pod created, waiting for ready", "name", created.Name, "namespace", namespace)

	if err := waitForPodReady(ctx, clientset, namespace, name); err != nil {
		// Cleanup the pod on failure
		_ = clientset.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
		return nil, fmt.Errorf("pod not ready: %w", err)
	}

	slog.Info("sandbox pod ready", "name", name, "namespace", namespace, "image", cfg.Image)

	// Run optional setup command
	now := time.Now()
	sbx := &K8sSandbox{
		podName:    name,
		namespace:  namespace,
		config:     cfg,
		clientset:  clientset,
		restConfig: restConfig,
		createdAt:  now,
		lastUsed:   now,
	}

	if cfg.SetupCommand != "" {
		result, err := sbx.Exec(ctx, []string{"sh", "-lc", cfg.SetupCommand}, "")
		if err != nil {
			slog.Warn("sandbox setup command failed", "pod", name, "error", err)
		} else if result.ExitCode != 0 {
			slog.Warn("sandbox setup command non-zero exit", "pod", name, "exit_code", result.ExitCode, "stderr", result.Stderr)
		} else {
			slog.Info("sandbox setup command completed", "pod", name)
		}
	}

	return sbx, nil
}

// Exec runs a command inside the Pod.
func (s *K8sSandbox) Exec(ctx context.Context, command []string, workDir string) (*ExecResult, error) {
	return s.execInPod(ctx, command, workDir, nil)
}

// ExecWithStdin runs a command inside the Pod with stdin data piped in.
func (s *K8sSandbox) ExecWithStdin(ctx context.Context, command []string, workDir string, stdin []byte) (*ExecResult, error) {
	return s.execInPod(ctx, command, workDir, stdin)
}

// execInPod executes a command in the sandbox pod via SPDY remote execution.
func (s *K8sSandbox) execInPod(ctx context.Context, command []string, workDir string, stdin []byte) (*ExecResult, error) {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()

	timeout := time.Duration(s.config.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Wrap command with cd if workDir is specified
	execCommand := command
	if workDir != "" {
		// sh -c 'cd <workDir> && exec <command>'
		cmdStr := shellJoin(command)
		execCommand = []string{"sh", "-c", fmt.Sprintf("cd %s && exec %s", shellQuote(workDir), cmdStr)}
	}

	req := s.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(s.namespace).
		Name(s.podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: k8sContainerName,
			Command:   execCommand,
			Stdin:     stdin != nil,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(s.restConfig, "POST", req.URL())
	if err != nil {
		return nil, fmt.Errorf("create SPDY executor: %w", err)
	}

	maxOut := s.config.MaxOutputBytes
	if maxOut <= 0 {
		maxOut = 1 << 20 // 1MB default
	}
	stdout := &limitedBuffer{max: maxOut}
	stderr := &limitedBuffer{max: maxOut}

	streamOpts := remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: stderr,
	}
	if stdin != nil {
		streamOpts.Stdin = bytes.NewReader(stdin)
	}

	err = executor.StreamWithContext(execCtx, streamOpts)

	exitCode := 0
	if err != nil {
		// Try to extract exit code from the error
		if codeErr, ok := err.(interface{ ExitStatus() int }); ok {
			exitCode = codeErr.ExitStatus()
		} else if strings.Contains(err.Error(), "command terminated with exit code") {
			// Parse exit code from error message as fallback
			fmt.Sscanf(err.Error(), "command terminated with exit code %d", &exitCode)
		} else {
			return nil, fmt.Errorf("pod exec: %w", err)
		}
	}

	result := &ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}
	if stdout.truncated {
		result.Stdout += "\n...[output truncated]"
	}
	if stderr.truncated {
		result.Stderr += "\n...[output truncated]"
	}
	return result, nil
}

// Destroy deletes the Pod.
func (s *K8sSandbox) Destroy(ctx context.Context) error {
	gracePeriod := int64(0)
	err := s.clientset.CoreV1().Pods(s.namespace).Delete(ctx, s.podName, metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
	})
	if err != nil {
		if errors.IsNotFound(err) {
			slog.Debug("sandbox pod already deleted", "pod", s.podName)
			return nil
		}
		slog.Warn("failed to delete sandbox pod", "pod", s.podName, "error", err)
		return err
	}
	slog.Info("sandbox pod destroyed", "pod", s.podName)
	return nil
}

// ID returns the pod name.
func (s *K8sSandbox) ID() string { return s.podName }

// -------------------------------------------------------------------
// K8sManager
// -------------------------------------------------------------------

// K8sManager manages K8s sandbox Pods.
type K8sManager struct {
	config     Config
	clientset  *kubernetes.Clientset
	restConfig *rest.Config
	namespace  string
	sandboxes  map[string]*K8sSandbox
	mu         sync.RWMutex
	stopCh     chan struct{}
}

// NewK8sManager creates a manager for K8s sandboxes.
// Automatically ensures the namespace exists and starts background pruning.
func NewK8sManager(cfg Config, clientset *kubernetes.Clientset, restConfig *rest.Config) *K8sManager {
	ns := cfg.Namespace
	if ns == "" {
		ns = defaultK8sNS
	}

	m := &K8sManager{
		config:     cfg,
		clientset:  clientset,
		restConfig: restConfig,
		namespace:  ns,
		sandboxes:  make(map[string]*K8sSandbox),
		stopCh:     make(chan struct{}),
	}

	// Best-effort namespace creation at startup
	if err := ensureNamespace(context.Background(), clientset, ns); err != nil {
		slog.Warn("failed to ensure k8s namespace", "namespace", ns, "error", err)
	}

	m.startPruning()
	return m
}

// Get returns an existing sandbox or creates a new one for the given key.
func (m *K8sManager) Get(ctx context.Context, key string, workspace string, cfgOverride *Config) (Sandbox, error) {
	cfg := m.config
	if cfgOverride != nil {
		cfg = *cfgOverride
	}
	if cfg.Mode == ModeOff {
		return nil, ErrSandboxDisabled
	}

	m.mu.RLock()
	if sb, ok := m.sandboxes[key]; ok {
		m.mu.RUnlock()
		return sb, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if sb, ok := m.sandboxes[key]; ok {
		return sb, nil
	}

	name := k8sPodPrefix + sanitizeK8sName(key)
	sb, err := newK8sSandbox(ctx, name, cfg, m.clientset, m.restConfig, m.namespace)
	if err != nil {
		return nil, err
	}

	m.sandboxes[key] = sb
	return sb, nil
}

// Release destroys a sandbox by key.
func (m *K8sManager) Release(ctx context.Context, key string) error {
	m.mu.Lock()
	sb, ok := m.sandboxes[key]
	if ok {
		delete(m.sandboxes, key)
	}
	m.mu.Unlock()

	if ok {
		return sb.Destroy(ctx)
	}
	return nil
}

// ReleaseAll destroys all active sandboxes.
func (m *K8sManager) ReleaseAll(ctx context.Context) error {
	m.mu.Lock()
	sbs := make(map[string]*K8sSandbox, len(m.sandboxes))
	maps.Copy(sbs, m.sandboxes)
	m.sandboxes = make(map[string]*K8sSandbox)
	m.mu.Unlock()

	for key, sb := range sbs {
		if err := sb.Destroy(ctx); err != nil {
			slog.Warn("failed to release k8s sandbox", "key", key, "error", err)
		}
	}
	return nil
}

// Stop signals the pruning goroutine to stop.
func (m *K8sManager) Stop() {
	select {
	case <-m.stopCh:
		// already closed
	default:
		close(m.stopCh)
	}
}

// Stats returns information about active sandboxes.
func (m *K8sManager) Stats() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pods := make(map[string]string, len(m.sandboxes))
	for key, sb := range m.sandboxes {
		pods[key] = sb.podName
	}

	return map[string]any{
		"runtime":   "k8s",
		"mode":      m.config.Mode,
		"image":     m.config.Image,
		"namespace": m.namespace,
		"active":    len(m.sandboxes),
		"pods":      pods,
	}
}

// startPruning launches a background goroutine that periodically prunes idle/old Pods.
func (m *K8sManager) startPruning() {
	interval := time.Duration(m.config.PruneIntervalMin) * time.Minute
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-m.stopCh:
				return
			case <-ticker.C:
				m.Prune(context.Background())
			}
		}
	}()

	slog.Debug("k8s sandbox pruning started", "interval", interval)
}

// Prune removes Pods that are idle too long or exceed max age.
func (m *K8sManager) Prune(ctx context.Context) {
	idleHours := m.config.IdleHours
	if idleHours <= 0 {
		idleHours = 24
	}
	maxAgeDays := m.config.MaxAgeDays
	if maxAgeDays <= 0 {
		maxAgeDays = 7
	}

	now := time.Now()
	idleThreshold := now.Add(-time.Duration(idleHours) * time.Hour)
	ageThreshold := now.Add(-time.Duration(maxAgeDays) * 24 * time.Hour)

	// Collect keys to prune
	m.mu.RLock()
	var toRemove []string
	for key, sb := range m.sandboxes {
		sb.mu.Lock()
		lastUsed := sb.lastUsed
		created := sb.createdAt
		sb.mu.Unlock()

		if lastUsed.Before(idleThreshold) || created.Before(ageThreshold) {
			toRemove = append(toRemove, key)
		}
	}
	m.mu.RUnlock()

	if len(toRemove) == 0 {
		return
	}

	for _, key := range toRemove {
		m.mu.Lock()
		sb, ok := m.sandboxes[key]
		if ok {
			delete(m.sandboxes, key)
		}
		m.mu.Unlock()

		if ok {
			if err := sb.Destroy(ctx); err != nil {
				slog.Warn("prune: failed to destroy k8s sandbox", "key", key, "error", err)
			} else {
				slog.Info("pruned idle sandbox pod", "key", key, "pod", sb.podName)
			}
		}
	}

	slog.Info("k8s sandbox prune completed", "removed", len(toRemove))

	// Also sweep orphaned Pods (e.g. from prior gateway restarts) via K8s API.
	m.pruneOrphanedPods(ctx, ageThreshold)
}

// pruneOrphanedPods deletes sandbox Pods in K8s that are not tracked in-memory.
// This handles Pods left behind after a gateway restart.
func (m *K8sManager) pruneOrphanedPods(ctx context.Context, ageThreshold time.Time) {
	pods, err := m.clientset.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: k8sLabelSandbox + "=true",
	})
	if err != nil {
		slog.Warn("prune: failed to list orphaned pods", "error", err)
		return
	}

	m.mu.RLock()
	tracked := make(map[string]bool, len(m.sandboxes))
	for _, sb := range m.sandboxes {
		tracked[sb.podName] = true
	}
	m.mu.RUnlock()

	var orphansRemoved int
	for _, pod := range pods.Items {
		if tracked[pod.Name] {
			continue
		}
		// Only prune orphans older than age threshold
		if pod.CreationTimestamp.Time.After(ageThreshold) {
			continue
		}
		gracePeriod := int64(0)
		err := m.clientset.CoreV1().Pods(m.namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{
			GracePeriodSeconds: &gracePeriod,
		})
		switch {
		case err == nil:
			orphansRemoved++
			slog.Info("pruned orphaned sandbox pod", "pod", pod.Name, "created", pod.CreationTimestamp.Time)
		case errors.IsNotFound(err):
			// Already gone, skip
		default:
			slog.Warn("prune: failed to delete orphaned pod", "pod", pod.Name, "error", err)
		}
	}
	if orphansRemoved > 0 {
		slog.Info("orphaned pod prune completed", "removed", orphansRemoved)
	}
}

// -------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------

// waitForPodReady watches a Pod until it reaches Running phase with Ready condition.
func waitForPodReady(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) error {
	ctx, cancel := context.WithTimeout(ctx, podReadyTimeout)
	defer cancel()

	// Check if already ready
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get pod: %w", err)
	}
	if isPodReady(pod) {
		return nil
	}

	watcher, err := clientset.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=" + name,
	})
	if err != nil {
		return fmt.Errorf("watch pod: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for pod %q to be ready: %w", name, ctx.Err())
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("watch channel closed for pod %q", name)
			}
			switch event.Type {
			case watch.Modified:
				pod, ok := event.Object.(*corev1.Pod)
				if !ok {
					continue
				}
				if isPodReady(pod) {
					return nil
				}
				if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
					return fmt.Errorf("pod entered terminal phase: %s", pod.Status.Phase)
				}
			case watch.Deleted:
				return fmt.Errorf("pod was deleted while waiting for ready")
			case watch.Error:
				return fmt.Errorf("watch error for pod %q", name)
			}
		}
	}
}

// isPodReady checks if a Pod is in Running phase with Ready condition true.
func isPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// ensureNamespace creates the namespace if it doesn't exist.
func ensureNamespace(ctx context.Context, clientset *kubernetes.Clientset, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	_, err := clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("create namespace %q: %w", namespace, err)
	}
	slog.Info("created k8s namespace", "namespace", namespace)
	return nil
}

// buildResourceRequirements creates K8s resource limits from Config.
// Sets Requests == Limits for Guaranteed QoS class.
func buildResourceRequirements(cfg Config) corev1.ResourceRequirements {
	limits := corev1.ResourceList{}
	if cfg.MemoryMB > 0 {
		limits[corev1.ResourceMemory] = resource.MustParse(fmt.Sprintf("%dMi", cfg.MemoryMB))
	}
	if cfg.CPUs > 0 {
		limits[corev1.ResourceCPU] = resource.MustParse(fmt.Sprintf("%.1f", cfg.CPUs))
	}
	requests := make(corev1.ResourceList, len(limits))
	for k, v := range limits {
		requests[k] = v.DeepCopy()
	}
	return corev1.ResourceRequirements{
		Limits:   limits,
		Requests: requests,
	}
}

// imagePullPolicy converts a string to K8s PullPolicy, defaulting to IfNotPresent.
func imagePullPolicy(policy string) corev1.PullPolicy {
	switch policy {
	case "Always":
		return corev1.PullAlways
	case "Never":
		return corev1.PullNever
	default:
		return corev1.PullIfNotPresent
	}
}

// envVarsFromMap converts a map to K8s EnvVar slice.
func envVarsFromMap(env map[string]string) []corev1.EnvVar {
	if len(env) == 0 {
		return nil
	}
	vars := make([]corev1.EnvVar, 0, len(env))
	for k, v := range env {
		vars = append(vars, corev1.EnvVar{Name: k, Value: v})
	}
	return vars
}

// sanitizeK8sName makes a key safe for K8s resource names.
// K8s names must be lowercase, alphanumeric, or '-', max 63 chars, start/end alphanumeric.
func sanitizeK8sName(key string) string {
	s := strings.ToLower(key)

	// Replace common separators with dashes, strip everything else non-alphanumeric
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		case c == ':' || c == '/' || c == ' ' || c == '.' || c == '_' || c == '-':
			b.WriteByte('-')
		}
		// Skip all other characters (null bytes, unicode, etc.)
	}
	s = b.String()

	// Remove leading/trailing dashes
	s = strings.Trim(s, "-")

	if len(s) > 50 {
		s = s[:50]
	}
	// Remove trailing dash after truncation
	s = strings.TrimRight(s, "-")

	if s == "" {
		s = "default"
	}
	return s
}

// shellQuote wraps a string in single quotes for shell safety.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// shellJoin joins command parts for shell execution.
func shellJoin(parts []string) string {
	quoted := make([]string, len(parts))
	for i, p := range parts {
		if strings.ContainsAny(p, " \t\n\"'\\$`!#&|;(){}[]<>?*~") {
			quoted[i] = shellQuote(p)
		} else {
			quoted[i] = p
		}
	}
	return strings.Join(quoted, " ")
}
