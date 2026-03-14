package proxyserver

import (
	"log/slog"
	"sync"
	"time"
)

// CircuitBreaker manages pod health with cooldown logic (in-memory only).
type CircuitBreaker struct {
	allowedFails  int
	cooldownTime  time.Duration
	inMemory      map[string]*cooldownEntry
	inMemoryMu    sync.RWMutex
	failureCounts map[string]int
	failuresMu    sync.RWMutex
	logger        *slog.Logger
}

type cooldownEntry struct {
	until      time.Time
	statusCode int
	exception  string
}

// NewCircuitBreaker creates a new in-memory circuit breaker.
func NewCircuitBreaker(cfg *CBConfig, logger *slog.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		allowedFails:  cfg.AllowedFails,
		cooldownTime:  cfg.CooldownTime,
		inMemory:      make(map[string]*cooldownEntry),
		failureCounts: make(map[string]int),
		logger:        logger.With("component", "circuit-breaker"),
	}
}

// IsPodInCooldown checks if a pod is currently in cooldown.
func (cb *CircuitBreaker) IsPodInCooldown(podURL string) bool {
	cb.inMemoryMu.RLock()
	defer cb.inMemoryMu.RUnlock()

	entry, exists := cb.inMemory[podURL]
	if !exists {
		return false
	}
	if time.Now().After(entry.until) {
		go cb.cleanupExpiredCooldown(podURL)
		return false
	}
	return true
}

// ArePodsinCooldownBatch checks multiple pods at once.
func (cb *CircuitBreaker) ArePodsinCooldownBatch(pods []string) map[string]bool {
	results := make(map[string]bool, len(pods))
	for _, pod := range pods {
		results[pod] = cb.IsPodInCooldown(pod)
	}
	return results
}

func (cb *CircuitBreaker) cleanupExpiredCooldown(podURL string) {
	cb.inMemoryMu.Lock()
	defer cb.inMemoryMu.Unlock()
	delete(cb.inMemory, podURL)
}

// RecordSuccess records a successful request and clears failure count.
func (cb *CircuitBreaker) RecordSuccess(podURL string) {
	cb.failuresMu.Lock()
	defer cb.failuresMu.Unlock()
	delete(cb.failureCounts, podURL)
	cb.logger.Debug("recorded success for pod", "pod", podURL)
}

// RecordFailure records a failed request and may put pod in cooldown.
// Returns true if pod was put in cooldown.
func (cb *CircuitBreaker) RecordFailure(podURL string, statusCode int, errMsg string) bool {
	cb.failuresMu.Lock()
	cb.failureCounts[podURL]++
	count := cb.failureCounts[podURL]
	cb.failuresMu.Unlock()

	if count >= cb.allowedFails {
		cb.inMemoryMu.Lock()
		cb.inMemory[podURL] = &cooldownEntry{
			until:      time.Now().Add(cb.cooldownTime),
			statusCode: statusCode,
			exception:  errMsg,
		}
		cb.inMemoryMu.Unlock()

		cb.failuresMu.Lock()
		delete(cb.failureCounts, podURL)
		cb.failuresMu.Unlock()

		cb.logger.Warn("pod put in cooldown",
			"pod", podURL,
			"status_code", statusCode,
			"cooldown_time", cb.cooldownTime)
		return true
	}

	cb.logger.Debug("pod failure recorded (not cooldown yet)",
		"pod", podURL,
		"failures", count,
		"allowed", cb.allowedFails)
	return false
}

// GetCooldownPods returns list of pods currently in cooldown.
func (cb *CircuitBreaker) GetCooldownPods(allPods []string) []string {
	var cooldownPods []string
	for _, pod := range allPods {
		if cb.IsPodInCooldown(pod) {
			cooldownPods = append(cooldownPods, pod)
		}
	}
	return cooldownPods
}

// GetPodHealth returns health status of a specific pod.
func (cb *CircuitBreaker) GetPodHealth(podURL string) map[string]any {
	inCooldown := cb.IsPodInCooldown(podURL)
	status := "healthy"
	if inCooldown {
		status = "cooldown"
	}

	health := map[string]any{
		"pod_url":     podURL,
		"status":      status,
		"in_cooldown": inCooldown,
	}

	if inCooldown {
		cb.inMemoryMu.RLock()
		if entry, exists := cb.inMemory[podURL]; exists {
			health["cooldown_until"] = entry.until
			health["status_code"] = entry.statusCode
			health["exception"] = entry.exception
		}
		cb.inMemoryMu.RUnlock()
	}

	return health
}
