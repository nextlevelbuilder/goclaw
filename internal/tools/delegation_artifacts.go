package tools

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const (
	DelegationArtifactManifestVersion = 1
	DelegationArtifactMaxFileBytes    = int64(50 * 1024 * 1024)
	DelegationArtifactMaxTotalBytes   = int64(100 * 1024 * 1024)
	DelegationArtifactMaxFiles        = 100
)

var (
	ErrArtifactInvalidPath       = errors.New("invalid delegation artifact path")
	ErrArtifactNonRegular        = errors.New("delegation artifact is not a regular file")
	ErrArtifactHardlink          = errors.New("delegation artifact has multiple hard links")
	ErrArtifactLimitExceeded     = errors.New("delegation artifact limit exceeded")
	ErrArtifactPublishConflict   = errors.New("delegation artifact publication conflict")
	ErrArtifactSecureUnavailable = errors.New("secure delegation artifact filesystem unavailable")
	ErrArtifactState             = errors.New("invalid delegation artifact state")
)

// DelegationArtifactError is safe to return across runtime boundaries: Path is
// always a logical relative path and never a host workspace path.
type DelegationArtifactError struct {
	Code string
	Op   string
	Path string
	Err  error
}

func (e *DelegationArtifactError) Error() string {
	switch {
	case e.Path != "":
		return fmt.Sprintf("%s %q: %v", e.Op, e.Path, e.Err)
	case e.Op != "":
		return fmt.Sprintf("%s: %v", e.Op, e.Err)
	default:
		return e.Err.Error()
	}
}

func (e *DelegationArtifactError) Unwrap() error { return e.Err }

type DelegationArtifactLimits struct {
	MaxFileBytes  int64
	MaxTotalBytes int64
	MaxFiles      int
}

func DefaultDelegationArtifactLimits() DelegationArtifactLimits {
	return DelegationArtifactLimits{
		MaxFileBytes:  DelegationArtifactMaxFileBytes,
		MaxTotalBytes: DelegationArtifactMaxTotalBytes,
		MaxFiles:      DelegationArtifactMaxFiles,
	}
}

func (l DelegationArtifactLimits) validate() (DelegationArtifactLimits, error) {
	if l == (DelegationArtifactLimits{}) {
		return DefaultDelegationArtifactLimits(), nil
	}
	if l.MaxFileBytes <= 0 || l.MaxTotalBytes <= 0 || l.MaxFiles <= 0 {
		return DelegationArtifactLimits{}, artifactError(
			"artifact_invalid_limits", "configure", "", ErrArtifactLimitExceeded,
		)
	}
	if l.MaxFileBytes > l.MaxTotalBytes {
		return DelegationArtifactLimits{}, artifactError(
			"artifact_invalid_limits", "configure", "", ErrArtifactLimitExceeded,
		)
	}
	return l, nil
}

type DelegationArtifactStatus string

const (
	DelegationArtifactStaged    DelegationArtifactStatus = "staged"
	DelegationArtifactPublished DelegationArtifactStatus = "published"
)

type DelegationArtifact struct {
	Path      string                   `json:"path"`
	SizeBytes int64                    `json:"size_bytes"`
	SHA256    string                   `json:"sha256"`
	MediaType string                   `json:"media_type"`
	Status    DelegationArtifactStatus `json:"status"`
}

