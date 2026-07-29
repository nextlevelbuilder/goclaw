package tools

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const defaultDelegationArtifactFailureTTL = 60 * time.Minute

// NewDelegationArtifactExchange creates:
//
//	<tenant-workspace>/collaboration/delegations/<delegation-id>/{inputs,outputs}
//
// tenantWorkspace must already be resolved through the canonical tenant path
// helper. Both IDs are mandatory non-zero UUIDs.
func NewDelegationArtifactExchange(
	tenantWorkspace string,
	tenantID uuid.UUID,
	delegationID uuid.UUID,
	limits DelegationArtifactLimits,
	failureTTL time.Duration,
) (*DelegationArtifactExchange, error) {
	if tenantWorkspace == "" || tenantID == uuid.Nil || delegationID == uuid.Nil {
		return nil, artifactError(
			"artifact_invalid_identity", "create_exchange", "", ErrArtifactInvalidPath,
		)
	}
	validLimits, err := limits.validate()
	if err != nil {
		return nil, err
	}
	if failureTTL <= 0 {
		failureTTL = defaultDelegationArtifactFailureTTL
	}

	tenantRoot, err := openArtifactSecureRoot(tenantWorkspace)
	if err != nil {
		return nil, wrapArtifactFilesystemError("capture_tenant_root", "", err)
	}
	defer tenantRoot.close()

	const exchangeParent = "collaboration/delegations"
	if err := tenantRoot.mkdirAll(exchangeParent, 0750); err != nil {
		return nil, wrapArtifactFilesystemError("create_exchange_parent", "", err)
	}
	exchangeRelative := path.Join(exchangeParent, delegationID.String())
	exchangeRoot, err := tenantRoot.createSubroot(exchangeRelative, 0700)
	if err != nil {
		if isArtifactAlreadyExists(err) {
			return nil, artifactError(
				"artifact_exchange_conflict", "create_exchange", "", ErrArtifactPublishConflict,
			)
		}
		return nil, wrapArtifactFilesystemError("create_exchange", "", err)
	}
	if err := exchangeRoot.mkdirAll("inputs", 0750); err != nil {
		exchangeRoot.close()
		return nil, wrapArtifactFilesystemError("create_inputs", "", err)
	}
	if err := exchangeRoot.mkdirAll("outputs", 0700); err != nil {
		exchangeRoot.close()
		return nil, wrapArtifactFilesystemError("create_outputs", "", err)
	}

	exchange := &DelegationArtifactExchange{
		tenantID:     tenantID,
		delegationID: delegationID,
		hostRoot:     filepath.Join(tenantWorkspace, filepath.FromSlash(exchangeRelative)),
		root:         exchangeRoot,
		limits:       validLimits,
		failureTTL:   failureTTL,
		state:        artifactExchangeOpen,
	}
	startedAt := time.Now().UTC()
	lifecycleState := newStagingDelegationArtifactLifecycleState(
		tenantID,
		delegationID,
		startedAt,
		startedAt.Add(failureTTL),
	)
	if err := exchange.persistLifecycleState(lifecycleState); err != nil {
		_ = exchange.Close()
		_ = tenantRoot.removeTree(exchangeRelative)
		return nil, err
	}
	return exchange, nil
}

func (e *DelegationArtifactExchange) inputsHostPath() string {
	return filepath.Join(e.hostRoot, "inputs")
}

func (e *DelegationArtifactExchange) outputsHostPath() string {
	return filepath.Join(e.hostRoot, "outputs")
}

// StageInputs copies already-authorized relative files from the captured caller
// workspace. Duplicate basenames receive deterministic suffixes.
func (e *DelegationArtifactExchange) StageInputs(
	ctx context.Context,
	source *DelegationArtifactRoot,
	relativePaths []string,
) ([]DelegationArtifact, error) {
	if source == nil || source.root == nil {
		return nil, artifactError("artifact_missing_source_root", "stage_inputs", "", ErrArtifactState)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != artifactExchangeOpen {
		return nil, artifactError("artifact_invalid_state", "stage_inputs", "", ErrArtifactState)
	}
	if len(relativePaths) > e.limits.MaxFiles-e.inputCount {
		err := artifactError("artifact_file_limit", "stage_inputs", "", ErrArtifactLimitExceeded)
		e.retainFailureLocked(time.Now(), artifactErrorCode(err))
		return nil, err
	}

	usedNames := make(map[string]struct{}, len(e.inputs)+len(relativePaths))
	for _, input := range e.inputs {
		usedNames[strings.TrimPrefix(input.Path, "inputs/")] = struct{}{}
	}
	staged := make([]DelegationArtifact, 0, len(relativePaths))
	for _, rawPath := range relativePaths {
		if err := checkArtifactContext(ctx); err != nil {
			e.retainFailureLocked(time.Now(), "artifact_context_cancelled")
			return nil, err
		}
		sourcePath, err := validateArtifactRelativePath(rawPath)
		if err != nil {
			e.retainFailureLocked(time.Now(), artifactErrorCode(err))
			return nil, err
		}
		name := nextArtifactInputName(path.Base(sourcePath), usedNames)
		logicalPath := path.Join("inputs", name)
		remaining := e.limits.MaxTotalBytes - e.inputBytes
		artifact, err := copyArtifactFile(
			ctx,
			source.root,
			sourcePath,
			e.root,
			logicalPath,
			e.limits.MaxFileBytes,
			remaining,
			0440,
		)
		if err != nil {
			e.retainFailureLocked(time.Now(), artifactErrorCode(err))
			return nil, err
		}
		artifact.Status = DelegationArtifactStaged
		e.inputCount++
		e.inputBytes += artifact.SizeBytes
		e.inputs = append(e.inputs, artifact)
		staged = append(staged, artifact)
		usedNames[name] = struct{}{}
	}
	return append([]DelegationArtifact(nil), staged...), nil
}

func (e *DelegationArtifactExchange) StagedInputs() []DelegationArtifact {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]DelegationArtifact(nil), e.inputs...)
}

func nextArtifactInputName(base string, used map[string]struct{}) string {
	if _, exists := used[base]; !exists {
		return base
	}
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}

func artifactErrorCode(err error) string {
	var artifactErr *DelegationArtifactError
	if errors.As(err, &artifactErr) && artifactErr.Code != "" {
		return artifactErr.Code
	}
	return "artifact_exchange_failed"
}
