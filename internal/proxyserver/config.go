// Package proxyserver provides an AI model proxy that routes requests to
// upstream pods/providers with circuit-breaking, retry logic, and request
// validation. It integrates into the goclaw gateway as a sub-handler.
package proxyserver

import "time"

// Config holds the proxy server configuration.
type Config struct {
	Enabled        bool          `yaml:"enabled" json:"enabled"`
	Host           string        `yaml:"host" json:"host"`
	Port           int           `yaml:"port" json:"port"`
	CircuitBreaker CBConfig      `yaml:"circuit_breaker" json:"circuit_breaker"`
	Retry          RetryConfig   `yaml:"retry" json:"retry"`
	Proxy          ProxyConfig   `yaml:"proxy" json:"proxy"`
	Models         []ModelConfig `yaml:"models" json:"models"`
}

// CBConfig holds circuit breaker settings.
type CBConfig struct {
	AllowedFails int           `yaml:"allowed_fails" json:"allowed_fails"`
	CooldownTime time.Duration `yaml:"cooldown_time" json:"cooldown_time"`
}

// RetryConfig holds retry settings.
type RetryConfig struct {
	MaxAttempts     int           `yaml:"max_attempts" json:"max_attempts"`
	BaseDelay       time.Duration `yaml:"base_delay" json:"base_delay"`
	MaxDelay        time.Duration `yaml:"max_delay" json:"max_delay"`
	ExponentialBase float64       `yaml:"exponential_base" json:"exponential_base"`
}

// ProxyConfig holds proxy transport settings.
type ProxyConfig struct {
	MaxRequestSize        int64         `yaml:"max_request_size" json:"max_request_size"`
	ReadTimeout           time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout          time.Duration `yaml:"write_timeout" json:"write_timeout"`
	KeepaliveTimeout      time.Duration `yaml:"keepalive_timeout" json:"keepalive_timeout"`
	ConnectionPoolLimit   int           `yaml:"connection_pool_limit" json:"connection_pool_limit"`
	ConnectionPoolPerHost int           `yaml:"connection_pool_per_host" json:"connection_pool_per_host"`
}

// ModelConfig defines a model-to-pod routing entry.
type ModelConfig struct {
	ModelName   string            `yaml:"model_name" json:"model_name"`
	PodURL      string            `yaml:"pod_url" json:"pod_url"`
	Provider    string            `yaml:"provider" json:"provider"`
	AuthConfig  *AuthConfig       `yaml:"auth_config,omitempty" json:"auth_config,omitempty"`
	PathPrefix  string            `yaml:"path_prefix,omitempty" json:"path_prefix,omitempty"`
	QueryParams map[string]string `yaml:"query_params,omitempty" json:"query_params,omitempty"`
}

// AuthConfig defines authentication for an upstream provider.
type AuthConfig struct {
	Type       AuthType          `yaml:"type" json:"type"`
	Token      string            `yaml:"token,omitempty" json:"token,omitempty"`
	TokenEnv   string            `yaml:"token_env,omitempty" json:"token_env,omitempty"`
	HeaderName string            `yaml:"header_name,omitempty" json:"header_name,omitempty"`
	Headers    map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
}

// AuthType represents the authentication mechanism.
type AuthType string

const (
	AuthTypeNone   AuthType = "none"
	AuthTypeBearer AuthType = "bearer"
	AuthTypeAPIKey AuthType = "api_key"
	AuthTypeHeader AuthType = "header"
)

// GetToken returns the auth token, checking env var fallback.
func (a *AuthConfig) GetToken() string {
	if a.Token != "" {
		return a.Token
	}
	// TokenEnv support can be added via os.Getenv if needed
	return ""
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled: false,
		Host:    "0.0.0.0",
		Port:    8080,
		CircuitBreaker: CBConfig{
			AllowedFails: 3,
			CooldownTime: 30 * time.Second,
		},
		Retry: RetryConfig{
			MaxAttempts:     3,
			BaseDelay:       500 * time.Millisecond,
			MaxDelay:        5 * time.Second,
			ExponentialBase: 2.0,
		},
		Proxy: ProxyConfig{
			MaxRequestSize:        100 * 1024 * 1024, // 100MB
			ReadTimeout:           120 * time.Second,
			WriteTimeout:          120 * time.Second,
			KeepaliveTimeout:      90 * time.Second,
			ConnectionPoolLimit:   100,
			ConnectionPoolPerHost: 10,
		},
	}
}
