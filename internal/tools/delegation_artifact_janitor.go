package tools

import (
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type retainedDelegationArtifact struct {
	tenantWorkspace     string
	tenantID            uuid.UUID
	tenantSlug          string
	delegationID        uuid.UUID
	retainUntil         time.Time
	callerLocation      *delegationArtifactCallerLocation
	publicationTempPath string
	publicationDurable  bool
	onCleaned           func()
}

func (t *DelegateTool) resolveDelegationCallerLocation(
	job *delegateArtifactJob,
) *delegationArtifactCallerLocation {
	type candidate struct {
		name string
		root string
	}
	candidates := []candidate{{name: "workspace", root: job.tenantWorkspace}}
	if t.dataDir != "" {
		candidates = append(candidates, candidate{
			name: "data",
			root: config.TenantDataDir(t.dataDir, job.tenantID, job.tenantSlug),
		})
	}
	for _, candidate := range candidates {
		relativePath, ok := artifactRelativeToRoot(candidate.root, job.callerWorkspace)
		if !ok {
			continue
		}
		return &delegationArtifactCallerLocation{
			Base:         candidate.name,
			RelativePath: relativePath,
		}
	}
	return nil
}

func artifactRelativeToRoot(root, target string) (string, bool) {
	if root == "" || target == "" {
		return "", false
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		return "", false
	}
	relativePath, err := filepath.Rel(absoluteRoot, absoluteTarget)
	if err != nil || relativePath == ".." ||
		strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", false
	}
	relativePath = filepath.ToSlash(relativePath)
	if relativePath == "." {
		return relativePath, true
	}
	validated, err := validateArtifactRelativePath(relativePath)
	return validated, err == nil
}

func (t *DelegateTool) updateActiveDelegationLifecycle(
	exchange *DelegationArtifactExchange,
	job *delegateArtifactJob,
) error {
	state, err := readDelegationArtifactLifecycleState(exchange.root, job.delegationID)
	if err != nil {
		return err
	}
	if state.Status != artifactLifecycleStaging {
		return ErrArtifactState
	}
	state.TenantSlug = job.tenantSlug
	state.CallerLocation = job.callerLocation
	return exchange.persistLifecycleState(state)
}

func (t *DelegateTool) markDelegationRunning(
	exchange *DelegationArtifactExchange,
	job *delegateArtifactJob,
	now time.Time,
) error {
	state, err := readDelegationArtifactLifecycleState(exchange.root, job.delegationID)
	if err != nil {
		return err
	}
	if state.Status != artifactLifecycleStaging {
		return ErrArtifactState
	}
	state.TenantSlug = job.tenantSlug
	state.Status = artifactLifecycleRunning
	state.FailedAt = nil
	state.RetainUntil = now.UTC().Add(delegationArtifactFailureTTL)
	state.ReasonCode = "artifact_running"
	state.CallerLocation = job.callerLocation
	state.PublicationTempPath = ""
	return exchange.persistLifecycleState(state)
}

func (t *DelegateTool) markDelegationPublishing(
	exchange *DelegationArtifactExchange,
	job *delegateArtifactJob,
	tempPath string,
	now time.Time,
) error {
	if err := validateDelegationPublicationTempPath(job.delegationID, tempPath); err != nil {
		return err
	}
	state, err := readDelegationArtifactLifecycleState(exchange.root, job.delegationID)
	if err != nil {
		return err
	}
	if state.Status != artifactLifecycleRunning {
		return ErrArtifactState
	}
	state.TenantSlug = job.tenantSlug
	state.Status = artifactLifecyclePublishing
	state.FailedAt = nil
	state.RetainUntil = now.UTC().Add(delegationArtifactFailureTTL)
	state.ReasonCode = "artifact_publishing"
	state.CallerLocation = job.callerLocation
	state.PublicationTempPath = tempPath
	return exchange.persistLifecycleState(state)
}

