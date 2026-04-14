package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Vertex AI constants. Kept in the providers package (not store) to avoid an
// import cycle — store is imported by providers, so providers cannot import store.
const (
	// VertexDefaultModel is the default Gemini model id (Vertex requires the "google/" prefix).
	VertexDefaultModel = "google/gemini-2.0-flash-001"

	// VertexDefaultScope is the OAuth2 scope for Vertex AI access.
	VertexDefaultScope = "https://www.googleapis.com/auth/cloud-platform"

	// ProviderTypeVertex mirrors store.ProviderVertex; duplicated here to keep the
	// providers package free of a store import. Kept in sync by convention.
	ProviderTypeVertex = "vertex"
)

// VertexDefaultAPIBase builds the Vertex AI OpenAI-compatible endpoint URL
// from a GCP project ID and region. Returns empty when either is missing.
// Matches: https://{region}-aiplatform.googleapis.com/v1/projects/{project}/locations/{region}/endpoints/openapi
func VertexDefaultAPIBase(projectID, region string) string {
	if projectID == "" || region == "" {
		return ""
	}
	return "https://" + region + "-aiplatform.googleapis.com/v1/projects/" +
		projectID + "/locations/" + region + "/endpoints/openapi"
}

// VertexConfig is the input needed to build a Vertex AI provider instance.
// Credentials precedence: CredentialsJSON > CredentialsFile > ADC (Application Default Credentials).
// When all credential sources are empty, ADC is used — works on GCE/GKE/Cloud Run where
// the metadata server issues tokens automatically, or when GOOGLE_APPLICATION_CREDENTIALS is set.
type VertexConfig struct {
	Name            string // registry name (e.g. "vertex"); defaults to "vertex"
	CredentialsJSON string // inline service account JSON (typically from DB or env)
	CredentialsFile string // path to service account JSON file. OPERATOR-ONLY — never expose via admin UI
	// or DB settings without path allow-list validation: this path is read directly from disk,
	// which would let remote admins exfiltrate arbitrary readable files via crafted settings.
	ProjectID       string // required — GCP project ID (6-30 chars, lowercase letters/digits/hyphens, must start with a letter)
	Region          string // required — GCP region (e.g. "us-central1", "asia-southeast1")
	DefaultModel    string // e.g. "google/gemini-2.0-flash-001"; defaults to VertexDefaultModel
	APIBaseOverride string // optional — explicit base URL; defaults to computed from project+region
}

// GCP region format: lowercase, hyphen-separated alphanum segments. e.g. "us-central1", "asia-southeast1", "global".
var vertexRegionRe = regexp.MustCompile(`^[a-z]+(-[a-z0-9]+)*$`)

// GCP project ID format per https://cloud.google.com/resource-manager/docs/creating-managing-projects:
// 6-30 chars, lowercase letters/digits/hyphens, must start with a letter.
var vertexProjectIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)

// validateVertexProjectID rejects project IDs that don't match GCP's documented shape.
// Defense-in-depth: values come from admin-authenticated input (config, env, or Settings JSONB)
// and are interpolated into the endpoint URL — a malformed value could escape the intended host.
func validateVertexProjectID(id string) error {
	if !vertexProjectIDRe.MatchString(id) {
		return fmt.Errorf("vertex: invalid project_id %q (expected 6-30 lowercase letters/digits/hyphens starting with a letter)", id)
	}
	return nil
}

// validateVertexRegion rejects region strings that don't match GCP's documented shape.
func validateVertexRegion(region string) error {
	if !vertexRegionRe.MatchString(region) {
		return fmt.Errorf("vertex: invalid region %q (expected lowercase hyphen-separated alphanum, e.g. us-central1)", region)
	}
	return nil
}

