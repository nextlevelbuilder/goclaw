// Package backup provides system-level backup and restore for GoClaw.
package backup

// BackupManifest describes the contents and metadata of a system backup archive.
type BackupManifest struct {
	Version       int         `json:"version"`          // always 1
	Format        string      `json:"format"`           // "goclaw-system-backup"
	CreatedAt     string      `json:"created_at"`       // RFC3339
	CreatedBy     string      `json:"created_by"`       // user ID or "cli"
	GoclawVersion string      `json:"goclaw_version"`
	SchemaVersion int         `json:"schema_version"`
	PgDumpVersion string      `json:"pg_dump_version"`  // pg_dump --version output or "sqlite"
	DatabaseDSN   string      `json:"database_dsn"`     // sanitized (no password)
	Paths         PathsInfo   `json:"paths"`
	Stats         BackupStats `json:"stats"`
}

// PathsInfo records the source directories included in the backup.
type PathsInfo struct {
	DataDir   string `json:"data_dir"`
	Workspace string `json:"workspace"`
}

// BackupStats records size and file counts for the backup contents.
type BackupStats struct {
	DatabaseSizeBytes int64 `json:"database_size_bytes"`
	FilesystemFiles   int   `json:"filesystem_files"`
	FilesystemBytes   int64 `json:"filesystem_bytes"`
	TotalBytes        int64 `json:"total_bytes"`
}
