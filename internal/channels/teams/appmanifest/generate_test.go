package appmanifest

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"testing"
)

// TestGenerateZIPMinimal tests happy path with just BotID and Name
func TestGenerateZIPMinimal(t *testing.T) {
	opts := Options{
		BotID: "550e8400-e29b-41d4-a716-446655440000",
		Name:  "TestBot",
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	// Verify ZIP is valid
	if len(zipData) == 0 {
		t.Fatal("ZIP data is empty")
	}

	// Parse ZIP
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	// Verify exactly 3 files
	if len(zr.File) != 3 {
		t.Errorf("ZIP contains %d files, want 3", len(zr.File))
	}

	// Verify file names
	fileNames := map[string]bool{}
	for _, f := range zr.File {
		fileNames[f.Name] = true
	}

	for _, wantName := range []string{"manifest.json", "color.png", "outline.png"} {
		if !fileNames[wantName] {
			t.Errorf("ZIP missing file: %s", wantName)
		}
	}
}

// TestGenerateZIPFull tests with all fields set
func TestGenerateZIPFull(t *testing.T) {
	customColor := createTestPNG(t, 192, 192)
	customOutline := createTestPNG(t, 32, 32)

	opts := Options{
		BotID:           "550e8400-e29b-41d4-a716-446655440000",
		Name:            "MyAwesomeBot",
		FullName:        "My Awesome Bot - Full Name",
		Description:     "Short description",
		FullDescription: "Full description with much more details",
		Developer:       "My Company",
		WebsiteURL:      "https://example.com",
		PrivacyURL:      "https://example.com/privacy",
		TermsURL:        "https://example.com/terms",
		Version:         "2.1.0",
		Scopes:          []string{"personal", "team"},
		ColorIcon:       customColor,
		OutlineIcon:     customOutline,
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	// Extract and verify manifest
	m := extractManifest(t, zipData)

	// Verify all fields
	if m.ID != opts.BotID {
		t.Errorf("ID = %q, want %q", m.ID, opts.BotID)
	}
	if m.Name.Short != opts.Name {
		t.Errorf("Name.Short = %q, want %q", m.Name.Short, opts.Name)
	}
	if m.Name.Full != opts.FullName {
		t.Errorf("Name.Full = %q, want %q", m.Name.Full, opts.FullName)
	}
	if m.Description.Short != opts.Description {
		t.Errorf("Description.Short = %q, want %q", m.Description.Short, opts.Description)
	}
	if m.Description.Full != opts.FullDescription {
		t.Errorf("Description.Full = %q, want %q", m.Description.Full, opts.FullDescription)
	}
	if m.Developer.Name != opts.Developer {
		t.Errorf("Developer.Name = %q, want %q", m.Developer.Name, opts.Developer)
	}
	if m.Developer.WebsiteURL != opts.WebsiteURL {
		t.Errorf("Developer.WebsiteURL = %q, want %q", m.Developer.WebsiteURL, opts.WebsiteURL)
	}
	if m.Developer.PrivacyURL != opts.PrivacyURL {
		t.Errorf("Developer.PrivacyURL = %q, want %q", m.Developer.PrivacyURL, opts.PrivacyURL)
	}
	if m.Developer.TermsURL != opts.TermsURL {
		t.Errorf("Developer.TermsURL = %q, want %q", m.Developer.TermsURL, opts.TermsURL)
	}
	if m.Version != opts.Version {
		t.Errorf("Version = %q, want %q", m.Version, opts.Version)
	}

	// Verify custom scopes
	if len(m.Bots) == 0 || len(m.Bots[0].Scopes) == 0 {
		t.Fatal("Bots or scopes empty")
	}
	if !stringSliceEqual(m.Bots[0].Scopes, opts.Scopes) {
		t.Errorf("Scopes = %v, want %v", m.Bots[0].Scopes, opts.Scopes)
	}

	// Verify custom icons used
	verifyZIPFileContent(t, zipData, "color.png", customColor)
	verifyZIPFileContent(t, zipData, "outline.png", customOutline)
}

// TestGenerateZIPEmptyBotID tests error on missing BotID
func TestGenerateZIPEmptyBotID(t *testing.T) {
	opts := Options{
		BotID: "",
		Name:  "TestBot",
	}

	_, err := GenerateZIP(opts)
	if err == nil {
		t.Error("expected error for empty bot_id")
	}
	if err.Error() != "bot_id is required: Teams channel not configured" {
		t.Errorf("error = %q, want 'bot_id is required: Teams channel not configured'", err.Error())
	}
}

// TestGenerateZIPEmptyName tests error on missing Name
func TestGenerateZIPEmptyName(t *testing.T) {
	opts := Options{
		BotID: "550e8400-e29b-41d4-a716-446655440000",
		Name:  "",
	}

	_, err := GenerateZIP(opts)
	if err == nil {
		t.Error("expected error for empty name")
	}
	if err.Error() != "name is required" {
		t.Errorf("error = %q, want 'name is required'", err.Error())
	}
}

// TestGenerateZIPNameTruncation tests Name > 30 chars is truncated
func TestGenerateZIPNameTruncation(t *testing.T) {
	longName := "This is a very long bot name that exceeds thirty characters"
	opts := Options{
		BotID: "550e8400-e29b-41d4-a716-446655440000",
		Name:  longName,
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	m := extractManifest(t, zipData)
	if len(m.Name.Short) > maxNameShort {
		t.Errorf("Name.Short length = %d, want <= %d", len(m.Name.Short), maxNameShort)
	}
	if m.Name.Short != longName[:maxNameShort] {
		t.Errorf("Name.Short = %q, want %q", m.Name.Short, longName[:maxNameShort])
	}
}

// TestGenerateZIPDescriptionTruncation tests Description > 80 chars is truncated
func TestGenerateZIPDescriptionTruncation(t *testing.T) {
	longDesc := "This is a very long description that exceeds eighty characters and should be truncated by the system"
	opts := Options{
		BotID:       "550e8400-e29b-41d4-a716-446655440000",
		Name:        "Bot",
		Description: longDesc,
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	m := extractManifest(t, zipData)
	if len(m.Description.Short) > maxDescShort {
		t.Errorf("Description.Short length = %d, want <= %d", len(m.Description.Short), maxDescShort)
	}
	if m.Description.Short != longDesc[:maxDescShort] {
		t.Errorf("Description.Short truncated incorrectly")
	}
}

// TestGenerateZIPFullDescriptionTruncation tests FullDescription > 4000 chars is truncated
func TestGenerateZIPFullDescriptionTruncation(t *testing.T) {
	// Create a string > 4000 chars
	longDesc := ""
	for i := 0; i < 500; i++ {
		longDesc += "1234567890"
	}
	if len(longDesc) <= maxDescFull {
		t.Fatalf("test setup: longDesc not long enough: %d <= %d", len(longDesc), maxDescFull)
	}

	opts := Options{
		BotID:           "550e8400-e29b-41d4-a716-446655440000",
		Name:            "Bot",
		FullDescription: longDesc,
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	m := extractManifest(t, zipData)
	if len(m.Description.Full) > maxDescFull {
		t.Errorf("Description.Full length = %d, want <= %d", len(m.Description.Full), maxDescFull)
	}
	if m.Description.Full != longDesc[:maxDescFull] {
		t.Errorf("Description.Full truncated incorrectly")
	}
}

// TestGenerateZIPCustomColorIconValidPNG tests custom PNG icon is used
func TestGenerateZIPCustomColorIconValidPNG(t *testing.T) {
	customIcon := createTestPNG(t, 192, 192)

	opts := Options{
		BotID:     "550e8400-e29b-41d4-a716-446655440000",
		Name:      "Bot",
		ColorIcon: customIcon,
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	verifyZIPFileContent(t, zipData, "color.png", customIcon)
}

// TestGenerateZIPCustomOutlineIconValidPNG tests custom outline icon is used
func TestGenerateZIPCustomOutlineIconValidPNG(t *testing.T) {
	customIcon := createTestPNG(t, 32, 32)

	opts := Options{
		BotID:        "550e8400-e29b-41d4-a716-446655440000",
		Name:         "Bot",
		OutlineIcon:  customIcon,
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	verifyZIPFileContent(t, zipData, "outline.png", customIcon)
}

// TestGenerateZIPCustomIconInvalidPNG tests invalid PNG is rejected
func TestGenerateZIPCustomIconInvalidPNG(t *testing.T) {
	// JPEG magic bytes instead of PNG
	jpegBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}

	opts := Options{
		BotID:     "550e8400-e29b-41d4-a716-446655440000",
		Name:      "Bot",
		ColorIcon: jpegBytes,
	}

	_, err := GenerateZIP(opts)
	if err == nil {
		t.Error("expected error for invalid PNG")
	}
	if err.Error() != "color icon: invalid PNG format" {
		t.Errorf("error = %q, want 'color icon: invalid PNG format'", err.Error())
	}
}

// TestGenerateZIPCustomIconOversizeInvalidPNG tests invalid PNG with outline icon
func TestGenerateZIPCustomIconInvalidPNGOutline(t *testing.T) {
	jpegBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}

	opts := Options{
		BotID:       "550e8400-e29b-41d4-a716-446655440000",
		Name:        "Bot",
		OutlineIcon: jpegBytes,
	}

	_, err := GenerateZIP(opts)
	if err == nil {
		t.Error("expected error for invalid PNG outline icon")
	}
	if err.Error() != "outline icon: invalid PNG format" {
		t.Errorf("error = %q, want 'outline icon: invalid PNG format'", err.Error())
	}
}

// TestGenerateZIPCustomIconOversizeColor tests icon > 500KB is rejected (color)
func TestGenerateZIPCustomIconOversizeColor(t *testing.T) {
	// Create valid PNG header + oversized data
	oversizeData := append(
		pngMagic,
		make([]byte, maxIconBytes+1)...,
	)

	opts := Options{
		BotID:     "550e8400-e29b-41d4-a716-446655440000",
		Name:      "Bot",
		ColorIcon: oversizeData,
	}

	_, err := GenerateZIP(opts)
	if err == nil {
		t.Error("expected error for oversized color icon")
	}
	if err.Error() != "color icon: exceeds 500KB limit (512009 bytes)" {
		t.Errorf("error = %q, want 'color icon: exceeds 500KB limit (512009 bytes)'", err.Error())
	}
}

// TestGenerateZIPCustomIconOversizeOutline tests icon > 500KB is rejected (outline)
func TestGenerateZIPCustomIconOversizeOutline(t *testing.T) {
	oversizeData := append(
		pngMagic,
		make([]byte, maxIconBytes+1)...,
	)

	opts := Options{
		BotID:       "550e8400-e29b-41d4-a716-446655440000",
		Name:        "Bot",
		OutlineIcon: oversizeData,
	}

	_, err := GenerateZIP(opts)
	if err == nil {
		t.Error("expected error for oversized outline icon")
	}
	if err.Error() != "outline icon: exceeds 500KB limit (512009 bytes)" {
		t.Errorf("error = %q, want 'outline icon: exceeds 500KB limit (512009 bytes)'", err.Error())
	}
}

// TestGenerateZIPCustomIconExactlyAtLimit tests icon exactly at 500KB is accepted
func TestGenerateZIPCustomIconExactlyAtLimit(t *testing.T) {
	// PNG magic + padding = exactly maxIconBytes
	data := append(pngMagic, make([]byte, maxIconBytes-len(pngMagic))...)
	if len(data) != maxIconBytes {
		t.Fatalf("test setup: data length %d, want %d", len(data), maxIconBytes)
	}

	opts := Options{
		BotID:     "550e8400-e29b-41d4-a716-446655440000",
		Name:      "Bot",
		ColorIcon: data,
	}

	_, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("expected no error for icon exactly at 500KB, got: %v", err)
	}
}

// TestGenerateZIPEmptyScopes tests that empty scopes slice gets defaults
func TestGenerateZIPEmptyScopes(t *testing.T) {
	opts := Options{
		BotID:  "550e8400-e29b-41d4-a716-446655440000",
		Name:   "Bot",
		Scopes: []string{},
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	m := extractManifest(t, zipData)
	expectedScopes := []string{"personal", "team", "groupChat"}
	if !stringSliceEqual(m.Bots[0].Scopes, expectedScopes) {
		t.Errorf("Scopes = %v, want %v (empty should get defaults)", m.Bots[0].Scopes, expectedScopes)
	}
}

// TestGenerateZIPUnicodeNameTruncation tests Unicode-safe truncation
func TestGenerateZIPUnicodeNameTruncation(t *testing.T) {
	// 31 CJK characters (each 3 bytes in UTF-8, but 1 rune)
	longUnicodeName := ""
	for i := 0; i < 31; i++ {
		longUnicodeName += "\u4e16" // Chinese char
	}
	opts := Options{
		BotID: "550e8400-e29b-41d4-a716-446655440000",
		Name:  longUnicodeName,
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	m := extractManifest(t, zipData)
	runes := []rune(m.Name.Short)
	if len(runes) > maxNameShort {
		t.Errorf("Name.Short rune count = %d, want <= %d", len(runes), maxNameShort)
	}
	if len(runes) != maxNameShort {
		t.Errorf("Name.Short rune count = %d, want exactly %d (truncated)", len(runes), maxNameShort)
	}
}

// TestGenerateZIPZIPStructure verifies ZIP structure has exactly 3 files
func TestGenerateZIPZIPStructure(t *testing.T) {
	opts := Options{
		BotID: "550e8400-e29b-41d4-a716-446655440000",
		Name:  "Bot",
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	if len(zr.File) != 3 {
		t.Errorf("ZIP file count = %d, want 3", len(zr.File))
	}

	// Verify file order and names
	expectedOrder := []string{"manifest.json", "color.png", "outline.png"}
	for i, f := range zr.File {
		if i >= len(expectedOrder) {
			t.Errorf("extra file in ZIP: %s", f.Name)
			continue
		}
		if f.Name != expectedOrder[i] {
			t.Errorf("file[%d] name = %q, want %q", i, f.Name, expectedOrder[i])
		}
	}
}

// TestGenerateZIPManifestSchema verifies manifest JSON schema and structure
func TestGenerateZIPManifestSchema(t *testing.T) {
	opts := Options{
		BotID: "550e8400-e29b-41d4-a716-446655440000",
		Name:  "TestBot",
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	m := extractManifest(t, zipData)

	// Verify schema
	if m.Schema != schemaURL {
		t.Errorf("Schema = %q, want %q", m.Schema, schemaURL)
	}

	// Verify manifest version
	if m.ManifestVersion != manifestVersion {
		t.Errorf("ManifestVersion = %q, want %q", m.ManifestVersion, manifestVersion)
	}

	// Verify bots array exists and has entry
	if len(m.Bots) == 0 {
		t.Fatal("Bots array is empty")
	}
	if m.Bots[0].BotID != opts.BotID {
		t.Errorf("Bots[0].BotID = %q, want %q", m.Bots[0].BotID, opts.BotID)
	}

	// Verify permissions
	if len(m.Permissions) == 0 {
		t.Error("Permissions array is empty")
	}
	expectedPerms := []string{"identity", "messageTeamMembers"}
	if !stringSliceEqual(m.Permissions, expectedPerms) {
		t.Errorf("Permissions = %v, want %v", m.Permissions, expectedPerms)
	}

	// Verify icons
	if m.Icons.Color != "color.png" {
		t.Errorf("Icons.Color = %q, want 'color.png'", m.Icons.Color)
	}
	if m.Icons.Outline != "outline.png" {
		t.Errorf("Icons.Outline = %q, want 'outline.png'", m.Icons.Outline)
	}
}

// TestGenerateZIPDefaultScopes verifies default scopes
func TestGenerateZIPDefaultScopes(t *testing.T) {
	opts := Options{
		BotID: "550e8400-e29b-41d4-a716-446655440000",
		Name:  "Bot",
		// Scopes not specified
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	m := extractManifest(t, zipData)
	expectedScopes := []string{"personal", "team", "groupChat"}
	if !stringSliceEqual(m.Bots[0].Scopes, expectedScopes) {
		t.Errorf("Scopes = %v, want %v", m.Bots[0].Scopes, expectedScopes)
	}
}

// TestGenerateZIPCustomScopes verifies custom scopes are applied
func TestGenerateZIPCustomScopes(t *testing.T) {
	customScopes := []string{"personal", "groupChat"}
	opts := Options{
		BotID:  "550e8400-e29b-41d4-a716-446655440000",
		Name:   "Bot",
		Scopes: customScopes,
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	m := extractManifest(t, zipData)
	if !stringSliceEqual(m.Bots[0].Scopes, customScopes) {
		t.Errorf("Scopes = %v, want %v", m.Bots[0].Scopes, customScopes)
	}
}

// TestGenerateZIPDefaultValues verifies default values are applied
func TestGenerateZIPDefaultValues(t *testing.T) {
	opts := Options{
		BotID: "550e8400-e29b-41d4-a716-446655440000",
		Name:  "Bot",
		// Everything else unset — should get defaults
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	m := extractManifest(t, zipData)

	if m.Name.Full != opts.Name {
		t.Errorf("FullName fallback = %q, want %q", m.Name.Full, opts.Name)
	}
	if m.Description.Short != "AI assistant powered by GoClaw" {
		t.Errorf("Description.Short = %q, want default", m.Description.Short)
	}
	if m.Description.Full != m.Description.Short {
		t.Errorf("Description.Full should match Short when FullDescription not set")
	}
	if m.Developer.Name != "GoClaw" {
		t.Errorf("Developer.Name = %q, want 'GoClaw'", m.Developer.Name)
	}
	if m.Developer.WebsiteURL != "https://goclaw.dev" {
		t.Errorf("Developer.WebsiteURL = %q, want 'https://goclaw.dev'", m.Developer.WebsiteURL)
	}
	if m.Developer.PrivacyURL != "https://goclaw.dev/privacy" {
		t.Errorf("Developer.PrivacyURL = %q, want 'https://goclaw.dev/privacy'", m.Developer.PrivacyURL)
	}
	if m.Developer.TermsURL != "https://goclaw.dev/terms" {
		t.Errorf("Developer.TermsURL = %q, want 'https://goclaw.dev/terms'", m.Developer.TermsURL)
	}
	if m.Version != "1.0.0" {
		t.Errorf("Version = %q, want '1.0.0'", m.Version)
	}
}

// TestGenerateZIPFullNameFallback verifies FullName falls back to Name
func TestGenerateZIPFullNameFallback(t *testing.T) {
	opts := Options{
		BotID: "550e8400-e29b-41d4-a716-446655440000",
		Name:  "ShortName",
		// FullName not set
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	m := extractManifest(t, zipData)
	if m.Name.Full != "ShortName" {
		t.Errorf("Name.Full = %q, want 'ShortName'", m.Name.Full)
	}
}

// TestGenerateZIPDescriptionFullFallback verifies FullDescription falls back to Description
func TestGenerateZIPDescriptionFullFallback(t *testing.T) {
	opts := Options{
		BotID:       "550e8400-e29b-41d4-a716-446655440000",
		Name:        "Bot",
		Description: "Short desc",
		// FullDescription not set
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	m := extractManifest(t, zipData)
	if m.Description.Full != "Short desc" {
		t.Errorf("Description.Full = %q, want 'Short desc'", m.Description.Full)
	}
}

// TestGenerateZIPManifestJsonValid verifies manifest.json is valid JSON
func TestGenerateZIPManifestJsonValid(t *testing.T) {
	opts := Options{
		BotID: "550e8400-e29b-41d4-a716-446655440000",
		Name:  "Bot",
	}

	zipData, err := GenerateZIP(opts)
	if err != nil {
		t.Fatalf("GenerateZIP: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	// Find and extract manifest.json
	var manifestFile *zip.File
	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			manifestFile = f
			break
		}
	}

	if manifestFile == nil {
		t.Fatal("manifest.json not found in ZIP")
	}

	rc, err := manifestFile.Open()
	if err != nil {
		t.Fatalf("Open manifest.json: %v", err)
	}
	defer rc.Close()

	var m Manifest
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		t.Fatalf("Decode manifest.json: %v", err)
	}
}

// Helper functions

func createTestPNG(t *testing.T, width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("creating test PNG: %v", err)
	}
	return buf.Bytes()
}

func extractManifest(t *testing.T, zipData []byte) Manifest {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	var m Manifest
	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("Open manifest.json: %v", err)
			}
			defer rc.Close()

			if err := json.NewDecoder(rc).Decode(&m); err != nil {
				t.Fatalf("Decode manifest.json: %v", err)
			}
			return m
		}
	}
	t.Fatal("manifest.json not found in ZIP")
	return Manifest{}
}

func verifyZIPFileContent(t *testing.T, zipData []byte, filename string, expectedContent []byte) {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	for _, f := range zr.File {
		if f.Name == filename {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("Open %s: %v", filename, err)
			}
			defer rc.Close()

			var content bytes.Buffer
			if _, err := content.ReadFrom(rc); err != nil {
				t.Fatalf("Read %s: %v", filename, err)
			}

			if !bytes.Equal(content.Bytes(), expectedContent) {
				t.Errorf("%s content mismatch: got %d bytes, want %d bytes", filename, content.Len(), len(expectedContent))
			}
			return
		}
	}
	t.Errorf("%s not found in ZIP", filename)
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
