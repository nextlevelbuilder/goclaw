package tools

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A delegated input file is chmodded read-only after staging. Removing the
// exchange must still succeed on every platform; on Windows the read-only
// attribute blocks deletion unless it is cleared first.
func TestDelegationArtifactRemoveTreeDeletesReadOnlyFiles(t *testing.T) {
	base := t.TempDir()
	inputs := filepath.Join(base, "exchange", "inputs")
	if err := os.MkdirAll(inputs, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(inputs, "report.md")
	if err := os.WriteFile(file, []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}

	root, err := openArtifactSecureRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.close()
	if err := root.removeTree("exchange"); err != nil {
		t.Fatalf("removeTree with read-only file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "exchange")); !os.IsNotExist(err) {
		t.Fatalf("exchange still exists after removeTree: %v", err)
	}
}

// A failed exchange can carry a publication temp path without a recorded caller
// location. There is nothing to locate in the caller workspace, so retention
// cleanup must still remove the exchange instead of failing on every sweep.
func TestDelegationArtifactRetentionCleansExchangeWithoutCallerLocation(t *testing.T) {
	tenantWorkspace := t.TempDir()
	delegationID := uuid.New()
	exchange := filepath.Join(tenantWorkspace, "collaboration", "delegations", delegationID.String())
	if err := os.MkdirAll(filepath.Join(exchange, "inputs"), 0o755); err != nil {
		t.Fatal(err)
	}

	tool := &DelegateTool{}
	err := tool.cleanupRetainedDelegationArtifact(retainedDelegationArtifact{
		tenantWorkspace:     tenantWorkspace,
		delegationID:        delegationID,
		retainUntil:         time.Now().Add(-time.Minute),
		publicationTempPath: ".delegations/.tmp-" + delegationID.String() + "-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("cleanup without caller location: %v", err)
	}
	if _, err := os.Stat(exchange); !os.IsNotExist(err) {
		t.Fatalf("exchange still exists: %v", err)
	}
}
