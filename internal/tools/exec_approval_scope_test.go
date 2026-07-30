package tools

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Approval ids are short and sequential ("exec-1", "exec-2"), so an id is not a
// secret. These tests pin that an id alone cannot let one user see or answer
// another user's pending action — without them, any operator-scoped caller in any
// tenant could authorise code execution inside someone else's session.
func TestApprovalScoping(t *testing.T) {
	tenantA, tenantB := uuid.New(), uuid.New()

	newPending := func(m *ExecApprovalManager, tenant uuid.UUID, user string) string {
		idCh := make(chan string, 1)
		go func() {
			// Raise a request and let it time out; we only need it pending.
			_, _ = m.RequestApproval(ApprovalRequest{
				ToolName: "Bash", Detail: "secret-command", UserID: user, TenantID: tenant,
			}, 3*time.Second)
		}()
		// Wait for it to register.
		deadline := time.After(2 * time.Second)
		for {
			for _, pa := range m.ListPending() {
				if pa.UserID == user && pa.tenantID == tenant {
					idCh <- pa.ID
					return <-idCh
				}
			}
			select {
			case <-deadline:
				t.Fatalf("approval for %s never became pending", user)
			case <-time.After(5 * time.Millisecond):
			}
		}
	}

	t.Run("list hides other users and other tenants", func(t *testing.T) {
		m := NewExecApprovalManager(DefaultExecApprovalConfig())
		newPending(m, tenantA, "user-A")
		newPending(m, tenantB, "user-B")

		got := m.ListPendingFor(tenantA, "user-A")
		if len(got) != 1 || got[0].UserID != "user-A" {
			t.Fatalf("user-A should see exactly their own approval, got %d", len(got))
		}
		if seen := m.ListPendingFor(tenantA, "user-other"); len(seen) != 0 {
			t.Errorf("LEAK: a different user in the same tenant saw %d approval(s)", len(seen))
		}
		if seen := m.ListPendingFor(tenantB, "user-A"); len(seen) != 0 {
			// user-A's id in tenant B must not surface tenant A's row.
			t.Errorf("CROSS-TENANT LEAK: saw %d approval(s)", len(seen))
		}
	})

	t.Run("another user cannot resolve by id", func(t *testing.T) {
		m := NewExecApprovalManager(DefaultExecApprovalConfig())
		id := newPending(m, tenantA, "user-A")

		if err := m.ResolveFor(id, ApprovalAllowOnce, tenantA, "user-B"); err == nil {
			t.Fatal("ESCALATION: user-B approved user-A's pending action")
		}
		if err := m.ResolveFor(id, ApprovalAllowOnce, tenantB, "user-A"); err == nil {
			t.Fatal("CROSS-TENANT ESCALATION: resolved with the wrong tenant")
		}
		// The rightful owner still can.
		if err := m.ResolveFor(id, ApprovalAllowOnce, tenantA, "user-A"); err != nil {
			t.Fatalf("owner could not resolve their own approval: %v", err)
		}
	})

	t.Run("a non-visible id is indistinguishable from a missing one", func(t *testing.T) {
		m := NewExecApprovalManager(DefaultExecApprovalConfig())
		id := newPending(m, tenantA, "user-A")

		wrongUser := m.ResolveFor(id, ApprovalAllowOnce, tenantA, "user-B")
		missing := m.ResolveFor("exec-99999", ApprovalAllowOnce, tenantA, "user-B")
		if wrongUser == nil || missing == nil {
			t.Fatal("both should error")
		}
		// Compare SHAPE, not the literal text: each message quotes its own id, so
		// the strings differ by construction. What must not differ is the reason —
		// a distinct "exists but not yours" would let a caller enumerate ids.
		norm := func(err error, id string) string {
			return strings.ReplaceAll(err.Error(), strconv.Quote(id), "<id>")
		}
		if got, want := norm(wrongUser, id), norm(missing, "exec-99999"); got != want {
			t.Errorf("error reason differs, letting a caller probe which ids exist:\n  wrong user: %s\n  missing:    %s", got, want)
		}
	})

	t.Run("system approvals (no user) stay answerable in-tenant", func(t *testing.T) {
		m := NewExecApprovalManager(DefaultExecApprovalConfig())
		id := newPending(m, tenantA, "") // cron/system exec: no user in context

		if got := m.ListPendingFor(tenantA, "anyone"); len(got) != 1 {
			t.Fatalf("a system approval must remain visible in-tenant, got %d", len(got))
		}
		if err := m.ResolveFor(id, ApprovalAllowOnce, tenantA, "anyone"); err != nil {
			t.Errorf("a system approval should be answerable in-tenant: %v", err)
		}
		if seen := m.ListPendingFor(tenantB, "anyone"); len(seen) != 0 {
			t.Errorf("system approval leaked across tenants: %d", len(seen))
		}
	})
}
