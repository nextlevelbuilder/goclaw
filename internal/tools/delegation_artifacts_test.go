package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDelegationArtifactExchangeTenantLayoutAndInputContract(t *testing.T) {
	tenantRoot := t.TempDir()
	sourcePath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sourcePath, "one"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sourcePath, "two"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourcePath, "one", "report.txt"), []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourcePath, "two", "report.txt"), []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}

	tenantID := uuid.New()
	delegationID := uuid.New()
	exchange := newTestDelegationExchange(t, tenantRoot, tenantID, delegationID, DelegationArtifactLimits{})
	source := openTestArtifactRoot(t, sourcePath)
	staged, err := exchange.StageInputs(
		context.Background(),
		source,
		[]string{"one/report.txt", "two/report.txt"},
	)
	if err != nil {
		t.Fatalf("StageInputs() error = %v", err)
	}
	if got, want := len(staged), 2; got != want {
		t.Fatalf("staged count = %d, want %d", got, want)
	}
	if staged[0].Path != "inputs/report.txt" || staged[1].Path != "inputs/report-2.txt" {
		t.Fatalf("staged paths = %#v", []string{staged[0].Path, staged[1].Path})
	}

	wantExchangeRoot := filepath.Join(
		tenantRoot,
		"collaboration",
		"delegations",
		delegationID.String(),
	)
	if got := exchange.OutputsHostPath(); got != filepath.Join(wantExchangeRoot, "outputs") {
		t.Fatalf("OutputsHostPath() = %q, want %q", got, filepath.Join(wantExchangeRoot, "outputs"))
	}
	mount := exchange.InputsMount()
	if mount.LogicalAlias != "inputs" || mount.ContainerPath != "/workspace/inputs" || !mount.ReadOnly {
		t.Fatalf("InputsMount() = %#v", mount)
	}
	if mount.HostRoot != filepath.Join(wantExchangeRoot, "inputs") {
		t.Fatalf("input host root = %q", mount.HostRoot)
	}
	for _, name := range []string{"report.txt", "report-2.txt"} {
		info, err := os.Stat(filepath.Join(mount.HostRoot, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0222 != 0 {
			t.Fatalf("%s mode = %o, want read-only", name, info.Mode().Perm())
		}
	}
}

func TestDelegationArtifactStageRejectsTraversalSymlinksAndHardlinks(t *testing.T) {
	testCases := []struct {
		name    string
		prepare func(t *testing.T, source string) string
		wantErr error
	}{
		{
			name: "traversal",
			prepare: func(t *testing.T, source string) string {
				return "../outside.txt"
			},
			wantErr: ErrArtifactInvalidPath,
		},
		{
			name: "absolute",
			prepare: func(t *testing.T, source string) string {
				return filepath.Join(source, "outside.txt")
			},
			wantErr: ErrArtifactInvalidPath,
		},
		{
			name: "alternate data stream",
			prepare: func(t *testing.T, source string) string {
				return "report.txt:secret"
			},
			wantErr: ErrArtifactInvalidPath,
		},
		{
			name: "reserved device",
			prepare: func(t *testing.T, source string) string {
				return "NUL.txt"
			},
			wantErr: ErrArtifactInvalidPath,
		},
		{
			name: "control character",
			prepare: func(t *testing.T, source string) string {
				return "report\n.txt"
			},
			wantErr: ErrArtifactInvalidPath,
		},
		{
			name: "symlink",
			prepare: func(t *testing.T, source string) string {
				if runtime.GOOS == "windows" {
					t.Skip("symlink creation requires privileges on some Windows hosts")
				}
				if err := os.WriteFile(filepath.Join(source, "real.txt"), []byte("secret"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("real.txt", filepath.Join(source, "link.txt")); err != nil {
					t.Fatal(err)
				}
				return "link.txt"
			},
			wantErr: ErrArtifactNonRegular,
		},
		{
			name: "hardlink",
			prepare: func(t *testing.T, source string) string {
				if err := os.WriteFile(filepath.Join(source, "real.txt"), []byte("secret"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(filepath.Join(source, "real.txt"), filepath.Join(source, "hard.txt")); err != nil {
					t.Skipf("hardlinks unavailable: %v", err)
				}
				return "hard.txt"
			},
			wantErr: ErrArtifactHardlink,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			sourcePath := t.TempDir()
			input := testCase.prepare(t, sourcePath)
			exchange := newTestDelegationExchange(
				t,
				t.TempDir(),
				uuid.New(),
				uuid.New(),
				DelegationArtifactLimits{},
			)
			source := openTestArtifactRoot(t, sourcePath)
			_, err := exchange.StageInputs(context.Background(), source, []string{input})
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("StageInputs() error = %v, want errors.Is(%v)", err, testCase.wantErr)
			}
			retention, retained := exchange.FailureRetention()
			if !retained || retention.ReasonCode == "" || !retention.RetainUntil.After(retention.FailedAt) {
				t.Fatalf("failure retention = %#v, %v", retention, retained)
			}
		})
	}
}

func TestDelegationArtifactPublishManifestV1IsSortedAndPathSafe(t *testing.T) {
	exchange := newTestDelegationExchange(
		t,
		t.TempDir(),
		uuid.New(),
		uuid.New(),
		DelegationArtifactLimits{},
	)
	if err := os.MkdirAll(filepath.Join(exchange.OutputsHostPath(), "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(exchange.OutputsHostPath(), "z.txt"), []byte("z"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(exchange.OutputsHostPath(), "nested", "a.txt"), []byte("alpha"), 0600); err != nil {
		t.Fatal(err)
	}

	destinationPath := t.TempDir()
	destination := openTestArtifactRoot(t, destinationPath)
	publishedAt := time.Date(2026, 7, 29, 8, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	publication, err := exchange.Publish(context.Background(), destination, publishedAt)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if publication.RootPath != filepath.ToSlash(filepath.Join(".delegations", exchange.DelegationID().String())) {
		t.Fatalf("publication root = %q", publication.RootPath)
	}
	if publication.Manifest.SchemaVersion != 1 ||
		publication.Manifest.DelegationID != exchange.DelegationID().String() ||
		publication.Manifest.PublishedAt.Location() != time.UTC ||
		publication.Manifest.OutputCount != 2 ||
		publication.Manifest.OutputBytes != 6 {
		t.Fatalf("manifest = %#v", publication.Manifest)
	}
	if got := []string{
		publication.Manifest.Outputs[0].Path,
		publication.Manifest.Outputs[1].Path,
	}; got[0] != "outputs/nested/a.txt" || got[1] != "outputs/z.txt" {
		t.Fatalf("manifest output order = %#v", got)
	}
	for _, output := range publication.Manifest.Outputs {
		if err := validateManifestOutputPath(output.Path); err != nil {
			t.Fatalf("manifest output path %q invalid: %v", output.Path, err)
		}
		if len(output.SHA256) != 64 || output.MediaType == "" {
			t.Fatalf("manifest output = %#v", output)
		}
	}

	manifestBytes, err := os.ReadFile(filepath.Join(destinationPath, filepath.FromSlash(publication.ManifestPath)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifestBytes), destinationPath) ||
		strings.Contains(string(manifestBytes), exchange.OutputsHostPath()) {
		t.Fatalf("manifest leaked host path: %s", manifestBytes)
	}
	var durable DelegationArtifactManifest
	if err := json.Unmarshal(manifestBytes, &durable); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if durable.Outputs == nil || durable.OutputCount != len(durable.Outputs) {
		t.Fatalf("durable outputs = %#v", durable.Outputs)
	}
	var rawManifest map[string]json.RawMessage
	if err := json.Unmarshal(manifestBytes, &rawManifest); err != nil {
		t.Fatal(err)
	}
	if len(rawManifest) != 6 {
		t.Fatalf("manifest persisted unexpected fields: %v", rawManifest)
	}
	for _, forbidden := range []string{"tenant_id", "agent_id", "workspace", "trace_id", "status"} {
		if _, exists := rawManifest[forbidden]; exists {
			t.Fatalf("manifest persisted forbidden field %q", forbidden)
		}
	}
}

func TestDelegationArtifactPublishEmptyManifest(t *testing.T) {
	exchange := newTestDelegationExchange(
		t,
		t.TempDir(),
		uuid.New(),
		uuid.New(),
		DelegationArtifactLimits{},
	)
	destinationPath := t.TempDir()
	publication, err := exchange.Publish(
		context.Background(),
		openTestArtifactRoot(t, destinationPath),
		time.Unix(123, 0),
	)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if publication.Manifest.OutputCount != 0 ||
		publication.Manifest.OutputBytes != 0 ||
		publication.Manifest.Outputs == nil ||
		len(publication.Manifest.Outputs) != 0 {
		t.Fatalf("empty manifest = %#v", publication.Manifest)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(destinationPath, filepath.FromSlash(publication.ManifestPath)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifestBytes), `"outputs": []`) {
		t.Fatalf("empty outputs must encode as [], manifest: %s", manifestBytes)
	}
}

func TestDelegationArtifactPublishRejectsExcessEmptyDirectories(t *testing.T) {
	exchange := newTestDelegationExchange(
		t,
		t.TempDir(),
		uuid.New(),
		uuid.New(),
		DelegationArtifactLimits{},
	)
	for index := 0; index <= DelegationArtifactMaxFiles; index++ {
		name := fmt.Sprintf("empty-%03d", index)
		if err := os.Mkdir(filepath.Join(exchange.OutputsHostPath(), name), 0700); err != nil {
			t.Fatal(err)
		}
	}

	_, err := exchange.Publish(
		context.Background(),
		openTestArtifactRoot(t, t.TempDir()),
		time.Now(),
	)
	var artifactErr *DelegationArtifactError
	if !errors.Is(err, ErrArtifactLimitExceeded) ||
		!errors.As(err, &artifactErr) ||
		artifactErr.Code != "artifact_directory_limit" {
		t.Fatalf("Publish() error = %v, want artifact_directory_limit", err)
	}
}

func TestDelegationArtifactPublishRejectsDeepOutputTree(t *testing.T) {
	exchange := newTestDelegationExchange(
		t,
		t.TempDir(),
		uuid.New(),
		uuid.New(),
		DelegationArtifactLimits{},
	)
	current := exchange.OutputsHostPath()
	for depth := 0; depth <= artifactSecureMaxDepth; depth++ {
		current = filepath.Join(current, "d")
		if err := os.Mkdir(current, 0700); err != nil {
			t.Fatal(err)
		}
	}

	_, err := exchange.Publish(
		context.Background(),
		openTestArtifactRoot(t, t.TempDir()),
		time.Now(),
	)
	var artifactErr *DelegationArtifactError
	if !errors.Is(err, ErrArtifactLimitExceeded) ||
		!errors.As(err, &artifactErr) ||
		artifactErr.Code != "artifact_depth_limit" {
		t.Fatalf("Publish() error = %v, want artifact_depth_limit", err)
	}
}

func TestDelegationArtifactPublishRejectsLongLogicalOutputPath(t *testing.T) {
	exchange := newTestDelegationExchange(
		t,
		t.TempDir(),
		uuid.New(),
		uuid.New(),
		DelegationArtifactLimits{},
	)
	currentHostPath := exchange.OutputsHostPath()
	logicalPath := "outputs"
	for depth := 0; len(logicalPath) <= artifactOutputMaxPathBytes; depth++ {
		if depth >= artifactSecureMaxDepth {
			t.Fatal("test path reached depth limit before path-length limit")
		}
		component := fmt.Sprintf("%02d-%s", depth, strings.Repeat("x", 47))
		currentHostPath = filepath.Join(currentHostPath, component)
		logicalPath += "/" + component
		if err := os.Mkdir(currentHostPath, 0700); err != nil {
			t.Fatal(err)
		}
	}

	_, err := exchange.Publish(
		context.Background(),
		openTestArtifactRoot(t, t.TempDir()),
		time.Now(),
	)
	var artifactErr *DelegationArtifactError
	if !errors.Is(err, ErrArtifactLimitExceeded) ||
		!errors.As(err, &artifactErr) ||
		artifactErr.Code != "artifact_path_limit" {
		t.Fatalf("Publish() error = %v, want artifact_path_limit", err)
	}
}

func TestDelegationArtifactPublishPreservesRegularOutputFileLimit(t *testing.T) {
	limits := DelegationArtifactLimits{
		MaxFileBytes:  8,
		MaxTotalBytes: 32,
		MaxFiles:      2,
	}
	exchange := newTestDelegationExchange(
		t,
		t.TempDir(),
		uuid.New(),
		uuid.New(),
		limits,
	)
	for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
		if err := os.WriteFile(filepath.Join(exchange.OutputsHostPath(), name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}

	_, err := exchange.Publish(
		context.Background(),
		openTestArtifactRoot(t, t.TempDir()),
		time.Now(),
	)
	var artifactErr *DelegationArtifactError
	if !errors.Is(err, ErrArtifactLimitExceeded) ||
		!errors.As(err, &artifactErr) ||
		artifactErr.Code != "artifact_file_limit" {
		t.Fatalf("Publish() error = %v, want artifact_file_limit", err)
	}
}

func TestDelegationArtifactPublishRejectsSymlinkAndHardlinkOutputs(t *testing.T) {
	testCases := []struct {
		name    string
		prepare func(t *testing.T, exchange *DelegationArtifactExchange)
		wantErr error
	}{
		{
			name: "symlink",
			prepare: func(t *testing.T, exchange *DelegationArtifactExchange) {
				if runtime.GOOS == "windows" {
					t.Skip("symlink creation requires privileges on some Windows hosts")
				}
				outside := filepath.Join(t.TempDir(), "outside.txt")
				if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(exchange.OutputsHostPath(), "link.txt")); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: ErrArtifactNonRegular,
		},
		{
			name: "hardlink",
			prepare: func(t *testing.T, exchange *DelegationArtifactExchange) {
				outside := filepath.Join(t.TempDir(), "outside.txt")
				if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(outside, filepath.Join(exchange.OutputsHostPath(), "hard.txt")); err != nil {
					t.Skipf("hardlinks unavailable: %v", err)
				}
			},
			wantErr: ErrArtifactHardlink,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			exchange := newTestDelegationExchange(
				t,
				t.TempDir(),
				uuid.New(),
				uuid.New(),
				DelegationArtifactLimits{},
			)
			testCase.prepare(t, exchange)
			destinationPath := t.TempDir()
			_, err := exchange.Publish(
				context.Background(),
				openTestArtifactRoot(t, destinationPath),
				time.Now(),
			)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Publish() error = %v, want errors.Is(%v)", err, testCase.wantErr)
			}
			if _, statErr := os.Stat(filepath.Join(destinationPath, ".delegations", exchange.DelegationID().String())); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("final destination visible after rejection: %v", statErr)
			}
			retention, retained := exchange.FailureRetention()
			if !retained || !strings.HasPrefix(
				retention.PublicationTempPath,
				".delegations/.tmp-"+exchange.DelegationID().String()+"-",
			) {
				t.Fatalf("publication temp path not retained for janitor: %#v, %v", retention, retained)
			}
			parentEntries, readErr := os.ReadDir(filepath.Join(destinationPath, ".delegations"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, entry := range parentEntries {
				if !strings.HasPrefix(entry.Name(), ".tmp-") {
					t.Fatalf("unexpected publication entry remains: %s", entry.Name())
				}
			}
		})
	}
}

func TestDelegationArtifactPublishNoReplaceCollision(t *testing.T) {
	exchange := newTestDelegationExchange(
		t,
		t.TempDir(),
		uuid.New(),
		uuid.New(),
		DelegationArtifactLimits{},
	)
	if err := os.WriteFile(filepath.Join(exchange.OutputsHostPath(), "new.txt"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	destinationPath := t.TempDir()
	finalPath := filepath.Join(destinationPath, ".delegations", exchange.DelegationID().String())
	if err := os.MkdirAll(finalPath, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(finalPath, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := exchange.Publish(
		context.Background(),
		openTestArtifactRoot(t, destinationPath),
		time.Now(),
	)
	if !errors.Is(err, ErrArtifactPublishConflict) {
		t.Fatalf("Publish() error = %v, want conflict", err)
	}
	content, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(content) != "original" {
		t.Fatalf("pre-existing destination changed: %q, %v", content, readErr)
	}
}

func TestDelegationArtifactSecureRenameNeverReplaces(t *testing.T) {
	root := openTestArtifactRoot(t, t.TempDir())
	source, err := root.root.createSubroot("source", 0700)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.close(); err != nil {
		t.Fatal(err)
	}
	destination, err := root.root.createSubroot("destination", 0700)
	if err != nil {
		t.Fatal(err)
	}
	if err := destination.close(); err != nil {
		t.Fatal(err)
	}

	err = root.root.renameNoReplace("source", "destination")
	if !errors.Is(err, ErrArtifactPublishConflict) {
		t.Fatalf("renameNoReplace() error = %v, want conflict", err)
	}
	for _, name := range []string{"source", "destination"} {
		exists, statErr := root.root.exists(name)
		if statErr != nil || !exists {
			t.Fatalf("%s existence = %v, %v; rename replaced an entry", name, exists, statErr)
		}
	}
}

func TestDelegationArtifactPublishUsesCapturedDestinationRoot(t *testing.T) {
	exchange := newTestDelegationExchange(
		t,
		t.TempDir(),
		uuid.New(),
		uuid.New(),
		DelegationArtifactLimits{},
	)
	if err := os.WriteFile(filepath.Join(exchange.OutputsHostPath(), "result.txt"), []byte("result"), 0600); err != nil {
		t.Fatal(err)
	}

	parent := t.TempDir()
	originalPath := filepath.Join(parent, "workspace")
	movedPath := filepath.Join(parent, "workspace-captured")
	if err := os.Mkdir(originalPath, 0700); err != nil {
		t.Fatal(err)
	}
	destination := openTestArtifactRoot(t, originalPath)
	if err := os.Rename(originalPath, movedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(originalPath, 0700); err != nil {
		t.Fatal(err)
	}

	publication, err := exchange.Publish(context.Background(), destination, time.Now())
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(movedPath, filepath.FromSlash(publication.ManifestPath))); err != nil {
		t.Fatalf("artifact not published to captured root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(originalPath, filepath.FromSlash(publication.ManifestPath))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement path received publication: %v", err)
	}
}

func TestDelegationArtifactBudgetsAreSharedAcrossInputsAndOutputs(t *testing.T) {
	limits := DelegationArtifactLimits{
		MaxFileBytes:  5,
		MaxTotalBytes: 8,
		MaxFiles:      2,
	}
	sourcePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourcePath, "input.bin"), []byte("1234"), 0600); err != nil {
		t.Fatal(err)
	}
	exchange := newTestDelegationExchange(t, t.TempDir(), uuid.New(), uuid.New(), limits)
	if _, err := exchange.StageInputs(
		context.Background(),
		openTestArtifactRoot(t, sourcePath),
		[]string{"input.bin"},
	); err != nil {
		t.Fatalf("StageInputs() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(exchange.OutputsHostPath(), "output.bin"), []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	destinationPath := t.TempDir()
	_, err := exchange.Publish(
		context.Background(),
		openTestArtifactRoot(t, destinationPath),
		time.Now(),
	)
	if !errors.Is(err, ErrArtifactLimitExceeded) {
		t.Fatalf("Publish() error = %v, want shared total-byte limit", err)
	}
	if _, statErr := os.Stat(filepath.Join(destinationPath, ".delegations", exchange.DelegationID().String())); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("final destination visible after budget failure: %v", statErr)
	}
}

func TestDelegationArtifactInputFileLimitRejectsBeforeStaging(t *testing.T) {
	limits := DelegationArtifactLimits{
		MaxFileBytes:  8,
		MaxTotalBytes: 16,
		MaxFiles:      1,
	}
	sourcePath := t.TempDir()
	for _, name := range []string{"one.txt", "two.txt"} {
		if err := os.WriteFile(filepath.Join(sourcePath, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	exchange := newTestDelegationExchange(t, t.TempDir(), uuid.New(), uuid.New(), limits)
	_, err := exchange.StageInputs(
		context.Background(),
		openTestArtifactRoot(t, sourcePath),
		[]string{"one.txt", "two.txt"},
	)
	if !errors.Is(err, ErrArtifactLimitExceeded) {
		t.Fatalf("StageInputs() error = %v, want file limit", err)
	}
	entries, readErr := os.ReadDir(exchange.InputsMount().HostRoot)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("staging began before structured input count rejection: %v", entries)
	}
}

func TestDelegationArtifactPublishRejectsSymlinkPublicationParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on some Windows hosts")
	}
	exchange := newTestDelegationExchange(
		t,
		t.TempDir(),
		uuid.New(),
		uuid.New(),
		DelegationArtifactLimits{},
	)
	destinationPath := t.TempDir()
	outsidePath := t.TempDir()
	if err := os.Symlink(outsidePath, filepath.Join(destinationPath, ".delegations")); err != nil {
		t.Fatal(err)
	}
	_, err := exchange.Publish(
		context.Background(),
		openTestArtifactRoot(t, destinationPath),
		time.Now(),
	)
	if err == nil {
		t.Fatal("Publish() accepted symlink publication parent")
	}
	entries, readErr := os.ReadDir(outsidePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("publication escaped through symlink: %v", entries)
	}
}

func TestDelegationArtifactFailureRetentionCleanupHook(t *testing.T) {
	exchange := newTestDelegationExchange(
		t,
		t.TempDir(),
		uuid.New(),
		uuid.New(),
		DelegationArtifactLimits{},
	)
	failedAt := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	exchange.RetainFailure(failedAt, "validation_failed")
	exchange.RetainFailure(failedAt.Add(time.Hour), "must_not_extend_ttl")
	retention, ok := exchange.FailureRetention()
	if !ok || retention.FailedAt != failedAt || retention.ReasonCode != "validation_failed" {
		t.Fatalf("retention = %#v, %v", retention, ok)
	}
	if exchange.ReadyForCleanup(failedAt.Add(59 * time.Minute)) {
		t.Fatal("exchange ready for cleanup before TTL")
	}
	if !exchange.ReadyForCleanup(failedAt.Add(60 * time.Minute)) {
		t.Fatal("exchange not ready for cleanup at TTL")
	}
}

func newTestDelegationExchange(
	t *testing.T,
	tenantRoot string,
	tenantID uuid.UUID,
	delegationID uuid.UUID,
	limits DelegationArtifactLimits,
) *DelegationArtifactExchange {
	t.Helper()
	exchange, err := NewDelegationArtifactExchange(
		tenantRoot,
		tenantID,
		delegationID,
		limits,
		0,
	)
	if err != nil {
		t.Fatalf("NewDelegationArtifactExchange() error = %v", err)
	}
	t.Cleanup(func() {
		if err := exchange.Close(); err != nil {
			t.Errorf("close exchange: %v", err)
		}
	})
	return exchange
}

func openTestArtifactRoot(t *testing.T, hostPath string) *DelegationArtifactRoot {
	t.Helper()
	root, err := OpenDelegationArtifactRoot(hostPath)
	if err != nil {
		t.Fatalf("OpenDelegationArtifactRoot() error = %v", err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close root: %v", err)
		}
	})
	return root
}
