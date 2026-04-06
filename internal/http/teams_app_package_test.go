package http

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/channels/teams/appmanifest"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MockChannelInstanceStore implements store.ChannelInstanceStore for testing
type MockChannelInstanceStore struct {
	instances map[uuid.UUID]*store.ChannelInstanceData
}

func (m *MockChannelInstanceStore) Get(ctx context.Context, id uuid.UUID) (*store.ChannelInstanceData, error) {
	if inst, ok := m.instances[id]; ok {
		return inst, nil
	}
	return nil, fmt.Errorf("instance not found")
}

func (m *MockChannelInstanceStore) GetByName(ctx context.Context, name string) (*store.ChannelInstanceData, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockChannelInstanceStore) Create(ctx context.Context, inst *store.ChannelInstanceData) error {
	return nil
}

func (m *MockChannelInstanceStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	return nil
}

func (m *MockChannelInstanceStore) Delete(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (m *MockChannelInstanceStore) ListEnabled(ctx context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}

func (m *MockChannelInstanceStore) ListAll(ctx context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}

func (m *MockChannelInstanceStore) ListAllInstances(ctx context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}

func (m *MockChannelInstanceStore) ListAllEnabled(ctx context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}

func (m *MockChannelInstanceStore) ListPaged(ctx context.Context, opts store.ChannelInstanceListOpts) ([]store.ChannelInstanceData, error) {
	return nil, nil
}

func (m *MockChannelInstanceStore) CountInstances(ctx context.Context, opts store.ChannelInstanceListOpts) (int, error) {
	return 0, nil
}

func TestTeamsAppPackageHandler_MissingName(t *testing.T) {
	handler := NewTeamsAppPackageHandler(nil)
	req := httptest.NewRequest("GET", "/v1/teams/app-package?bot_id=test-bot", nil)
	req = req.WithContext(store.WithLocale(context.Background(), "en"))

	w := httptest.NewRecorder()
	handler.handleGenerate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestTeamsAppPackageHandler_MissingBotIDNoInstance(t *testing.T) {
	handler := NewTeamsAppPackageHandler(nil)
	req := httptest.NewRequest("GET", "/v1/teams/app-package?name=TestBot", nil)
	req = req.WithContext(store.WithLocale(context.Background(), "en"))

	w := httptest.NewRecorder()
	handler.handleGenerate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestTeamsAppPackageHandler_BotIDFromQuery(t *testing.T) {
	handler := NewTeamsAppPackageHandler(nil)
	req := httptest.NewRequest(
		"GET",
		"/v1/teams/app-package?name=TestBot&bot_id=550e8400-e29b-41d4-a716-446655440000",
		nil,
	)
	req = req.WithContext(store.WithLocale(context.Background(), "en"))

	w := httptest.NewRecorder()
	handler.handleGenerate(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	// Verify ZIP response
	verifyZIPResponse(t, w)
}

func TestTeamsAppPackageHandler_BotIDFromInstance(t *testing.T) {
	instID := uuid.New()
	creds := map[string]string{"bot_id": "550e8400-e29b-41d4-a716-446655440000"}
	credBytes, _ := json.Marshal(creds)
	mockStore := &MockChannelInstanceStore{
		instances: map[uuid.UUID]*store.ChannelInstanceData{
			instID: {
				BaseModel:   store.BaseModel{ID: instID},
				ChannelType: "teams",
				Credentials: credBytes,
			},
		},
	}

	handler := NewTeamsAppPackageHandler(mockStore)
	req := httptest.NewRequest(
		"GET",
		fmt.Sprintf("/v1/teams/app-package?name=TestBot&instance_id=%s", instID.String()),
		nil,
	)
	req = req.WithContext(store.WithLocale(context.Background(), "en"))

	w := httptest.NewRecorder()
	handler.handleGenerate(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200. Response body: %s", w.Code, w.Body.String())
	}

	verifyZIPResponse(t, w)
}

func TestTeamsAppPackageHandler_BotIDQueryOverridesInstance(t *testing.T) {
	instID := uuid.New()
	creds := map[string]string{"bot_id": "from-instance-id"}
	credBytes, _ := json.Marshal(creds)
	mockStore := &MockChannelInstanceStore{
		instances: map[uuid.UUID]*store.ChannelInstanceData{
			instID: {
				BaseModel:   store.BaseModel{ID: instID},
				ChannelType: "teams",
				Credentials: credBytes,
			},
		},
	}

	handler := NewTeamsAppPackageHandler(mockStore)
	req := httptest.NewRequest(
		"GET",
		fmt.Sprintf("/v1/teams/app-package?name=TestBot&bot_id=550e8400-e29b-41d4-a716-446655440000&instance_id=%s", instID.String()),
		nil,
	)
	req = req.WithContext(store.WithLocale(context.Background(), "en"))

	w := httptest.NewRecorder()
	handler.handleGenerate(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	// Verify the manifest contains the query bot_id, not instance one
	manifest := extractManifestFromResponse(t, w)
	if manifest.ID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("manifest ID = %q, want 550e8400-e29b-41d4-a716-446655440000", manifest.ID)
	}
}

func TestTeamsAppPackageHandler_InvalidInstanceID(t *testing.T) {
	handler := NewTeamsAppPackageHandler(nil)
	req := httptest.NewRequest(
		"GET",
		"/v1/teams/app-package?name=TestBot&instance_id=not-a-uuid",
		nil,
	)
	req = req.WithContext(store.WithLocale(context.Background(), "en"))

	w := httptest.NewRecorder()
	handler.handleGenerate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestTeamsAppPackageHandler_NonexistentInstance(t *testing.T) {
	mockStore := &MockChannelInstanceStore{
		instances: make(map[uuid.UUID]*store.ChannelInstanceData),
	}

	handler := NewTeamsAppPackageHandler(mockStore)
	req := httptest.NewRequest(
		"GET",
		fmt.Sprintf("/v1/teams/app-package?name=TestBot&instance_id=%s", uuid.New().String()),
		nil,
	)
	req = req.WithContext(store.WithLocale(context.Background(), "en"))

	w := httptest.NewRecorder()
	handler.handleGenerate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestTeamsAppPackageHandler_BotIDFromInstanceNonStringValue(t *testing.T) {
	instID := uuid.New()
	// Simulate credentials with numeric bot_id (non-string value)
	credBytes := []byte(`{"bot_id": 12345, "bot_password": "secret"}`)
	mockStore := &MockChannelInstanceStore{
		instances: map[uuid.UUID]*store.ChannelInstanceData{
			instID: {
				BaseModel:   store.BaseModel{ID: instID},
				ChannelType: "teams",
				Credentials: credBytes,
			},
		},
	}

	handler := NewTeamsAppPackageHandler(mockStore)
	req := httptest.NewRequest(
		"GET",
		fmt.Sprintf("/v1/teams/app-package?name=TestBot&instance_id=%s", instID.String()),
		nil,
	)
	req = req.WithContext(store.WithLocale(context.Background(), "en"))

	w := httptest.NewRecorder()
	handler.handleGenerate(w, req)

	// Should succeed — bot_id should be coerced from number to string
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200. Response body: %s", w.Code, w.Body.String())
	}
}

func TestTeamsAppPackageHandler_AllParameters(t *testing.T) {
	handler := NewTeamsAppPackageHandler(nil)
	url := "/v1/teams/app-package?" +
		"name=MyBot&" +
		"full_name=My+Bot+Full+Name&" +
		"description=A+test+bot&" +
		"bot_id=550e8400-e29b-41d4-a716-446655440000&" +
		"developer=Test+Company"
	req := httptest.NewRequest("GET", url, nil)
	req = req.WithContext(store.WithLocale(context.Background(), "en"))

	w := httptest.NewRecorder()
	handler.handleGenerate(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	manifest := extractManifestFromResponse(t, w)
	if manifest.Name.Short != "MyBot" {
		t.Errorf("Name.Short = %q, want 'MyBot'", manifest.Name.Short)
	}
	if manifest.Name.Full != "My Bot Full Name" {
		t.Errorf("Name.Full = %q, want 'My Bot Full Name'", manifest.Name.Full)
	}
	if manifest.Description.Short != "A test bot" {
		t.Errorf("Description.Short = %q, want 'A test bot'", manifest.Description.Short)
	}
	if manifest.Developer.Name != "Test Company" {
		t.Errorf("Developer.Name = %q, want 'Test Company'", manifest.Developer.Name)
	}
}

func TestTeamsAppPackageHandler_ContentType(t *testing.T) {
	handler := NewTeamsAppPackageHandler(nil)
	req := httptest.NewRequest(
		"GET",
		"/v1/teams/app-package?name=TestBot&bot_id=550e8400-e29b-41d4-a716-446655440000",
		nil,
	)
	req = req.WithContext(store.WithLocale(context.Background(), "en"))

	w := httptest.NewRecorder()
	handler.handleGenerate(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want 'application/zip'", ct)
	}
}

func TestTeamsAppPackageHandler_ContentDisposition(t *testing.T) {
	handler := NewTeamsAppPackageHandler(nil)
	botName := "My-Test-Bot"
	req := httptest.NewRequest(
		"GET",
		fmt.Sprintf("/v1/teams/app-package?name=%s&bot_id=550e8400-e29b-41d4-a716-446655440000", botName),
		nil,
	)
	req = req.WithContext(store.WithLocale(context.Background(), "en"))

	w := httptest.NewRecorder()
	handler.handleGenerate(w, req)

	cd := w.Header().Get("Content-Disposition")
	if cd == "" {
		t.Error("Content-Disposition header missing")
	}
	if !bytes.Contains([]byte(cd), []byte("teams-app-")) {
		t.Errorf("Content-Disposition missing 'teams-app-': %q", cd)
	}
	if !bytes.Contains([]byte(cd), []byte(".zip")) {
		t.Errorf("Content-Disposition missing '.zip': %q", cd)
	}
}

func TestTeamsAppPackageHandler_SanitizeFilename(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		contains string
	}{
		{"special chars", "MyBot!@#$", "MyBot"},
		{"long name", "MyVeryLongBotNameThatExceedsFiftyCharactersAndShouldBeTruncated123", "MyVeryLongBotNameThatExceedsFift"},
		{"spaces", "My+Bot+Name", "My-Bot-Name"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewTeamsAppPackageHandler(nil)
			url := fmt.Sprintf("/v1/teams/app-package?name=%s&bot_id=550e8400-e29b-41d4-a716-446655440000", tc.input)
			req := httptest.NewRequest("GET", url, nil)
			req = req.WithContext(store.WithLocale(context.Background(), "en"))

			w := httptest.NewRecorder()
			handler.handleGenerate(w, req)

			cd := w.Header().Get("Content-Disposition")
			if !bytes.Contains([]byte(cd), []byte(tc.contains)) {
				t.Errorf("Content-Disposition missing %q: %q", tc.contains, cd)
			}
		})
	}
}

func TestSanitizeAppFilename(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"MyBot", "MyBot"},
		{"My Bot", "My-Bot"},
		{"My!@#$%Bot", "My-----Bot"},
		{"test_bot-name", "test_bot-name"},
		{"!@#$", "bot"},
		// Trailing dashes after truncation should be trimmed
		{"a" + strings.Repeat("!", 60), "a"},
		// All-special-chars long string should become "bot"
		{strings.Repeat("!", 100), "bot"},
	}

	for _, tc := range cases {
		result := sanitizeAppFilename(tc.input)
		if result != tc.expected {
			t.Errorf("sanitizeAppFilename(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestSanitizeAppFilename_Length(t *testing.T) {
	longName := "a"
	for i := 0; i < 100; i++ {
		longName += "b"
	}

	result := sanitizeAppFilename(longName)
	if len(result) > 50 {
		t.Errorf("sanitizeAppFilename result length = %d, want <= 50", len(result))
	}
}

// Helper functions

func verifyZIPResponse(t *testing.T, w *httptest.ResponseRecorder) {
	body := w.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("Failed to read ZIP response: %v", err)
	}

	if len(zr.File) != 3 {
		t.Errorf("ZIP file count = %d, want 3", len(zr.File))
	}

	expectedFiles := []string{"manifest.json", "color.png", "outline.png"}
	for i, f := range zr.File {
		if i >= len(expectedFiles) {
			t.Errorf("extra file in ZIP: %s", f.Name)
			continue
		}
		if f.Name != expectedFiles[i] {
			t.Errorf("file[%d] name = %q, want %q", i, f.Name, expectedFiles[i])
		}
	}
}

func extractManifestFromResponse(t *testing.T, w *httptest.ResponseRecorder) *appmanifest.Manifest {
	body := w.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("Failed to read ZIP response: %v", err)
	}

	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("Open manifest.json: %v", err)
			}
			defer rc.Close()

			m := &appmanifest.Manifest{}
			if err := json.NewDecoder(rc).Decode(m); err != nil {
				t.Fatalf("Decode manifest.json: %v", err)
			}
			return m
		}
	}
	t.Fatal("manifest.json not found in response")
	return nil
}