func (t *DelegateTool) markDelegationPublished(
	exchange *DelegationArtifactExchange,
	job *delegateArtifactJob,
	now time.Time,
) error {
	state, err := readDelegationArtifactLifecycleState(exchange.root, job.delegationID)
	if err != nil {
		return err
	}
	if state.Status != artifactLifecyclePublishing {
		return ErrArtifactState
	}
	state.Status = artifactLifecyclePublished
	state.FailedAt = nil
	state.RetainUntil = now.UTC()
	state.ReasonCode = "artifact_published"
	return exchange.persistLifecycleState(state)
}

func (t *DelegateTool) registerRetainedDelegationExchange(
	exchange *DelegationArtifactExchange,
	job *delegateArtifactJob,
	status delegationArtifactLifecycleStatus,
) error {
	if exchange == nil || job == nil {
		return ErrArtifactState
	}

	retention, ok := exchange.FailureRetention()
	if !ok {
		return ErrArtifactState
	}
	state, err := readDelegationArtifactLifecycleState(exchange.root, job.delegationID)
	if err != nil {
		return err
	}
	failedAt := retention.FailedAt.UTC()
	state.TenantSlug = job.tenantSlug
	if status != artifactLifecycleCancelled {
		status = artifactLifecycleFailed
	}
	state.Status = status
	state.FailedAt = &failedAt
	state.RetainUntil = retention.RetainUntil.UTC()
	state.ReasonCode = retention.ReasonCode
	state.CallerLocation = job.callerLocation
	if err := exchange.persistLifecycleState(state); err != nil {
		return err
	}

	t.addRetainedDelegationArtifact(retainedDelegationArtifact{
		tenantWorkspace:     job.tenantWorkspace,
		tenantID:            job.tenantID,
		tenantSlug:          job.tenantSlug,
		delegationID:        job.delegationID,
		retainUntil:         retention.RetainUntil,
		callerLocation:      job.callerLocation,
		publicationTempPath: state.PublicationTempPath,
	})
	return nil
}

func (t *DelegateTool) registerPublishedDelegationCleanup(
	job *delegateArtifactJob,
	publicationTempPath string,
	onCleaned func(),
) {
	t.addRetainedDelegationArtifact(retainedDelegationArtifact{
		tenantWorkspace:     job.tenantWorkspace,
		tenantID:            job.tenantID,
		tenantSlug:          job.tenantSlug,
		delegationID:        job.delegationID,
		retainUntil:         time.Time{},
		callerLocation:      job.callerLocation,
		publicationTempPath: publicationTempPath,
		publicationDurable:  true,
		onCleaned:           onCleaned,
	})
}

func (t *DelegateTool) addRetainedDelegationArtifact(item retainedDelegationArtifact) {
	key := retainedDelegationArtifactKey(item.tenantWorkspace, item.delegationID)
	t.retainedMu.Lock()
	defer t.retainedMu.Unlock()
	if t.sweeperClosed {
		return
	}
	if existing, ok := t.retained[key]; ok && item.onCleaned == nil {
		item.onCleaned = existing.onCleaned
	}
	t.retained[key] = item
	if t.sweeperStarted {
		return
	}
	t.sweeperStarted = true
	go t.runDelegationArtifactSweeper()
}

func retainedDelegationArtifactKey(tenantWorkspace string, delegationID uuid.UUID) string {
	return tenantWorkspace + "\x00" + delegationID.String()
}

func (t *DelegateTool) runDelegationArtifactSweeper() {
	ticker := time.NewTicker(delegationArtifactSweepInterval)
	defer func() {
		ticker.Stop()
		close(t.sweeperDone)
	}()
	for {
		select {
		case now := <-ticker.C:
			t.recoverRetainedDelegationExchanges()
			t.sweepRetainedDelegationExchanges(now)
		case <-t.sweeperStop:
			return
		}
	}
}

func (t *DelegateTool) recoverRetainedDelegationExchanges() {
	for _, tenantWorkspace := range t.delegationTenantWorkspaces() {
		if err := t.recoverTenantDelegationExchanges(tenantWorkspace); err != nil {
			slog.Warn("delegate.artifact_recovery_scan_failed")
		}
	}
}

