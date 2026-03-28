package providers

// CursorAuthStatus holds auth state for the Cursor CLI provider.
type CursorAuthStatus struct {
	Authenticated bool
}

// CheckCursorAuthStatus verifies an API key is configured.
// No subprocess needed — API key presence is sufficient for headless operation.
func CheckCursorAuthStatus(apiKey string) *CursorAuthStatus {
	return &CursorAuthStatus{Authenticated: apiKey != ""}
}
