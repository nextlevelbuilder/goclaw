package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

const (
	delegationArtifactLifecycleVersion  = 2
	delegationArtifactLifecycleFile     = ".goclaw-lifecycle.json"
	delegationArtifactLifecycleMaxBytes = 64 * 1024
)

type delegationArtifactLifecycleStatus string

const (
	artifactLifecycleStaging    delegationArtifactLifecycleStatus = "staging"
	artifactLifecycleRunning    delegationArtifactLifecycleStatus = "running"
	artifactLifecyclePublishing delegationArtifactLifecycleStatus = "publishing"
	artifactLifecyclePublished  delegationArtifactLifecycleStatus = "published"
	artifactLifecycleFailed     delegationArtifactLifecycleStatus = "failed"
	artifactLifecycleCancelled  delegationArtifactLifecycleStatus = "cancelled"
)

type delegationArtifactCallerLocation struct {
	Base         string `json:"base"`
	RelativePath string `json:"relative_path"`
}

// delegationArtifactLifecycleState contains only logical locations and stable
// identifiers. Host roots are reconstructed from runtime configuration and are
// never serialized.
type delegationArtifactLifecycleState struct {
	SchemaVersion       int                               `json:"schema_version"`
	DelegationID        string                            `json:"delegation_id"`
	TenantID            string                            `json:"tenant_id"`
	TenantSlug          string                            `json:"tenant_slug,omitempty"`
	Status              delegationArtifactLifecycleStatus `json:"status"`
	StartedAt           time.Time                         `json:"started_at"`
	FailedAt            *time.Time                        `json:"failed_at,omitempty"`
	RetainUntil         time.Time                         `json:"retain_until"`
	ReasonCode          string                            `json:"reason_code"`
	CallerLocation      *delegationArtifactCallerLocation `json:"caller_location,omitempty"`
	PublicationTempPath string                            `json:"publication_temp_path,omitempty"`
}

func newStagingDelegationArtifactLifecycleState(
	tenantID uuid.UUID,
	delegationID uuid.UUID,
	startedAt time.Time,
	retainUntil time.Time,
) delegationArtifactLifecycleState {
	return delegationArtifactLifecycleState{
		SchemaVersion: delegationArtifactLifecycleVersion,
		DelegationID:  delegationID.String(),
		TenantID:      tenantID.String(),
		Status:        artifactLifecycleStaging,
		StartedAt:     startedAt.UTC(),
		RetainUntil:   retainUntil.UTC(),
		ReasonCode:    "artifact_staging",
	}
}

func persistDelegationArtifactLifecycleState(
	root *artifactSecureRoot,
	delegationID uuid.UUID,
	state delegationArtifactLifecycleState,
) error {
	if root == nil {
		return artifactError("artifact_lifecycle_state_failed", "write_lifecycle_state", "", ErrArtifactState)
	}
	if err := validateDelegationArtifactLifecycleState(state, delegationID); err != nil {
		return err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return artifactError("artifact_lifecycle_state_failed", "marshal_lifecycle_state", "", err)
	}
	encoded = append(encoded, '\n')
	tempName := ".goclaw-lifecycle-" + uuid.NewString() + ".tmp"
	stateFile, err := root.createFile(tempName, 0600)
	if err != nil {
		return wrapArtifactFilesystemError("create_lifecycle_state", "", err)
	}
	removeTemp := true
	defer func() {
		_ = stateFile.Close()
		if removeTemp {
			_ = root.removeTree(tempName)
		}
	}()
	if _, err := stateFile.Write(encoded); err != nil {
		return artifactError("artifact_lifecycle_state_failed", "write_lifecycle_state", "", err)
	}
	if err := stateFile.Sync(); err != nil {
		return artifactError("artifact_lifecycle_state_failed", "sync_lifecycle_state", "", err)
	}
	if err := stateFile.Close(); err != nil {
		return artifactError("artifact_lifecycle_state_failed", "close_lifecycle_state", "", err)
	}
	if err := root.renameReplace(tempName, delegationArtifactLifecycleFile); err != nil {
		return wrapArtifactFilesystemError("publish_lifecycle_state", "", err)
	}
	removeTemp = false
	if err := root.syncDir("."); err != nil {
		return artifactError("artifact_lifecycle_state_failed", "sync_lifecycle_directory", "", err)
	}
	return nil
}