func (t *DelegateTool) delegationTenantWorkspaces() []string {
	if t.workspace == "" {
		return nil
	}
	roots := make([]string, 0, 8)
	if info, err := os.Lstat(t.workspace); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		roots = append(roots, t.workspace)
	}
	tenantParent := filepath.Join(t.workspace, "tenants")
	entries, err := os.ReadDir(tenantParent)
	if err != nil {
		return roots
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.IsDir() {
			continue
		}
		roots = append(roots, filepath.Join(tenantParent, entry.Name()))
	}
	sort.Strings(roots)
	return roots
}

func (t *DelegateTool) recoverTenantDelegationExchanges(tenantWorkspace string) error {
	tenantRoot, err := openArtifactSecureRoot(tenantWorkspace)
	if err != nil {
		return err
	}
	defer tenantRoot.close()
	delegationsRoot, err := tenantRoot.openSubroot("collaboration/delegations")
	if err != nil {
		if isArtifactNotExist(err) {
			return nil
		}
		return err
	}
	defer delegationsRoot.close()
	names, err := tenantRoot.readDir("collaboration/delegations")
	if err != nil {
		return err
	}
	sort.Strings(names)
	recoveredAt := time.Now().UTC()
	for _, name := range names {
		delegationID, err := uuid.Parse(name)
		if err != nil || delegationID == uuid.Nil || delegationID.String() != name {
			continue
		}
		exchangeRoot, err := delegationsRoot.openSubroot(name)
		if err != nil {
			continue
		}
		state, stateErr := readDelegationArtifactLifecycleState(exchangeRoot, delegationID)
		if stateErr != nil {
			_ = exchangeRoot.close()
			continue
		}
		tenantID, err := uuid.Parse(state.TenantID)
		if err != nil || tenantID == uuid.Nil {
			_ = exchangeRoot.close()
			continue
		}
		if !t.lifecycleStateMatchesTenantWorkspace(state, tenantWorkspace, tenantID) {
			_ = exchangeRoot.close()
			continue
		}
		retainUntil := state.RetainUntil
		switch state.Status {
		case artifactLifecycleStaging, artifactLifecycleRunning, artifactLifecyclePublishing:
			failedAt := recoveredAt
			state.Status = artifactLifecycleFailed
			state.FailedAt = &failedAt
			state.RetainUntil = recoveredAt.Add(delegationArtifactFailureTTL)
			state.ReasonCode = "artifact_recovered_stale"
			if err := persistDelegationArtifactLifecycleState(exchangeRoot, delegationID, state); err != nil {
				_ = exchangeRoot.close()
				continue
			}
			retainUntil = state.RetainUntil
		case artifactLifecyclePublished:
			retainUntil = time.Time{}
		}
		_ = exchangeRoot.close()
		t.addRetainedDelegationArtifact(retainedDelegationArtifact{
			tenantWorkspace:     tenantWorkspace,
			tenantID:            tenantID,
			tenantSlug:          state.TenantSlug,
			delegationID:        delegationID,
			retainUntil:         retainUntil,
			callerLocation:      state.CallerLocation,
			publicationTempPath: state.PublicationTempPath,
			publicationDurable:  state.Status == artifactLifecyclePublished,
		})
	}
	return nil
}

func (t *DelegateTool) lifecycleStateMatchesTenantWorkspace(
	state delegationArtifactLifecycleState,
	tenantWorkspace string,
	tenantID uuid.UUID,
) bool {
	if tenantID == store.MasterTenantID {
		expected, err := filepath.Abs(t.workspace)
		if err != nil {
			return false
		}
		actual, err := filepath.Abs(tenantWorkspace)
		return err == nil && actual == expected
	}
	if state.TenantSlug == "" {
		// A staging state can be created immediately before its tenant slug is
		// added. It has no caller publication path and can only delete its scanned
		// exchange directory.
		return state.Status == artifactLifecycleStaging &&
			state.CallerLocation == nil &&
			state.PublicationTempPath == ""
	}
	expected := config.TenantWorkspace(t.workspace, tenantID, state.TenantSlug)
	expectedAbs, err := filepath.Abs(expected)
	if err != nil {
		return false
	}
	actualAbs, err := filepath.Abs(tenantWorkspace)
	return err == nil && actualAbs == expectedAbs
}

