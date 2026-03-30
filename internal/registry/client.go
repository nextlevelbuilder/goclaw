package registry

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Client provides access to the GoClaw Hub marketplace API.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewClient creates a new registry client with the given base URL and API key.
// Forces HTTP/1.1 to avoid HTTP/2 stream errors with reverse proxies.
func NewClient(baseURL, apiKey string) *Client {
	transport := &http.Transport{
		TLSNextProto:    make(map[string]func(string, *tls.Conn) http.RoundTripper), // disable HTTP/2
		DialContext:     (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 60 * time.Second, Transport: transport},
	}
}

// Listing represents a marketplace listing from the Hub API.
type Listing struct {
	Slug             string   `json:"slug"`
	Title            string   `json:"title"`
	Tagline          string   `json:"tagline"`
	ListingType      string   `json:"listing_type"`
	PriceCents       int      `json:"price_cents"`
	PricingModel     string   `json:"pricing_model"`
	DownloadCount    int      `json:"download_count"`
	AvgRating        float64  `json:"avg_rating"`
	ReviewCount      int      `json:"review_count"`
	CreatorSlug      string   `json:"creator_slug"`
	CreatorName      string   `json:"creator_display_name"`
	Agents           []Agent  `json:"agents"`
}

// Agent represents an individual agent within a team listing.
type Agent struct {
	AgentKey    string   `json:"agent_key"`
	DisplayName string   `json:"display_name"`
	Emoji       string   `json:"emoji"`
	Role        string   `json:"role"`
	Skills      []string `json:"skills"`
	Model       string   `json:"model_default"`
}

// Category represents a marketplace category.
type Category struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// UpdateInfo contains information about available updates.
type UpdateInfo struct {
	HasUpdate     bool `json:"has_update"`
	LatestVersion int  `json:"latest_version"`
}

// PaginatedResponse wraps paginated API responses.
type PaginatedResponse struct {
	Data json.RawMessage `json:"data"`
	Meta struct {
		Total int `json:"total"`
	} `json:"meta"`
}

// AgentCount returns the number of agents in this listing.
func (l *Listing) AgentCount() int {
	return len(l.Agents)
}

// Search searches marketplace listings with optional filters.
func (c *Client) Search(ctx context.Context, query, category, listingType string) ([]Listing, error) {
	params := url.Values{}
	if query != "" {
		params.Set("q", query)
	}
	if category != "" {
		params.Set("category", category)
	}
	if listingType != "" {
		params.Set("type", listingType)
	}

	reqURL := c.BaseURL + "/registry/search?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Registry-Key", c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, truncateBody(string(body), 200))
	}

	var response PaginatedResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	var listings []Listing
	if err := json.Unmarshal(response.Data, &listings); err != nil {
		return nil, fmt.Errorf("parse listings: %w", err)
	}

	return listings, nil
}

// GetListing retrieves detailed information about a specific listing.
func (c *Client) GetListing(ctx context.Context, slug string) (*Listing, error) {
	reqURL := c.BaseURL + "/registry/listings/" + url.PathEscape(slug)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Registry-Key", c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, truncateBody(string(body), 200))
	}

	var listing Listing
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, fmt.Errorf("parse listing: %w", err)
	}

	return &listing, nil
}

// Download downloads a team bundle from the marketplace.
// Returns an io.ReadCloser for the tar.gz stream that must be closed by the caller.
func (c *Client) Download(ctx context.Context, slug string, goclawVersion string) (io.ReadCloser, error) {
	params := url.Values{}
	if goclawVersion != "" {
		params.Set("goclaw_version", goclawVersion)
	}

	reqURL := c.BaseURL + "/registry/download/" + url.PathEscape(slug)
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	// Use background context for downloads — the agent's request context
	// may be cancelled before the download completes, causing "unexpected EOF".
	dlCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	_ = cancel // caller closes the body, which cancels implicitly

	req, err := http.NewRequestWithContext(dlCtx, "GET", reqURL, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("X-Registry-Key", c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, truncateBody(string(body), 200))
	}

	return resp.Body, nil
}

// CheckUpdate checks if there's a newer version available for a listing.
func (c *Client) CheckUpdate(ctx context.Context, slug string, currentVersion int) (*UpdateInfo, error) {
	params := url.Values{}
	params.Set("slug", slug)
	params.Set("current_version", fmt.Sprintf("%d", currentVersion))

	reqURL := c.BaseURL + "/registry/check-update?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Registry-Key", c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, truncateBody(string(body), 200))
	}

	var updateInfo UpdateInfo
	if err := json.Unmarshal(body, &updateInfo); err != nil {
		return nil, fmt.Errorf("parse update info: %w", err)
	}

	return &updateInfo, nil
}

// Categories retrieves all available marketplace categories.
func (c *Client) Categories(ctx context.Context) ([]Category, error) {
	reqURL := c.BaseURL + "/registry/categories"
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Registry-Key", c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, truncateBody(string(body), 200))
	}

	var categories []Category
	if err := json.Unmarshal(body, &categories); err != nil {
		return nil, fmt.Errorf("parse categories: %w", err)
	}

	return categories, nil
}

func truncateBody(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}