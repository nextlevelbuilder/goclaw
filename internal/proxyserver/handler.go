package proxyserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/proxyserver/validation"
)

// Context keys for passing data between request parsing and response modification.
type contextKey string

const ctxKeyAudioDuration contextKey = "audio_input_duration"
const ctxKeyInputText contextKey = "input_text"

// FlexibleBool allows unmarshaling from both boolean and string ("true"/"false").
type FlexibleBool bool

func (fb *FlexibleBool) UnmarshalJSON(data []byte) error {
	str := strings.Trim(string(data), `"`)
	switch strings.ToLower(str) {
	case "true", "1":
		*fb = true
	case "false", "0", "":
		*fb = false
	default:
		var b bool
		if err := json.Unmarshal(data, &b); err != nil {
			return fmt.Errorf("cannot parse %q as boolean", string(data))
		}
		*fb = FlexibleBool(b)
	}
	return nil
}

// Handler handles proxy requests using httputil.ReverseProxy.
type Handler struct {
	router            *ModelRouter
	circuitBreaker    *CircuitBreaker
	retryer           *Retryer
	transport         *http.Transport
	responseModifier  *ResponseModifier
	authInjector      *AuthInjector
	securityValidator *validation.SecurityValidator
	audioExtractor    *validation.AudioExtractor
	imageValidator    *validation.ImageValidator
	config            *ProxyConfig
	logger            *slog.Logger
}

// NewHandler creates a new proxy handler.
func NewHandler(
	router *ModelRouter,
	circuitBreaker *CircuitBreaker,
	retryer *Retryer,
	cfg *ProxyConfig,
	logger *slog.Logger,
) *Handler {
	securityCfg := &validation.SecurityConfig{
		MaxFileSize:       cfg.MaxRequestSize,
		MaxAudioSize:      50 * 1024 * 1024,
		MaxImageSize:      20 * 1024 * 1024,
		BlockExecutables:  true,
		ValidateExtension: true,
	}

	return &Handler{
		router:            router,
		circuitBreaker:    circuitBreaker,
		retryer:           retryer,
		transport:         NewPooledTransport(cfg),
		responseModifier:  NewResponseModifier(logger, validation.NewAudioExtractor(logger)),
		authInjector:      NewAuthInjector(logger),
		securityValidator: validation.NewSecurityValidator(securityCfg, logger),
		audioExtractor:    validation.NewAudioExtractor(logger),
		imageValidator:    validation.NewImageValidator(4096, 4096, logger),
		config:            cfg,
		logger:            logger.With("component", "proxy-handler"),
	}
}