func (t *DelegateTool) sweepRetainedDelegationExchanges(now time.Time) {
	due := make([]retainedDelegationArtifact, 0, delegationArtifactSweepBatch)
	t.retainedMu.Lock()
	for _, item := range t.retained {
		if !now.Before(item.retainUntil) {
			due = append(due, item)
			if len(due) == delegationArtifactSweepBatch {
				break
			}
		}
	}
	t.retainedMu.Unlock()

	for _, item := range due {
		if err := t.cleanupRetainedDelegationArtifact(item); err != nil {
			slog.Warn("delegate.artifact_retention_cleanup_failed")
			continue
		}
		t.retainedMu.Lock()
		delete(t.retained, retainedDelegationArtifactKey(item.tenantWorkspace, item.delegationID))
		t.retainedMu.Unlock()
		if item.onCleaned != nil {
			item.onCleaned()
		}
	}
}

func (t *DelegateTool) cleanupRetainedDelegationArtifact(item retainedDelegationArtifact) error {
	if item.publicationTempPath != "" && !item.publicationDurable {
		if item.callerLocation == nil {
			return ErrArtifactState
		}
		if err := validateDelegationPublicationTempPath(item.delegationID, item.publicationTempPath); err != nil {
			return err
		}
		callerRootPath, err := t.resolveRetainedCallerRoot(item)
		if err != nil {
			return err
		}
		callerRoot, err := openArtifactSecureRoot(callerRootPath)
		if err != nil {
			return err
		}
		err = callerRoot.removeTree(item.publicationTempPath)
		_ = callerRoot.close()
		if err != nil && !isArtifactNotExist(err) {
			return err
		}
	}
	return t.tryRemoveDelegationExchange(item.tenantWorkspace, item.delegationID)
}

func (t *DelegateTool) resolveRetainedCallerRoot(item retainedDelegationArtifact) (string, error) {
	if item.callerLocation == nil {
		return "", ErrArtifactState
	}
	var base string
	switch item.callerLocation.Base {
	case "workspace":
		base = item.tenantWorkspace
	case "data":
		if t.dataDir == "" {
			return "", ErrArtifactState
		}
		base = config.TenantDataDir(t.dataDir, item.tenantID, item.tenantSlug)
	default:
		return "", ErrArtifactState
	}
	relativePath := item.callerLocation.RelativePath
	if relativePath == "." {
		return base, nil
	}
	validated, err := validateArtifactRelativePath(relativePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, filepath.FromSlash(validated)), nil
}

func validateDelegationPublicationTempPath(delegationID uuid.UUID, tempPath string) error {
	validated, err := validateArtifactRelativePath(tempPath)
	if err != nil {
		return err
	}
	prefix := ".tmp-" + delegationID.String() + "-"
	if path.Dir(validated) != ".delegations" ||
		!strings.HasPrefix(path.Base(validated), prefix) {
		return ErrArtifactInvalidPath
	}
	suffix := strings.TrimPrefix(path.Base(validated), prefix)
	parsed, err := uuid.Parse(suffix)
	if err != nil || parsed == uuid.Nil || parsed.String() != suffix {
		return ErrArtifactInvalidPath
	}
	return nil
}

func (t *DelegateTool) removeDelegationExchange(
	tenantWorkspace string,
	delegationID uuid.UUID,
) error {
	if tenantWorkspace == "" || delegationID == uuid.Nil {
		return ErrArtifactState
	}
	tenantRoot, err := openArtifactSecureRoot(tenantWorkspace)
	if err != nil {
		return err
	}
	defer tenantRoot.close()
	relativePath := path.Join("collaboration", "delegations", delegationID.String())
	if err := tenantRoot.removeTree(relativePath); err != nil && !isArtifactNotExist(err) {
		return fmt.Errorf("remove delegation exchange: %w", err)
	}
	return nil
}

func (t *DelegateTool) tryRemoveDelegationExchange(
	tenantWorkspace string,
	delegationID uuid.UUID,
) error {
	if t.removeExchange != nil {
		return t.removeExchange(tenantWorkspace, delegationID)
	}
	return t.removeDelegationExchange(tenantWorkspace, delegationID)
}
