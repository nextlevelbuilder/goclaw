package http

import (
	"context"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

func lifecycleCheckSkillDeps(m *skills.SkillManifest) (bool, []string) {
	ok, missing := checkSkillDeps(m)
	missing = appendUniqueDeps(missing, missingGitHubSkillDeps(m)...)
	return ok && len(missing) == 0, missing
}

func missingGitHubSkillDeps(m *skills.SkillManifest) []string {
	if m == nil {
		return nil
	}
	var missing []string
	for _, raw := range m.Explicit {
		if strings.HasPrefix(raw, "github:") && !githubSkillDependencyInstalled(raw) {
			missing = append(missing, raw)
		}
	}
	return missing
}

func skillGitHubDependencyInstalled(raw string) bool {
	spec, err := skills.ParseGitHubSpec(raw)
	if err != nil {
		return false
	}
	installer := skills.DefaultGitHubInstaller()
	if installer == nil {
		return false
	}
	entries, err := installer.List()
	if err != nil {
		return false
	}
	wantRepo := spec.Owner + "/" + spec.Repo
	for _, entry := range entries {
		if strings.EqualFold(entry.Repo, wantRepo) && (spec.Tag == "" || entry.Tag == spec.Tag) {
			return true
		}
	}
	return false
}

func installLifecycleDeps(ctx context.Context, manifest *skills.SkillManifest, missing []string) (*skills.InstallResult, error) {
	bulkMissing, githubMissing := splitGitHubMissingDeps(missing)
	result := &skills.InstallResult{}
	if len(bulkMissing) > 0 {
		bulkResult, err := installManagedDeps(ctx, manifest, bulkMissing)
		if err != nil {
			return nil, err
		}
		mergeInstallResult(result, bulkResult)
	}
	for _, dep := range githubMissing {
		ok, errMsg := installSingleDep(ctx, dep)
		name := strings.TrimPrefix(dep, "github:")
		if !ok {
			result.Errors = append(result.Errors, "github "+name+": "+errMsg)
			continue
		}
		result.GitHub = append(result.GitHub, name)
	}
	return result, nil
}

func splitGitHubMissingDeps(missing []string) ([]string, []string) {
	var bulkMissing []string
	var githubMissing []string
	for _, dep := range missing {
		if strings.HasPrefix(dep, "github:") {
			githubMissing = append(githubMissing, dep)
			continue
		}
		bulkMissing = append(bulkMissing, dep)
	}
	return bulkMissing, githubMissing
}

func mergeInstallResult(dst, src *skills.InstallResult) {
	if dst == nil || src == nil {
		return
	}
	dst.System = append(dst.System, src.System...)
	dst.Pip = append(dst.Pip, src.Pip...)
	dst.Npm = append(dst.Npm, src.Npm...)
	dst.GitHub = append(dst.GitHub, src.GitHub...)
	dst.Errors = append(dst.Errors, src.Errors...)
}

func dependencyItems(m *skills.SkillManifest, missing []string) []skillDependencyItem {
	missingSet := dependencyMissingSet(missing)
	var out []skillDependencyItem
	add := func(source, name string) {
		status := "installed"
		if missingSet[dependencyKey(source, name)] {
			status = "missing"
		}
		out = append(out, skillDependencyItem{Source: source, Name: name, Status: status})
	}
	for _, name := range m.Requires {
		add("system", name)
	}
	for _, name := range m.RequiresPython {
		add("pip", name)
	}
	for _, name := range m.RequiresNode {
		add("npm", name)
	}
	for _, raw := range m.Explicit {
		if after, ok := strings.CutPrefix(raw, "github:"); ok {
			add("github", after)
		}
	}
	return out
}

func dependencyMissingSet(missing []string) map[string]bool {
	out := make(map[string]bool, len(missing))
	for _, dep := range missing {
		source, name := splitDependency(dep)
		out[dependencyKey(source, name)] = true
	}
	return out
}

func splitDependency(dep string) (string, string) {
	for _, prefix := range []string{"pip:", "npm:", "github:", "system:"} {
		if after, ok := strings.CutPrefix(dep, prefix); ok {
			return strings.TrimSuffix(prefix, ":"), after
		}
	}
	return "system", dep
}

func dependencyKey(source, name string) string { return source + ":" + name }

func appendUniqueDeps(dst []string, src ...string) []string {
	seen := make(map[string]bool, len(dst)+len(src))
	for _, dep := range dst {
		seen[dep] = true
	}
	for _, dep := range src {
		if seen[dep] {
			continue
		}
		dst = append(dst, dep)
		seen[dep] = true
	}
	return dst
}

func dependencyStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "missing"
}