// ServeHTTP handles incoming proxy requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()
	path := r.URL.Path
	method := r.Method

	isAudioTranscription := IsAudioTranscriptionPath(path)
	isTTS := IsTextToSpeechPath(path)
	isAudioAPI := isAudioTranscription || isTTS
	isVideoGeneration := IsVideoGenerationPath(path)

	model, reqBody, err := h.extractModel(r)
	if err != nil {
		h.logger.Error("failed to extract model from request",
			"path", path,
			"error", err)
		h.sendError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	ctx = r.Context()

	if model == "" {
		if isVideoGeneration && method != http.MethodPost {
			model = r.Header.Get("X-Model-Name")
		}
		if model == "" {
			h.sendError(w, http.StatusBadRequest, "invalid_request", "Model name is required in request body or X-Model-Name header")
			return
		}
	}

	h.logger.Info("processing request",
		"method", method,
		"path", path,
		"model", model)

	podURLs, err := h.router.GetAllPodURLs(model)
	if err != nil {
		if ume, ok := err.(*UnknownModelError); ok {
			h.sendError(w, http.StatusNotFound, "model_not_found", ume.Message)
			return
		}
		h.logger.Error("routing error", "model", model, "error", err)
		h.sendError(w, http.StatusInternalServerError, "routing_error",
			"Unable to route request. Please try again later.")
		return
	}

	healthyPods := h.filterHealthyPods(podURLs)
	bypassCooldown := len(healthyPods) == 0
	if bypassCooldown {
		h.logger.Warn("all pods in cooldown, bypassing cooldown check", "model", model)
		healthyPods = podURLs
	}

	var lastErr error
	var lastStatusCode int

	for attempt := 0; attempt < h.retryer.GetMaxAttempts(); attempt++ {
		for _, podURL := range healthyPods {
			if !bypassCooldown && h.circuitBreaker.IsPodInCooldown(podURL) {
				h.logger.Debug("pod in cooldown, skipping", "pod", podURL)
				continue
			}

			target, err := url.Parse(podURL)
			if err != nil {
				h.logger.Error("invalid pod URL", "pod", podURL, "error", err)
				continue
			}

			proxy := h.createReverseProxy(target, model, isAudioAPI, isAudioTranscription, isTTS, isVideoGeneration)

			proxyReq, err := h.createProxyRequest(ctx, r, target, reqBody, model)
			if err != nil {
				h.logger.Error("failed to create proxy request", "error", err)
				lastErr = err
				continue
			}

			recorder := &responseRecorder{
				ResponseWriter: w,
				statusCode:     200,
				written:        false,
			}

			proxy.ServeHTTP(recorder, proxyReq)

			if recorder.statusCode >= 200 && recorder.statusCode < 300 {
				h.circuitBreaker.RecordSuccess(podURL)
				h.logger.Info("request successful",
					"pod", podURL,
					"status", recorder.statusCode,
					"duration", time.Since(startTime))
				return
			}

			lastStatusCode = recorder.statusCode
			lastErr = fmt.Errorf("upstream returned %d", recorder.statusCode)

			if IsRetryableStatusCode(recorder.statusCode) {
				h.logger.Warn("pod returned retryable status",
					"pod", podURL,
					"status", recorder.statusCode,
					"attempt", attempt+1)

				if !recorder.written {
					continue
				}
			}

			h.circuitBreaker.RecordFailure(podURL, recorder.statusCode, lastErr.Error())

			if recorder.written {
				return
			}
		}

		if attempt < h.retryer.GetMaxAttempts()-1 {
			if err := h.retryer.WaitForRetry(ctx, attempt); err != nil {
				h.logger.Debug("retry wait cancelled", "error", err)
				break
			}
		}
	}

	h.logger.Error("request failed after all retries",
		"model", model,
		"status", lastStatusCode,
		"error", lastErr)

	if lastStatusCode == 0 {
		lastStatusCode = http.StatusBadGateway
	}
	h.sendError(w, lastStatusCode, "request_failed",
		"Service temporarily unavailable. Please try again later.")
}

func (h *Handler) extractModel(r *http.Request) (string, []byte, error) {
	if r.Body == nil || r.ContentLength == 0 || r.Method == http.MethodGet || r.Method == http.MethodDelete {
		return "", nil, nil
	}

	contentType := r.Header.Get("Content-Type")

	if strings.Contains(contentType, "multipart/form-data") {
		return h.extractModelFromMultipart(r)
	}

	if strings.Contains(contentType, "application/json") {
		return h.extractModelFromJSON(r)
	}

	return "", nil, nil
}

