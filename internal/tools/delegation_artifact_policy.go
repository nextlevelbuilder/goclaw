package tools

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// resolveDelegationInputPath maps the logical inputs/ alias to the captured
// exchange input directory. The host root never needs to be disclosed to the
// delegated model.
func resolveDelegationInputPath(ctx context.Context, raw string) (string, bool, error) {
	root := DelegationArtifactInputsFromCtx(ctx)
	if root == "" || filepath.IsAbs(raw) {
		return "", false, nil
	}
	logical := path.Clean(filepath.ToSlash(strings.TrimSpace(raw)))
	if logical == "inputs" {
		return filepath.Clean(root), true, nil
	}
	if !strings.HasPrefix(logical, "inputs/") {
		return "", false, nil
	}
	relative, err := validateArtifactRelativePath(strings.TrimPrefix(logical, "inputs/"))
	if err != nil {
		return "", true, err
	}
	resolved, err := resolvePathWithAllowed(
		filepath.FromSlash(relative),
		root,
		true,
		nil,
	)
	if err != nil {
		return "", true, fmt.Errorf("cannot access delegation input")
	}
	return resolved, true, nil
}

func rejectDelegationInputMutation(ctx context.Context, raw string) error {
	if _, handled, err := resolveDelegationInputPath(ctx, raw); handled {
		if err != nil {
			return err
		}
		return fmt.Errorf("delegation inputs are read-only")
	}
	return nil
}

// resolveStructuredMediaPath resolves logical delegation inputs for media
// readers without widening filesystem-tool authority. Outside an artifact run
// it preserves the normal workspace and Agent Team read rules.
func resolveStructuredMediaPath(ctx context.Context, raw, kind string) (string, error) {
	if resolved, handled, err := resolveDelegationInputPath(ctx, raw); handled {
		if err != nil {
			return "", fmt.Errorf("invalid %s delegation input path", kind)
		}
		if err := ValidateRegularFileForRead(resolved); err != nil {
			return "", fmt.Errorf("%s delegation input is not a safe regular file", kind)
		}
		return resolved, nil
	}

	workspace := ToolWorkspaceFromCtx(ctx)
	resolved, err := resolvePathWithAllowed(raw, workspace, effectiveRestrict(ctx, true), allowedWithTeamWorkspace(ctx, nil))
	if err != nil {
		return "", fmt.Errorf("invalid %s path: %w", kind, err)
	}
	if err := checkDeniedPath(resolved, workspace, nil); err != nil {
		return "", err
	}
	if err := ValidateRegularFileForRead(resolved); err != nil {
		return "", fmt.Errorf("%s path is not a safe regular file: %w", kind, err)
	}
	return filepath.Clean(resolved), nil
}
