package permissions

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// A CLI connection is a tenant-wide row: it defines what every member of the
// workspace may delegate to, and deleting it cascades to every member's stored
// credential. Mutating it used to require only OPERATOR — the same level as sending
// a message — while creating an agent already required admin.
//
// The split these tests pin is deliberate and easy to get wrong in either
// direction: too strict and a member cannot remove their own API key; too loose and
// any seat can delete the workspace's shared tool.
func TestConnectionMutationsRequireAdmin(t *testing.T) {
	for _, method := range []string{
		protocol.MethodConnectionsCreate,
		protocol.MethodConnectionsUpdate,
		protocol.MethodConnectionsDelete,
	} {
		if got := MethodRole(method); got != RoleAdmin {
			t.Errorf("%s requires %q, want %q — it changes what the whole workspace can delegate to",
				method, got, RoleAdmin)
		}
	}
}

func TestConnectionCredentialsStayOperator(t *testing.T) {
	// A credential row is keyed (connection_id, user_id): it is the caller's own
	// secret. Requiring an admin to set or remove your own key would be absurd.
	for _, method := range []string{
		protocol.MethodConnectionsCredentialSet,
		protocol.MethodConnectionsCredentialDelete,
		// Opening a CLI chat spends only the caller's own credential.
		protocol.MethodConnectionsChatOpen,
	} {
		if got := MethodRole(method); got != RoleOperator {
			t.Errorf("%s requires %q, want %q — it only involves the caller's own credential",
				method, got, RoleOperator)
		}
	}
}

func TestConnectionListStaysReadable(t *testing.T) {
	// Every member needs to SEE what they can delegate to; the board draws a card
	// for it. Gating the read behind a write role would blank the canvas for
	// viewers.
	if got := MethodRole(protocol.MethodConnectionsList); got != RoleViewer {
		t.Errorf("connections.list requires %q, want %q", got, RoleViewer)
	}
}
