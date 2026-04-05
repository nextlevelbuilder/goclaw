package browser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStorageManager_ListProfiles_Empty(t *testing.T) {
	dir := t.TempDir()
	sm := NewStorageManager(dir, nil)
	profiles, err := sm.ListProfiles("default")
	if err != nil {
		t.Fatalf("ListProfiles error: %v", err)
	}
	if len(profiles) != 0 {
		t.Errorf("expected 0 profiles, got %d", len(profiles))
	}
}

func TestStorageManager_ListProfiles_WithProfiles(t *testing.T) {
	dir := t.TempDir()
	sm := NewStorageManager(dir, nil)

	// Create profile dirs
	base := filepath.Join(dir, "browser", "profiles", "default")
	for _, name := range []string{"profile-a", "profile-b"} {
		profileDir := filepath.Join(base, name)
		if err := os.MkdirAll(profileDir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", profileDir, err)
		}
		// Write a small file to give it non-zero size
		if err := os.WriteFile(filepath.Join(profileDir, "data.bin"), []byte("hello"), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	profiles, err := sm.ListProfiles("default")
	if err != nil {
		t.Fatalf("ListProfiles error: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}

	names := map[string]bool{}
	for _, p := range profiles {
		names[p.Name] = true
		if p.SizeBytes == 0 {
			t.Errorf("profile %q should have non-zero size", p.Name)
		}
		if p.Size == "" {
			t.Errorf("profile %q should have human-readable size", p.Name)
		}
	}
	if !names["profile-a"] || !names["profile-b"] {
		t.Errorf("expected profile-a and profile-b, got %v", names)
	}
}

func TestStorageManager_ListProfiles_DefaultTenant(t *testing.T) {
	dir := t.TempDir()
	sm := NewStorageManager(dir, nil)

	// Empty tenantID should use "default"
	base := filepath.Join(dir, "browser", "profiles", "default", "p1")
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}

	profiles, err := sm.ListProfiles("")
	if err != nil {
		t.Fatalf("ListProfiles error: %v", err)
	}
	if len(profiles) != 1 {
		t.Errorf("expected 1 profile, got %d", len(profiles))
	}
}

func TestStorageManager_ResolveProfileDir_Valid(t *testing.T) {
	sm := NewStorageManager("/workspace", nil)
	dir, err := sm.ResolveProfileDir("tenant1", "my-profile")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "/workspace/browser/profiles/tenant1/my-profile"
	if dir != expected {
		t.Errorf("expected %q, got %q", expected, dir)
	}
}

func TestStorageManager_ResolveProfileDir_DefaultTenant(t *testing.T) {
	sm := NewStorageManager("/workspace", nil)
	dir, err := sm.ResolveProfileDir("", "test-profile")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/workspace/browser/profiles/default/test-profile" {
		t.Errorf("unexpected dir: %s", dir)
	}
}

func TestStorageManager_ResolveProfileDir_InvalidName(t *testing.T) {
	sm := NewStorageManager("/workspace", nil)

	invalid := []string{"../escape", "../../etc/passwd", "has space", "has/slash", "has.dot", ""}
	for _, name := range invalid {
		_, err := sm.ResolveProfileDir("default", name)
		if err == nil {
			t.Errorf("expected error for profile name %q, got nil", name)
		}
	}
}

func TestStorageManager_ResolveProfileDir_InvalidTenant(t *testing.T) {
	sm := NewStorageManager("/workspace", nil)

	_, err := sm.ResolveProfileDir("../escape", "valid-profile")
	if err == nil {
		t.Error("expected error for path traversal tenant ID")
	}

	_, err = sm.ResolveProfileDir("has/slash", "valid-profile")
	if err == nil {
		t.Error("expected error for slash in tenant ID")
	}
}

func TestStorageManager_DeleteProfile_Success(t *testing.T) {
	dir := t.TempDir()
	sm := NewStorageManager(dir, nil)

	// Create a profile
	profileDir := filepath.Join(dir, "browser", "profiles", "default", "to-delete")
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "data"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := sm.DeleteProfile("default", "to-delete"); err != nil {
		t.Fatalf("DeleteProfile error: %v", err)
	}

	// Verify deleted
	if _, err := os.Stat(profileDir); !os.IsNotExist(err) {
		t.Error("profile directory should be deleted")
	}
}

func TestStorageManager_DeleteProfile_NonexistentDir(t *testing.T) {
	dir := t.TempDir()
	sm := NewStorageManager(dir, nil)

	err := sm.DeleteProfile("default", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent profile")
	}
}

func TestStorageManager_DeleteProfile_InvalidName(t *testing.T) {
	dir := t.TempDir()
	sm := NewStorageManager(dir, nil)

	err := sm.DeleteProfile("default", "../escape")
	if err == nil {
		t.Fatal("expected error for path traversal profile name")
	}
}

func TestStorageManager_GetUsage_Empty(t *testing.T) {
	dir := t.TempDir()
	sm := NewStorageManager(dir, nil)

	usage, err := sm.GetUsage("default")
	if err != nil {
		t.Fatalf("GetUsage error: %v", err)
	}
	if usage != 0 {
		t.Errorf("expected 0 usage, got %d", usage)
	}
}

func TestStorageManager_GetUsage_WithData(t *testing.T) {
	dir := t.TempDir()
	sm := NewStorageManager(dir, nil)

	profileDir := filepath.Join(dir, "browser", "profiles", "default", "p1")
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 1024)
	if err := os.WriteFile(filepath.Join(profileDir, "data.bin"), data, 0644); err != nil {
		t.Fatal(err)
	}

	usage, err := sm.GetUsage("default")
	if err != nil {
		t.Fatalf("GetUsage error: %v", err)
	}
	if usage < 1024 {
		t.Errorf("expected at least 1024 bytes usage, got %d", usage)
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		got := humanSize(tt.bytes)
		if got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}
