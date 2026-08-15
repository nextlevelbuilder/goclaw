package skills

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type systemPackageRecord struct {
	Name    string `json:"name"`
	Package string `json:"package"`
	Manager string `json:"manager"`
}

func systemPackageRecordsPath() string {
	return filepath.Join(packageRuntimeDir(), "system-packages.json")
}

func addSystemPackageRecord(requested, resolved, manager string) error {
	record := systemPackageRecord{
		Name:    normalizeSystemPackageDisplayName(requested),
		Package: strings.ToLower(strings.TrimSpace(resolved)),
		Manager: strings.ToLower(strings.TrimSpace(manager)),
	}
	if record.Name == "" || record.Package == "" || record.Manager == "" {
		return nil
	}

	records, err := readSystemPackageRecords()
	if err != nil {
		return err
	}
	for i, existing := range records {
		if existing.Manager == record.Manager && (existing.Name == record.Name || existing.Package == record.Package) {
			records[i] = record
			return writeSystemPackageRecords(records)
		}
	}
	records = append(records, record)
	return writeSystemPackageRecords(records)
}

func removeSystemPackageRecord(requested, resolved, manager string) error {
	wantName := normalizeSystemPackageDisplayName(requested)
	wantPackage := strings.ToLower(strings.TrimSpace(resolved))
	wantManager := strings.ToLower(strings.TrimSpace(manager))

	records, err := readSystemPackageRecords()
	if err != nil {
		return err
	}
	filtered := records[:0]
	for _, record := range records {
		if record.Manager == wantManager && (record.Name == wantName || record.Package == wantPackage) {
			continue
		}
		filtered = append(filtered, record)
	}
	return writeSystemPackageRecords(filtered)
}

func readSystemPackageRecords() ([]systemPackageRecord, error) {
	data, err := os.ReadFile(systemPackageRecordsPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var records []systemPackageRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func writeSystemPackageRecords(records []systemPackageRecord) error {
	path := systemPackageRecordsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func normalizeSystemPackageDisplayName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