func (h *Handler) extractModelFromJSON(r *http.Request) (string, []byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read request body: %w", err)
	}
	r.Body.Close()

	if h.config.MaxRequestSize > 0 && int64(len(body)) > h.config.MaxRequestSize {
		return "", nil, fmt.Errorf("request body too large")
	}

	if len(body) == 0 {
		return "", body, nil
	}

	var payload struct {
		Model         string                 `json:"model"`
		Stream        FlexibleBool           `json:"stream"`
		StreamOptions map[string]interface{} `json:"stream_options,omitempty"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, fmt.Errorf("invalid JSON: %w", err)
	}

	if bool(payload.Stream) {
		modified := false
		if payload.StreamOptions == nil {
			payload.StreamOptions = map[string]interface{}{"include_usage": true}
			modified = true
		} else if _, ok := payload.StreamOptions["include_usage"]; !ok {
			payload.StreamOptions["include_usage"] = true
			modified = true
		}

		if modified {
			var fullPayload map[string]interface{}
			json.Unmarshal(body, &fullPayload)
			fullPayload["stream_options"] = payload.StreamOptions
			body, _ = json.Marshal(fullPayload)
		}
	}

	if IsTextToSpeechPath(r.URL.Path) {
		var fullPayload map[string]interface{}
		if json.Unmarshal(body, &fullPayload) == nil {
			var inputText string
			if v, ok := fullPayload["input"].(string); ok {
				inputText = v
			} else if v, ok := fullPayload["text"].(string); ok {
				inputText = v
			}
			if inputText != "" {
				ctx := context.WithValue(r.Context(), ctxKeyInputText, inputText)
				*r = *r.WithContext(ctx)
			}
		}
	}

	return payload.Model, body, nil
}

func (h *Handler) extractModelFromMultipart(r *http.Request) (string, []byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read request body: %w", err)
	}
	r.Body.Close()

	reader := multipart.NewReader(bytes.NewReader(body), getBoundary(r.Header.Get("Content-Type")))

	var model string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", body, nil
		}

		formName := part.FormName()

		if formName == "model" {
			data, _ := io.ReadAll(part)
			model = strings.TrimSpace(string(data))
			continue
		}

		if formName == "file" || formName == "audio" || formName == "image" {
			fileData, err := io.ReadAll(part)
			if err != nil {
				h.logger.Warn("failed to read file part", "error", err)
				continue
			}

			filename := part.FileName()
			if filename == "" {
				filename = formName
			}

			fileType, err := h.securityValidator.ValidateFile(filename, fileData)
			if err != nil {
				h.logger.Warn("file validation failed",
					"filename", filename,
					"detected_type", fileType.String(),
					"error", err)
				return "", nil, fmt.Errorf("file validation failed: %w", err)
			}

			h.logger.Debug("file validated successfully",
				"filename", filename,
				"type", fileType.String(),
				"size", len(fileData))

			if fileType.IsAudio() {
				duration := h.audioExtractor.GetDuration(fileData, filename)
				h.logger.Debug("extracted audio duration",
					"filename", filename,
					"duration_seconds", duration)
				ctx := context.WithValue(r.Context(), ctxKeyAudioDuration, duration)
				*r = *r.WithContext(ctx)
			}

			if fileType.IsImage() {
				info, err := h.imageValidator.ValidateAndExtract(fileData)
				if err != nil {
					h.logger.Warn("image validation failed",
						"filename", filename,
						"error", err)
					return "", nil, fmt.Errorf("image validation failed: %w", err)
				}
				h.logger.Debug("extracted image info",
					"filename", filename,
					"width", info.Width,
					"height", info.Height)
			}
		}
	}

	return model, body, nil
}

func getBoundary(contentType string) string {
	parts := strings.Split(contentType, "boundary=")
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func (h *Handler) createReverseProxy(target *url.URL, model string, isAudioAPI, isTranscription, isTTS, isVideoGeneration bool) *httputil.ReverseProxy {
	proxy := &httputil.ReverseProxy{
		Director:       CreateSimpleDirector(target),
		Transport:      h.transport,
		ModifyResponse: h.responseModifier.ModifyResponseFunc(model, isAudioAPI, isTranscription, isTTS, isVideoGeneration),
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			h.logger.Error("proxy error",
				"target", target.String(),
				"error", err)
			h.sendError(w, http.StatusBadGateway, "proxy_error",
				"Unable to connect to upstream service. Please try again later.")
		},
		FlushInterval: -1,
	}
	return proxy
}

func (h *Handler) createProxyRequest(ctx context.Context, original *http.Request, target *url.URL, body []byte, model string) (*http.Request, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	targetPath := original.URL.Path
	pathPrefix := h.router.GetPathPrefix(model)
	if pathPrefix != "" {
		endpoint := extractEndpoint(original.URL.Path)
		targetPath = strings.TrimRight(pathPrefix, "/") + endpoint
	}

	req, err := http.NewRequestWithContext(ctx, original.Method, target.String()+targetPath, bodyReader)
	if err != nil {
		return nil, err
	}

	req.URL.RawQuery = original.URL.RawQuery

	queryParams := h.router.GetQueryParams(model)
	if len(queryParams) > 0 {
		q := req.URL.Query()
		for k, v := range queryParams {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	req.Header = FilterRequestHeaders(original.Header)
	authConfig := h.router.GetAuthConfig(model)
	h.authInjector.InjectAuth(req, authConfig)

	if body != nil {
		req.ContentLength = int64(len(body))
	}

	return req, nil
}

func extractEndpoint(path string) string {
	if strings.HasPrefix(path, "/v1/") {
		return path[3:]
	}
	return path
}

func (h *Handler) filterHealthyPods(pods []string) []string {
	if len(pods) == 0 {
		return pods
	}

	cooldownStatus := h.circuitBreaker.ArePodsinCooldownBatch(pods)

	healthy := make([]string, 0, len(pods))
	for _, pod := range pods {
		if !cooldownStatus[pod] {
			healthy = append(healthy, pod)
		}
	}
	return healthy
}

func (h *Handler) sendError(w http.ResponseWriter, statusCode int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    errType,
		},
	}
	json.NewEncoder(w).Encode(response)
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.written = true
	return r.ResponseWriter.Write(b)
}

func (r *responseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
