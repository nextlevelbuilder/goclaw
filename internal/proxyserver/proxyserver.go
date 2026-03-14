package proxyserver

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// Server is the proxy server that can be embedded into the gateway or run standalone.
type Server struct {
	handler        *Handler
	router         *ModelRouter
	circuitBreaker *CircuitBreaker
	retryer        *Retryer
	config         Config
	logger         *slog.Logger
}

// NewServer creates a new proxy server from configuration.
func NewServer(cfg Config, logger *slog.Logger) *Server {
	lg := logger.With("service", "proxy-server")

	circuitBreaker := NewCircuitBreaker(&cfg.CircuitBreaker, lg)
	modelRouter := NewModelRouter(lg)
	retryer := NewRetryer(&cfg.Retry, lg)

	// Load models into router
	modelRouter.Load(cfg.Models)

	handler := NewHandler(modelRouter, circuitBreaker, retryer, &cfg.Proxy, lg)

	return &Server{
		handler:        handler,
		router:         modelRouter,
		circuitBreaker: circuitBreaker,
		retryer:        retryer,
		config:         cfg,
		logger:         lg,
	}
}

// ServeHTTP implements http.Handler — proxies all requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// RegisterRoutes registers proxy routes on the given mux under the specified prefix.
// Example: prefix = "/proxy" will register:
//   - GET  /proxy/health
//   - POST /proxy/admin/reload
//   - *    /proxy/  (catch-all proxy)
func (s *Server) RegisterRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc("GET "+prefix+"/health", s.handleHealth)
	mux.HandleFunc("POST "+prefix+"/admin/reload", s.handleReload)
	// Catch-all: proxy all other requests under this prefix
	mux.Handle(prefix+"/", http.StripPrefix(prefix, s))
}

// handleHealth returns health status of the proxy and all pods.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	allPods := s.router.GetAllPods()
	cooldownPods := s.circuitBreaker.GetCooldownPods(allPods)

	podHealth := make(map[string]any)
	for _, pod := range allPods {
		podHealth[pod] = s.circuitBreaker.GetPodHealth(pod)
	}

	response := map[string]any{
		"status": "healthy",
		"circuit_breaker": map[string]any{
			"cooldown_pods": len(cooldownPods),
			"healthy_pods":  len(allPods) - len(cooldownPods),
		},
		"configured_models": s.router.GetModelCount(),
		"pod_health":        podHealth,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleReload hot-reloads the model configuration.
func (s *Server) handleReload(w http.ResponseWriter, _ *http.Request) {
	s.logger.Info("reloading proxy configuration...")

	// Re-load models from current config
	s.router.Load(s.config.Models)

	modelCount := s.router.GetModelCount()
	s.logger.Info("proxy configuration reloaded", "models", modelCount)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "success",
		"message": fmt.Sprintf("Configuration reloaded (%d models)", modelCount),
		"models":  modelCount,
	})
}

// ReloadModels allows external callers to update the model list.
func (s *Server) ReloadModels(models []ModelConfig) {
	s.config.Models = models
	s.router.Load(models)
	s.logger.Info("models reloaded", "count", len(models))
}
