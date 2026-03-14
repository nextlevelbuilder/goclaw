package proxyserver

import (
	"log/slog"
	"net/http"
)

// AuthInjector handles authentication header injection for proxy requests.
type AuthInjector struct {
	logger *slog.Logger
}

// NewAuthInjector creates a new auth injector instance.
func NewAuthInjector(logger *slog.Logger) *AuthInjector {
	return &AuthInjector{
		logger: logger.With("component", "auth-injector"),
	}
}

// InjectAuth adds authentication headers to the request based on auth config.
func (a *AuthInjector) InjectAuth(req *http.Request, authConfig *AuthConfig) {
	if authConfig == nil {
		return
	}

	token := authConfig.GetToken()
	if token == "" && len(authConfig.Headers) == 0 {
		a.logger.Debug("no auth token or headers configured, skipping injection")
		return
	}

	switch authConfig.Type {
	case AuthTypeBearer:
		a.injectBearer(req, token)
	case AuthTypeAPIKey:
		a.injectAPIKey(req, token, authConfig.HeaderName)
	case AuthTypeHeader:
		a.injectCustomHeader(req, token, authConfig.HeaderName)
	case AuthTypeNone:
		// No auth needed
	default:
		if token != "" {
			a.injectBearer(req, token)
		}
	}

	for key, value := range authConfig.Headers {
		req.Header.Set(key, value)
		a.logger.Debug("injected custom header", "header", key)
	}
}

func (a *AuthInjector) injectBearer(req *http.Request, token string) {
	if token == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	a.logger.Debug("injected Bearer auth", "header", "Authorization")
}

func (a *AuthInjector) injectAPIKey(req *http.Request, token, headerName string) {
	if token == "" {
		return
	}
	if headerName == "" {
		headerName = "api-key"
	}
	req.Header.Set(headerName, token)
	a.logger.Debug("injected API key auth", "header", headerName)
}

func (a *AuthInjector) injectCustomHeader(req *http.Request, token, headerName string) {
	if token == "" {
		return
	}
	if headerName == "" {
		headerName = "X-Auth-Token"
	}
	req.Header.Set(headerName, token)
	a.logger.Debug("injected custom auth header", "header", headerName)
}
