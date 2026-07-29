package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Publish validates outputs and copies them into a sibling temporary directory
// beneath the captured caller root. One atomic no-replace rename makes the
// complete outputs tree and manifest visible together.
func (e *DelegationArtifactExchange) Publish(
	ctx context.Context,
	destination *DelegationArtifactRoot,
	publishedAt time.Time,
) (DelegationArtifactPublication, error) {
	return e.publish(ctx, destination, publishedAt, nil)
}

// publishWithPreparation records the exact sibling staging path before any
// publication data is created. The callback must durably persist that path.
func (e *DelegationArtifactExchange) publishWithPreparation(
	ctx context.Context,
	destination *DelegationArtifactRoot,
	publishedAt time.Time,
	onPrepared func(string) error,
) (DelegationArtifactPublication, error) {
	return e.publish(ctx, destination, publishedAt, onPrepared)
}

func (e *DelegationArtifactExchange) publish(
	ctx context.Context,
	destination *DelegationArtifactRoot,
	publishedAt time.Time,
	onPrepared func(string) error,
) (DelegationArtifactPublication, error) {
	if destination == nil || destination.root == nil {
		return DelegationArtifactPublication{}, artifactError(
			"artifact_missing_destination_root", "publish", "", ErrArtifactState,
		)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != artifactExchangeOpen {
		return DelegationArtifactPublication{}, artifactError(
			"artifact_invalid_state", "publish", "", ErrArtifactState,
		)
	}

	publication, err := e.publishLocked(ctx, destination.root, publishedAt, onPrepared)
	if err != nil {
		e.retainFailureLocked(time.Now(), artifactErrorCode(err))
		return DelegationArtifactPublication{}, err
	}
	e.state = artifactExchangePublished
	return publication, nil
}

func (e *DelegationArtifactExchange) publishLocked(
	ctx context.Context,
	destinationRoot *artifactSecureRoot,
	publishedAt time.Time,
	onPrepared func(string) error,
) (publication DelegationArtifactPublication, returnErr error) {
	if err := destinationRoot.mkdirAll(".delegations", 0750); err != nil {
		return DelegationArtifactPublication{}, classifyPublicationFilesystemError(
			"open_publication_parent", "", err,
		)
	}
	publicationParent, err := destinationRoot.openSubroot(".delegations")
	if err != nil {
		return DelegationArtifactPublication{}, classifyPublicationFilesystemError(
			"open_publication_parent", "", err,
		)
	}
	defer publicationParent.close()

	finalName := e.delegationID.String()
	if exists, err := publicationParent.exists(finalName); err != nil {
		return DelegationArtifactPublication{}, wrapArtifactFilesystemError(
			"check_publication_destination", "", err,
		)
	} else if exists {
		return DelegationArtifactPublication{}, artifactError(
			"artifact_publish_conflict", "publish", "", ErrArtifactPublishConflict,
		)
	}

	tempName := ".tmp-" + finalName + "-" + uuid.NewString()
	tempPath := path.Join(".delegations", tempName)
	e.publicationTempPath = tempPath
	if onPrepared != nil {
		if err := onPrepared(tempPath); err != nil {
			return DelegationArtifactPublication{}, err
		}
	}
	tempRoot, err := publicationParent.createSubroot(tempName, 0700)
	if err != nil {
		return DelegationArtifactPublication{}, classifyPublicationFilesystemError(
			"create_publication_temp", "", err,
		)
	}
	tempClosed := false
	defer func() {
		if !tempClosed {
			if closeErr := tempRoot.close(); returnErr == nil && closeErr != nil {
				returnErr = artifactError(
					"artifact_close_failed", "close_publication_temp", "", closeErr,
				)
			}
			tempClosed = true
		}
	}()
	if err := tempRoot.mkdirAll("outputs", 0750); err != nil {
		return DelegationArtifactPublication{}, wrapArtifactFilesystemError(
			"create_publication_outputs", "", err,
		)
	}

	outputPaths, err := enumerateArtifactOutputs(e.root, "outputs", e.limits.MaxFiles-e.inputCount)
	if err != nil {
		return DelegationArtifactPublication{}, err
	}
	outputs := make([]DelegationArtifactManifestOutput, 0, len(outputPaths))
	outputBytes := int64(0)
	directories := map[string]struct{}{"outputs": {}}
	for _, outputPath := range outputPaths {
		if err := checkArtifactContext(ctx); err != nil {
			return DelegationArtifactPublication{}, err
		}
		if err := validateManifestOutputPath(outputPath); err != nil {
			return DelegationArtifactPublication{}, err
		}
		remaining := e.limits.MaxTotalBytes - e.inputBytes - outputBytes
		destinationPath := outputPath
		parent := path.Dir(destinationPath)
		if parent != "." {
			if err := tempRoot.mkdirAll(parent, 0750); err != nil {
				return DelegationArtifactPublication{}, wrapArtifactFilesystemError(
					"create_output_parent", destinationPath, err,
				)
			}
			for dir := parent; dir != "." && dir != "/"; dir = path.Dir(dir) {
				directories[dir] = struct{}{}
			}
		}
		artifact, err := copyArtifactFile(
			ctx,
			e.root,
			outputPath,
			tempRoot,
			destinationPath,
			e.limits.MaxFileBytes,
			remaining,
			0640,
		)
		if err != nil {
			return DelegationArtifactPublication{}, err
		}
		artifact.Status = DelegationArtifactPublished
		outputBytes += artifact.SizeBytes
		outputs = append(outputs, DelegationArtifactManifestOutput{
			Path:      artifact.Path,
			SizeBytes: artifact.SizeBytes,
			SHA256:    artifact.SHA256,
			MediaType: artifact.MediaType,
		})
	}

	sort.Slice(outputs, func(i, j int) bool { return outputs[i].Path < outputs[j].Path })
	manifest := DelegationArtifactManifest{
		SchemaVersion: DelegationArtifactManifestVersion,
		DelegationID:  finalName,
		PublishedAt:   publishedAt.UTC(),
		OutputCount:   len(outputs),
		OutputBytes:   outputBytes,
		Outputs:       outputs,
	}
	if manifest.Outputs == nil {
		manifest.Outputs = []DelegationArtifactManifestOutput{}
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return DelegationArtifactPublication{}, artifactError(
			"artifact_manifest_failed", "marshal_manifest", "", err,
		)
	}
	manifestBytes = append(manifestBytes, '\n')
	manifestFile, err := tempRoot.createFile("manifest.json", 0600)
	if err != nil {
		return DelegationArtifactPublication{}, wrapArtifactFilesystemError(
			"create_manifest", "manifest.json", err,
		)
	}
	if _, err := manifestFile.Write(manifestBytes); err != nil {
		manifestFile.Close()
		return DelegationArtifactPublication{}, artifactError(
			"artifact_manifest_failed", "write_manifest", "manifest.json", err,
		)
	}
	if err := manifestFile.Sync(); err != nil {
		manifestFile.Close()
		return DelegationArtifactPublication{}, artifactError(
			"artifact_sync_failed", "sync_manifest", "manifest.json", err,
		)
	}
	if err := manifestFile.Chmod(0640); err != nil {
		manifestFile.Close()
		return DelegationArtifactPublication{}, artifactError(
			"artifact_chmod_failed", "protect_manifest", "manifest.json", err,
		)
	}
	if err := manifestFile.Close(); err != nil {
		return DelegationArtifactPublication{}, artifactError(
			"artifact_close_failed", "close_manifest", "manifest.json", err,
		)
	}

	syncDirs := make([]string, 0, len(directories))
	for dir := range directories {
		syncDirs = append(syncDirs, dir)
	}
	sort.Slice(syncDirs, func(i, j int) bool {
		return strings.Count(syncDirs[i], "/") > strings.Count(syncDirs[j], "/")
	})
	for _, dir := range syncDirs {
		if err := tempRoot.syncDir(dir); err != nil {
			return DelegationArtifactPublication{}, artifactError(
				"artifact_sync_failed", "sync_directory", dir, err,
			)
		}
	}
	if err := tempRoot.syncDir("."); err != nil {
		return DelegationArtifactPublication{}, artifactError(
			"artifact_sync_failed", "sync_publication_temp", "", err,
		)
	}
	if err := tempRoot.close(); err != nil {
		return DelegationArtifactPublication{}, artifactError(
			"artifact_close_failed", "close_publication_temp", "", err,
		)
	}
	tempClosed = true
	if err := publicationParent.renameNoReplace(tempName, finalName); err != nil {
		return DelegationArtifactPublication{}, classifyPublicationFilesystemError(
			"publish", "", err,
		)
	}
	if err := publicationParent.syncDir("."); err != nil {
		return DelegationArtifactPublication{}, artifactError(
			"artifact_sync_failed", "sync_publication_parent", "", err,
		)
	}

	rootPath := path.Join(".delegations", finalName)
	return DelegationArtifactPublication{
		RootPath:     rootPath,
		ManifestPath: path.Join(rootPath, "manifest.json"),
		Manifest:     manifest,
	}, nil
}

func enumerateArtifactOutputs(
	root *artifactSecureRoot,
	base string,
	maxFiles int,
) ([]string, error) {
	if maxFiles < 0 {
		return nil, artifactError(
			"artifact_file_limit", "enumerate_outputs", "", ErrArtifactLimitExceeded,
		)
	}
	var outputs []string
	var walk func(string) error
	walk = func(dir string) error {
		entries, err := root.readDir(dir)
		if err != nil {
			return classifyArtifactOpenError("enumerate_outputs", dir, err)
		}
		sort.Strings(entries)
		for _, name := range entries {
			logicalPath := path.Join(dir, name)
			entry, err := root.openEntry(logicalPath)
			if err != nil {
				return classifyArtifactOpenError("open_output", logicalPath, err)
			}
			switch entry.kind {
			case artifactEntryDirectory:
				entry.close()
				if err := walk(logicalPath); err != nil {
					return err
				}
			case artifactEntryRegular:
				links := entry.links
				entry.close()
				if links != 1 {
					return artifactError(
						"artifact_hardlink", "open_output", logicalPath, ErrArtifactHardlink,
					)
				}
				outputs = append(outputs, logicalPath)
				if len(outputs) > maxFiles {
					return artifactError(
						"artifact_file_limit", "enumerate_outputs", logicalPath, ErrArtifactLimitExceeded,
					)
				}
			default:
				entry.close()
				return artifactError(
					"artifact_non_regular", "open_output", logicalPath, ErrArtifactNonRegular,
				)
			}
		}
		return nil
	}
	if err := walk(base); err != nil {
		return nil, err
	}
	sort.Strings(outputs)
	return outputs, nil
}

func classifyPublicationFilesystemError(op, logicalPath string, err error) error {
	if isArtifactAlreadyExists(err) || errors.Is(err, ErrArtifactPublishConflict) {
		return artifactError("artifact_publish_conflict", op, logicalPath, ErrArtifactPublishConflict)
	}
	return wrapArtifactFilesystemError(op, logicalPath, err)
}

func validateManifestOutputPath(raw string) error {
	clean, err := validateArtifactRelativePath(raw)
	if err != nil {
		return err
	}
	if clean == "outputs" || !strings.HasPrefix(clean, "outputs/") {
		return fmt.Errorf("%w: manifest output is outside outputs", ErrArtifactInvalidPath)
	}
	return nil
}
