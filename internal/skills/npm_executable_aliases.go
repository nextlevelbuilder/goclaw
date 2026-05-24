package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type npmPackageBinManifest struct {
	Name string          `json:"name"`
	Bin  json.RawMessage `json:"bin"`
}

func findNpmPackageExecutableAlias(name string) (string, bool) {
	packageDirs, err := npmGlobalPackageDirs()
	if err != nil {
		return "", false
	}
	for _, packageDir := range packageDirs {
		raw, err := os.ReadFile(filepath.Join(packageDir, "package.json"))
		if err != nil {
			continue
		}
		var manifest npmPackageBinManifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			continue
		}
		if !npmPackageNameMatchesExecutableAlias(manifest.Name, name) {
			continue
		}
		binName, ok := singleNpmBinName(manifest)
		if !ok {
			continue
		}
		path := filepath.Join(npmGlobalBinDir(), binName)
		if IsExecutableFile(path) {
			return path, true
		}
	}
	return "", false
}

func npmGlobalPackageDirs() ([]string, error) {
	root := filepath.Join(npmGlobalPrefix(), "lib", "node_modules")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if strings.HasPrefix(entry.Name(), "@") {
			scoped, err := os.ReadDir(path)
			if err != nil {
				continue
			}
			for _, scopedEntry := range scoped {
				if scopedEntry.IsDir() {
					dirs = append(dirs, filepath.Join(path, scopedEntry.Name()))
				}
			}
			continue
		}
		dirs = append(dirs, path)
	}
	return dirs, nil
}

func npmPackageNameMatchesExecutableAlias(packageName, executableName string) bool {
	base := npmPackageBaseName(packageName)
	if base == "" {
		return false
	}
	if base == executableName {
		return true
	}
	return strings.TrimSuffix(base, "-cli") == executableName
}

func npmPackageBaseName(packageName string) string {
	packageName = strings.TrimSpace(packageName)
	if packageName == "" {
		return ""
	}
	if slash := strings.LastIndexByte(packageName, '/'); slash >= 0 {
		return packageName[slash+1:]
	}
	return packageName
}

func singleNpmBinName(manifest npmPackageBinManifest) (string, bool) {
	if len(manifest.Bin) == 0 || string(manifest.Bin) == "null" {
		return "", false
	}
	var binPath string
	if err := json.Unmarshal(manifest.Bin, &binPath); err == nil {
		return npmPackageBaseName(manifest.Name), strings.TrimSpace(binPath) != ""
	}
	var bins map[string]string
	if err := json.Unmarshal(manifest.Bin, &bins); err != nil || len(bins) != 1 {
		return "", false
	}
	for name, path := range bins {
		if strings.TrimSpace(name) != "" && strings.TrimSpace(path) != "" {
			return name, true
		}
	}
	return "", false
}
