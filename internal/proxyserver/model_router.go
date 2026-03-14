package proxyserver

import (
	"errors"
	"log/slog"
	"sync"
)

// ErrUnknownModel is returned when a model is not found in configuration.
var ErrUnknownModel = errors.New("unknown model")

// UnknownModelError contains details about the unknown model.
type UnknownModelError struct {
	Model   string
	Message string
}

func (e *UnknownModelError) Error() string {
	return e.Message
}

// ModelRouter routes requests to correct pods based on model name.
type ModelRouter struct {
	podMap         sync.Map
	providerMap    sync.Map
	authConfigMap  sync.Map
	pathPrefixMap  sync.Map
	queryParamsMap sync.Map
	logger         *slog.Logger
}

// ModelInfo contains full model routing information.
type ModelInfo struct {
	PodURL      string
	Provider    string
	AuthConfig  *AuthConfig
	PathPrefix  string
	QueryParams map[string]string
}

// NewModelRouter creates a new model router.
func NewModelRouter(logger *slog.Logger) *ModelRouter {
	return &ModelRouter{
		logger: logger.With("component", "model-router"),
	}
}

// Load loads model configurations into the router.
func (r *ModelRouter) Load(models []ModelConfig) {
	clearMap := func(m *sync.Map) {
		m.Range(func(key, _ interface{}) bool {
			m.Delete(key)
			return true
		})
	}
	clearMap(&r.podMap)
	clearMap(&r.providerMap)
	clearMap(&r.authConfigMap)
	clearMap(&r.pathPrefixMap)
	clearMap(&r.queryParamsMap)

	for _, model := range models {
		r.podMap.Store(model.ModelName, model.PodURL)
		r.providerMap.Store(model.ModelName, model.Provider)

		if model.AuthConfig != nil {
			r.authConfigMap.Store(model.ModelName, model.AuthConfig)
		}
		if model.PathPrefix != "" {
			r.pathPrefixMap.Store(model.ModelName, model.PathPrefix)
		}
		if len(model.QueryParams) > 0 {
			r.queryParamsMap.Store(model.ModelName, model.QueryParams)
		}

		r.logger.Debug("loaded model route",
			"model", model.ModelName,
			"pod_url", model.PodURL,
			"provider", model.Provider,
			"has_auth", model.AuthConfig != nil,
			"path_prefix", model.PathPrefix)
	}

	r.logger.Info("model routes loaded", "count", len(models))
}

// GetPodURL returns the pod URL for a model (exact match, case-sensitive).
func (r *ModelRouter) GetPodURL(model string) (string, error) {
	if model == "" {
		return "", &UnknownModelError{
			Model:   model,
			Message: "Model name is required in request",
		}
	}

	value, exists := r.podMap.Load(model)
	if !exists {
		r.logger.Warn("unknown model", "model", model)
		return "", &UnknownModelError{
			Model:   model,
			Message: "Unknown model: '" + model + "'. Model must match exactly (case-sensitive).",
		}
	}

	podURL := value.(string)
	r.logger.Debug("routed model to pod", "model", model, "pod_url", podURL)
	return podURL, nil
}

// GetAllPodURLs returns all possible pod URLs for a model (for fallback routing).
func (r *ModelRouter) GetAllPodURLs(model string) ([]string, error) {
	podURL, err := r.GetPodURL(model)
	if err != nil {
		return nil, err
	}
	return []string{podURL}, nil
}

// GetProvider returns the provider for a model.
func (r *ModelRouter) GetProvider(model string) (string, error) {
	value, exists := r.providerMap.Load(model)
	if !exists {
		return "", &UnknownModelError{
			Model:   model,
			Message: "Provider not found in config for model '" + model + "'",
		}
	}
	return value.(string), nil
}

// GetAllPods returns all unique pod URLs.
func (r *ModelRouter) GetAllPods() []string {
	podSet := make(map[string]struct{})
	r.podMap.Range(func(_, value interface{}) bool {
		podSet[value.(string)] = struct{}{}
		return true
	})

	pods := make([]string, 0, len(podSet))
	for pod := range podSet {
		pods = append(pods, pod)
	}
	return pods
}

// GetModelCount returns the number of configured models.
func (r *ModelRouter) GetModelCount() int {
	count := 0
	r.podMap.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// GetAuthConfig returns the auth configuration for a model (may be nil).
func (r *ModelRouter) GetAuthConfig(model string) *AuthConfig {
	value, exists := r.authConfigMap.Load(model)
	if !exists {
		return nil
	}
	return value.(*AuthConfig)
}

// GetPathPrefix returns the path prefix for a model (empty for internal pods).
func (r *ModelRouter) GetPathPrefix(model string) string {
	value, exists := r.pathPrefixMap.Load(model)
	if !exists {
		return ""
	}
	return value.(string)
}

// GetQueryParams returns extra query params for a model (nil for internal pods).
func (r *ModelRouter) GetQueryParams(model string) map[string]string {
	value, exists := r.queryParamsMap.Load(model)
	if !exists {
		return nil
	}
	return value.(map[string]string)
}

// GetModelInfo returns complete model routing information including auth config.
func (r *ModelRouter) GetModelInfo(model string) (*ModelInfo, error) {
	podURL, err := r.GetPodURL(model)
	if err != nil {
		return nil, err
	}

	provider, _ := r.GetProvider(model)
	authConfig := r.GetAuthConfig(model)
	pathPrefix := r.GetPathPrefix(model)
	queryParams := r.GetQueryParams(model)

	return &ModelInfo{
		PodURL:      podURL,
		Provider:    provider,
		AuthConfig:  authConfig,
		PathPrefix:  pathPrefix,
		QueryParams: queryParams,
	}, nil
}