func (e *DelegationArtifactExchange) persistLifecycleState(state delegationArtifactLifecycleState) error {
	if e == nil {
		return artifactError("artifact_lifecycle_state_failed", "write_lifecycle_state", "", ErrArtifactState)
	}
	return persistDelegationArtifactLifecycleState(e.root, e.delegationID, state)
}

func readDelegationArtifactLifecycleState(
	root *artifactSecureRoot,
	delegationID uuid.UUID,
) (delegationArtifactLifecycleState, error) {
	entry, err := root.openEntry(delegationArtifactLifecycleFile)
	if err != nil {
		return delegationArtifactLifecycleState{}, err
	}
	defer entry.close()
	if entry.kind != artifactEntryRegular || entry.links != 1 ||
		entry.size <= 0 || entry.size > delegationArtifactLifecycleMaxBytes {
		return delegationArtifactLifecycleState{}, ErrArtifactState
	}
	encoded, err := io.ReadAll(io.LimitReader(entry.file, delegationArtifactLifecycleMaxBytes+1))
	if err != nil {
		return delegationArtifactLifecycleState{}, err
	}
	if len(encoded) > delegationArtifactLifecycleMaxBytes {
		return delegationArtifactLifecycleState{}, ErrArtifactState
	}
	var state delegationArtifactLifecycleState
	if err := json.Unmarshal(encoded, &state); err != nil {
		return delegationArtifactLifecycleState{}, err
	}
	if err := validateDelegationArtifactLifecycleState(state, delegationID); err != nil {
		return delegationArtifactLifecycleState{}, err
	}
	return state, nil
}

func validateDelegationArtifactLifecycleState(
	state delegationArtifactLifecycleState,
	delegationID uuid.UUID,
) error {
	if state.SchemaVersion != delegationArtifactLifecycleVersion ||
		state.DelegationID != delegationID.String() ||
		state.TenantID == "" ||
		state.StartedAt.IsZero() ||
		state.RetainUntil.IsZero() ||
		state.ReasonCode == "" {
		return artifactError("artifact_lifecycle_state_invalid", "validate_lifecycle_state", "", ErrArtifactState)
	}
	switch state.Status {
	case artifactLifecycleStaging, artifactLifecycleRunning:
		if state.FailedAt != nil || state.PublicationTempPath != "" {
			return artifactError("artifact_lifecycle_state_invalid", "validate_lifecycle_state", "", ErrArtifactState)
		}
	case artifactLifecyclePublishing, artifactLifecyclePublished:
		if state.FailedAt != nil ||
			validateDelegationPublicationTempPath(delegationID, state.PublicationTempPath) != nil {
			return artifactError("artifact_lifecycle_state_invalid", "validate_lifecycle_state", "", ErrArtifactState)
		}
	case artifactLifecycleFailed, artifactLifecycleCancelled:
		if state.FailedAt == nil {
			return artifactError("artifact_lifecycle_state_invalid", "validate_lifecycle_state", "", ErrArtifactState)
		}
	default:
		return artifactError("artifact_lifecycle_state_invalid", "validate_lifecycle_state", "", ErrArtifactState)
	}
	if state.CallerLocation != nil {
		if state.CallerLocation.Base != "workspace" && state.CallerLocation.Base != "data" {
			return artifactError("artifact_lifecycle_state_invalid", "validate_lifecycle_state", "", ErrArtifactState)
		}
		if state.CallerLocation.RelativePath == "" {
			return artifactError("artifact_lifecycle_state_invalid", "validate_lifecycle_state", "", ErrArtifactState)
		}
	}
	if state.PublicationTempPath != "" {
		if err := validateDelegationPublicationTempPath(delegationID, state.PublicationTempPath); err != nil {
			return fmt.Errorf("invalid publication temp path: %w", err)
		}
	}
	return nil
}
