package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
)

type artifactSniffWriter struct {
	buf []byte
}

func (w *artifactSniffWriter) Write(p []byte) (int, error) {
	if len(w.buf) < 512 {
		remaining := min(512-len(w.buf), len(p))
		w.buf = append(w.buf, p[:remaining]...)
	}
	return len(p), nil
}

func copyArtifactFile(
	ctx context.Context,
	sourceRoot *artifactSecureRoot,
	sourcePath string,
	destinationRoot *artifactSecureRoot,
	destinationPath string,
	maxFileBytes int64,
	maxRemainingBytes int64,
	destinationMode os.FileMode,
) (DelegationArtifact, error) {
	if maxRemainingBytes < 0 {
		return DelegationArtifact{}, artifactError(
			"artifact_total_byte_limit", "copy", destinationPath, ErrArtifactLimitExceeded,
		)
	}
	source, err := sourceRoot.openEntry(sourcePath)
	if err != nil {
		return DelegationArtifact{}, classifyArtifactOpenError("open_source", sourcePath, err)
	}
	defer source.close()
	if source.kind != artifactEntryRegular {
		return DelegationArtifact{}, artifactError(
			"artifact_non_regular", "open_source", sourcePath, ErrArtifactNonRegular,
		)
	}
	if source.links != 1 {
		return DelegationArtifact{}, artifactError(
			"artifact_hardlink", "open_source", sourcePath, ErrArtifactHardlink,
		)
	}
	effectiveLimit := min(maxRemainingBytes, maxFileBytes)
	if source.size > effectiveLimit {
		return DelegationArtifact{}, artifactError(
			"artifact_byte_limit", "copy", destinationPath, ErrArtifactLimitExceeded,
		)
	}

	destination, err := destinationRoot.createFile(destinationPath, 0600)
	if err != nil {
		return DelegationArtifact{}, wrapArtifactFilesystemError("create_destination", destinationPath, err)
	}
	closeDestination := true
	defer func() {
		if closeDestination {
			_ = destination.Close()
		}
	}()

	hash := sha256.New()
	sniff := &artifactSniffWriter{}
	writer := io.MultiWriter(destination, hash, sniff)
	reader := io.LimitReader(&contextArtifactReader{ctx: ctx, reader: source.file}, effectiveLimit+1)
	written, err := io.Copy(writer, reader)
	if err != nil {
		return DelegationArtifact{}, artifactError("artifact_copy_failed", "copy", destinationPath, err)
	}
	if written > effectiveLimit {
		return DelegationArtifact{}, artifactError(
			"artifact_byte_limit", "copy", destinationPath, ErrArtifactLimitExceeded,
		)
	}
	if err := destination.Sync(); err != nil {
		return DelegationArtifact{}, artifactError(
			"artifact_sync_failed", "sync_file", destinationPath, err,
		)
	}
	if err := destination.Chmod(destinationMode); err != nil {
		return DelegationArtifact{}, artifactError(
			"artifact_chmod_failed", "protect_file", destinationPath, err,
		)
	}
	if err := destination.Close(); err != nil {
		return DelegationArtifact{}, artifactError(
			"artifact_close_failed", "close_file", destinationPath, err,
		)
	}
	closeDestination = false

	mediaType := "application/octet-stream"
	if len(sniff.buf) > 0 {
		mediaType = http.DetectContentType(sniff.buf)
	}
	return DelegationArtifact{
		Path:      destinationPath,
		SizeBytes: written,
		SHA256:    hex.EncodeToString(hash.Sum(nil)),
		MediaType: mediaType,
	}, nil
}

type contextArtifactReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextArtifactReader) Read(p []byte) (int, error) {
	if err := checkArtifactContext(r.ctx); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func classifyArtifactOpenError(op, logicalPath string, err error) error {
	switch {
	case errors.Is(err, errArtifactSymlink):
		return artifactError("artifact_symlink", op, logicalPath, ErrArtifactNonRegular)
	case errors.Is(err, errArtifactReparsePoint):
		return artifactError("artifact_reparse_point", op, logicalPath, ErrArtifactNonRegular)
	default:
		return wrapArtifactFilesystemError(op, logicalPath, err)
	}
}
