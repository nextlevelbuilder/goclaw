// Package plugins — Reconciliation-based plugin install (CP-08).
// Kubernetes-style: declare desired state, reconcile with actual state.
package plugins

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// PluginDeclaration represents a desired plugin state from config.
type PluginDeclaration struct {
	Name    string `json:"name"`
	Source  string `json:"source"`  // "local:/path", "git:url", "marketplace:name"
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
}

// ReconcileDiff describes the difference between desired and actual state.
type ReconcileDiff struct {
	Missing       []PluginDeclaration // declared but not installed
	SourceChanged []PluginDeclaration // installed but source/version changed
	Removed       []string            // installed but not declared
	UpToDate      []string            // no changes needed
}

// Reconciler compares desired plugin declarations with actual installed state.
type Reconciler struct {
	registry   *Registry
	pluginsDir string // base directory for plugin installations
}

// NewReconciler creates a reconciler targeting the given registry and directory.
func NewReconciler(registry *Registry, pluginsDir string) *Reconciler {
	return &Reconciler{
		registry:   registry,
		pluginsDir: pluginsDir,
	}
}

// Diff computes the delta between declared and installed plugins.
func (r *Reconciler) Diff(declared []PluginDeclaration) ReconcileDiff {
	var diff ReconcileDiff

	installedNames := make(map[string]bool)
	for _, p := range r.registry.List() {
		installedNames[p.Manifest.Name] = true
	}

	declaredNames := make(map[string]bool)
	for _, d := range declared {
		declaredNames[d.Name] = true

		existing := r.registry.Get(d.Name)
		if existing == nil {
			diff.Missing = append(diff.Missing, d)
		} else if existing.Source != d.Source || existing.Manifest.Version != d.Version {
			diff.SourceChanged = append(diff.SourceChanged, d)
		} else {
			diff.UpToDate = append(diff.UpToDate, d.Name)
		}
	}

	for name := range installedNames {
		if !declaredNames[name] {
			diff.Removed = append(diff.Removed, name)
		}
	}

	return diff
}

// Apply reconciles the given diff by installing, updating, and removing plugins.
func (r *Reconciler) Apply(ctx context.Context, diff ReconcileDiff) error {
	// Install missing
	for _, d := range diff.Missing {
		slog.Info("reconciler: installing plugin", "name", d.Name, "source", d.Source)
		if err := r.installPlugin(ctx, d); err != nil {
			slog.Error("reconciler: install failed", "name", d.Name, "err", err)
			// Register as error state
			r.registry.Upsert(&Plugin{
				Manifest: PluginManifest{Name: d.Name, Version: d.Version},
				State:    PluginError,
				Source:   d.Source,
				Error:    err.Error(),
				LoadedAt: time.Now(),
			})
		}
	}

	// Update changed
	for _, d := range diff.SourceChanged {
		slog.Info("reconciler: updating plugin", "name", d.Name, "source", d.Source)
		r.registry.Remove(d.Name)
		if err := r.installPlugin(ctx, d); err != nil {
			slog.Error("reconciler: update failed", "name", d.Name, "err", err)
		}
	}

	// Remove undeclared
	for _, name := range diff.Removed {
		slog.Info("reconciler: removing plugin", "name", name)
		r.registry.Remove(name)
	}

	return nil
}

// Reconcile runs diff + apply in one step.
func (r *Reconciler) Reconcile(ctx context.Context, declared []PluginDeclaration) error {
	diff := r.Diff(declared)

	slog.Info("reconciler: diff computed",
		"missing", len(diff.Missing),
		"changed", len(diff.SourceChanged),
		"removed", len(diff.Removed),
		"up_to_date", len(diff.UpToDate))

	return r.Apply(ctx, diff)
}

func (r *Reconciler) installPlugin(ctx context.Context, d PluginDeclaration) error {
	pluginDir := filepath.Join(r.pluginsDir, d.Name)

	// For local source, just read from path
	if len(d.Source) > 6 && d.Source[:6] == "local:" {
		localPath := d.Source[6:]
		return r.loadFromPath(localPath, d)
	}

	// For other sources, create plugin directory
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return fmt.Errorf("create plugin dir: %w", err)
	}

	// TODO: implement git clone, marketplace download
	return fmt.Errorf("source type not yet implemented: %s", d.Source)
}

func (r *Reconciler) loadFromPath(path string, d PluginDeclaration) error {
	manifestPath := filepath.Join(path, "plugin.yaml")
	manifest, err := ParseManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("parse manifest at %s: %w", manifestPath, err)
	}

	if err := ValidateManifest(manifest); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	state := PluginDisabled
	if d.Enabled {
		state = PluginEnabled
	}

	r.registry.Upsert(&Plugin{
		Manifest: *manifest,
		State:    state,
		Path:     path,
		Source:   d.Source,
		LoadedAt: time.Now(),
	})

	return nil
}