// validateVertexAPIBaseOverride sanity-checks an explicit API base URL when provided.
// Belt-and-suspenders defense: `validateProviderURL` in internal/http runs at CRUD time,
// but a DB row inserted via migration or direct SQL can bypass that path.
// We require https + a Google-looking Vertex hostname to prevent data exfiltration
// (messages going to an attacker-controlled server while auth goes to Google).
func validateVertexAPIBaseOverride(base string) error {
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("vertex: invalid api_base_override %q: %w", base, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("vertex: api_base_override must use https scheme, got %q", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, "aiplatform.googleapis.com") && !strings.HasSuffix(host, ".googleapis.com") {
		return fmt.Errorf("vertex: api_base_override host %q is not a googleapis.com endpoint", host)
	}
	return nil
}

// NewVertexProvider constructs an OpenAIProvider pre-configured for Google Cloud Vertex AI.
// Uses oauth2.Transport for automatic token refresh (1-hour access tokens) — no manual refresh needed.
// The returned provider speaks OpenAI ChatCompletions format against Vertex's OpenAI-compatible endpoint.
func NewVertexProvider(ctx context.Context, cfg VertexConfig) (*OpenAIProvider, error) {
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("vertex: project_id is required")
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("vertex: region is required")
	}
	if err := validateVertexProjectID(cfg.ProjectID); err != nil {
		return nil, err
	}
	if err := validateVertexRegion(cfg.Region); err != nil {
		return nil, err
	}
	if override := strings.TrimSpace(cfg.APIBaseOverride); override != "" {
		if err := validateVertexAPIBaseOverride(override); err != nil {
			return nil, err
		}
	}

	tokenSource, err := resolveVertexTokenSource(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// ReuseTokenSource caches the current token in-memory until expiry (~1 hour),
	// then transparently fetches a fresh one. No extra work for callers.
	cached := oauth2.ReuseTokenSource(nil, tokenSource)

	client := &http.Client{
		Timeout: DefaultHTTPTimeout,
		Transport: &oauth2.Transport{
			Source: cached,
			Base:   http.DefaultTransport,
		},
	}

	apiBase := strings.TrimSpace(cfg.APIBaseOverride)
	if apiBase == "" {
		apiBase = VertexDefaultAPIBase(cfg.ProjectID, cfg.Region)
	}

	defaultModel := cfg.DefaultModel
	if defaultModel == "" {
		defaultModel = VertexDefaultModel
	}

	name := cfg.Name
	if name == "" {
		name = "vertex"
	}

	// apiKey is intentionally empty — oauth2.Transport injects Authorization from the TokenSource.
	// WithoutAuthHeader ensures doRequest() doesn't overwrite that with a "Bearer " header.
	prov := NewOpenAIProvider(name, "", apiBase, defaultModel).
		WithProviderType(ProviderTypeVertex).
		WithHTTPClient(client).
		WithoutAuthHeader()

	return prov, nil
}

// resolveVertexTokenSource returns a GCP TokenSource using the first available credential source:
// inline JSON → file path → Application Default Credentials.
func resolveVertexTokenSource(ctx context.Context, cfg VertexConfig) (oauth2.TokenSource, error) {
	scope := VertexDefaultScope

	if data := strings.TrimSpace(cfg.CredentialsJSON); data != "" {
		creds, err := google.CredentialsFromJSON(ctx, []byte(data), scope)
		if err != nil {
			return nil, fmt.Errorf("vertex: parse inline credentials: %w", err)
		}
		return creds.TokenSource, nil
	}

	if path := strings.TrimSpace(cfg.CredentialsFile); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("vertex: read credentials file: %w", err)
		}
		creds, err := google.CredentialsFromJSON(ctx, data, scope)
		if err != nil {
			return nil, fmt.Errorf("vertex: parse credentials file %q: %w", path, err)
		}
		return creds.TokenSource, nil
	}

	// ADC: GOOGLE_APPLICATION_CREDENTIALS env, ~/.config/gcloud/..., or GCE metadata server.
	creds, err := google.FindDefaultCredentials(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("vertex: application default credentials not found (set GOOGLE_APPLICATION_CREDENTIALS, provide credentials_file, or run on GCP): %w", err)
	}
	return creds.TokenSource, nil
}

// vertexInitTimeout caps credential discovery time so ADC on non-GCP machines
// doesn't stall gateway startup waiting for the metadata server.
const vertexInitTimeout = 10 * time.Second

// NewVertexProviderWithTimeout wraps NewVertexProvider with a bounded context.
// Recommended for startup-time registration where slow metadata lookups must not block boot.
func NewVertexProviderWithTimeout(cfg VertexConfig) (*OpenAIProvider, error) {
	ctx, cancel := context.WithTimeout(context.Background(), vertexInitTimeout)
	defer cancel()
	return NewVertexProvider(ctx, cfg)
}