type DelegationArtifactManifestOutput struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"media_type"`
}

// DelegationArtifactManifest is the durable, workspace-safe manifest. It
// deliberately excludes tenant IDs, agent IDs, host paths, prompts, and traces.
type DelegationArtifactManifest struct {
	SchemaVersion int                                `json:"schema_version"`
	DelegationID  string                             `json:"delegation_id"`
	PublishedAt   time.Time                          `json:"published_at"`
	OutputCount   int                                `json:"output_count"`
	OutputBytes   int64                              `json:"output_bytes"`
	Outputs       []DelegationArtifactManifestOutput `json:"outputs"`
}

type DelegationArtifactPublication struct {
	RootPath     string
	ManifestPath string
	Manifest     DelegationArtifactManifest
}

type DelegationArtifactMount struct {
	LogicalAlias  string
	HostRoot      string
	ContainerPath string
	ReadOnly      bool
}

type DelegationArtifactFailureRetention struct {
	FailedAt            time.Time
	RetainUntil         time.Time
	ReasonCode          string
	PublicationTempPath string
}

type delegationArtifactExchangeState string

const (
	artifactExchangeOpen      delegationArtifactExchangeState = "open"
	artifactExchangeFailed    delegationArtifactExchangeState = "failed_retained"
	artifactExchangePublished delegationArtifactExchangeState = "published"
)

// DelegationArtifactRoot retains a secure directory handle so later renames or
// replacements of the path used to open it cannot redirect artifact access.
type DelegationArtifactRoot struct {
	root *artifactSecureRoot
}

func OpenDelegationArtifactRoot(hostPath string) (*DelegationArtifactRoot, error) {
	root, err := openArtifactSecureRoot(hostPath)
	if err != nil {
		return nil, wrapArtifactFilesystemError("capture_root", "", err)
	}
	return &DelegationArtifactRoot{root: root}, nil
}

func (r *DelegationArtifactRoot) Close() error {
	if r == nil || r.root == nil {
		return nil
	}
	return r.root.close()
}

// DelegationArtifactExchange owns one tenant-scoped, delegation-scoped
// inputs/outputs exchange. Host paths are runtime-only and never serialized.
type DelegationArtifactExchange struct {
	tenantID     uuid.UUID
	delegationID uuid.UUID
	hostRoot     string
	root         *artifactSecureRoot
	limits       DelegationArtifactLimits
	failureTTL   time.Duration

	mu                  sync.Mutex
	state               delegationArtifactExchangeState
	inputCount          int
	inputBytes          int64
	inputs              []DelegationArtifact
	failure             *DelegationArtifactFailureRetention
	publicationTempPath string
}

func (e *DelegationArtifactExchange) DelegationID() uuid.UUID { return e.delegationID }
func (e *DelegationArtifactExchange) TenantID() uuid.UUID     { return e.tenantID }

// InputsMount is runtime wiring for the delegated sandbox. Do not include its
// HostRoot in prompts, tool results, logs, traces, or durable manifests.
func (e *DelegationArtifactExchange) InputsMount() DelegationArtifactMount {
	return DelegationArtifactMount{
		LogicalAlias:  "inputs",
		HostRoot:      e.inputsHostPath(),
		ContainerPath: "/workspace/inputs",
		ReadOnly:      true,
	}
}

// OutputsHostPath is the delegated run's ephemeral read/write workspace.
func (e *DelegationArtifactExchange) OutputsHostPath() string {
	return e.outputsHostPath()
}

func (e *DelegationArtifactExchange) Close() error {
	if e == nil || e.root == nil {
		return nil
	}
	return e.root.close()
}

func (e *DelegationArtifactExchange) FailureRetention() (DelegationArtifactFailureRetention, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failure == nil {
		return DelegationArtifactFailureRetention{}, false
	}
	return *e.failure, true
}

func (e *DelegationArtifactExchange) ReadyForCleanup(now time.Time) bool {
	retention, ok := e.FailureRetention()
	return ok && !now.Before(retention.RetainUntil)
}

func (e *DelegationArtifactExchange) RetainFailure(now time.Time, reasonCode string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.retainFailureLocked(now, reasonCode)
}

func (e *DelegationArtifactExchange) retainFailureLocked(now time.Time, reasonCode string) {
	if e.state == artifactExchangePublished {
		return
	}
	if e.failure != nil {
		return
	}
	if reasonCode == "" {
		reasonCode = "artifact_exchange_failed"
	}
	e.state = artifactExchangeFailed
	e.failure = &DelegationArtifactFailureRetention{
		FailedAt:            now.UTC(),
		RetainUntil:         now.UTC().Add(e.failureTTL),
		ReasonCode:          reasonCode,
		PublicationTempPath: e.publicationTempPath,
	}
}

func artifactError(code, op, logicalPath string, err error) error {
	return &DelegationArtifactError{
		Code: code,
		Op:   op,
		Path: logicalPath,
		Err:  err,
	}
}

func wrapArtifactFilesystemError(op, logicalPath string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrArtifactSecureUnavailable) {
		return artifactError("artifact_secure_open_unavailable", op, logicalPath, err)
	}
	if errors.Is(err, ErrArtifactPublishConflict) {
		return artifactError("artifact_publish_conflict", op, logicalPath, err)
	}
	return artifactError("artifact_filesystem_error", op, logicalPath, err)
}

func validateArtifactRelativePath(raw string) (string, error) {
	if raw == "" ||
		strings.Contains(raw, `\`) ||
		strings.Contains(raw, ":") ||
		strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return "", artifactError("artifact_invalid_path", "validate_path", raw, ErrArtifactInvalidPath)
	}
	if strings.HasPrefix(raw, "/") {
		return "", artifactError("artifact_invalid_path", "validate_path", raw, ErrArtifactInvalidPath)
	}
	clean := path.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != raw {
		return "", artifactError("artifact_invalid_path", "validate_path", raw, ErrArtifactInvalidPath)
	}
	for component := range strings.SplitSeq(clean, "/") {
		if component == "" ||
			component == "." ||
			component == ".." ||
			strings.HasSuffix(component, ".") ||
			strings.HasSuffix(component, " ") ||
			isReservedArtifactComponent(component) {
			return "", artifactError("artifact_invalid_path", "validate_path", raw, ErrArtifactInvalidPath)
		}
	}
	return clean, nil
}

func isReservedArtifactComponent(component string) bool {
	base := component
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}

func checkArtifactContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
