package gateway

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func TestBrowserTakeoverEvents_OnlyTheOwningUserReceivesThem(t *testing.T) {
	payload := map[string]any{"id": "x", "userId": "alice"}
	for _, name := range []string{protocol.EventBrowserTakeoverRequested, protocol.EventBrowserTakeoverResolved} {
		evt := makeEvent(name, masterTenant, payload)
		cases := []struct {
			who  string
			c    *Client
			want bool
		}{
			{"owning user", makeClient(permissions.RoleOperator, "alice", masterTenant), true},
			{"other user", makeClient(permissions.RoleOperator, "bob", masterTenant), false},
			{"tenant admin of another user", makeClient(permissions.RoleAdmin, "carol", masterTenant), false},
			{"owner role of another user", makeClient(permissions.RoleOwner, "root", masterTenant), false},
			{"same user id in another tenant", makeClient(permissions.RoleOperator, "alice", otherTenant), false},
		}
		for _, tc := range cases {
			if got := clientCanReceiveEvent(tc.c, evt); got != tc.want {
				t.Errorf("%s / %s: got %v want %v", name, tc.who, got, tc.want)
			}
		}
	}
}

func TestBrowserTakeoverEvents_MissingOwnerIsDeliveredToNobody(t *testing.T) {
	evt := makeEvent(protocol.EventBrowserTakeoverRequested, masterTenant, map[string]any{"id": "x"})
	for _, role := range []permissions.Role{permissions.RoleViewer, permissions.RoleAdmin, permissions.RoleOwner} {
		if clientCanReceiveEvent(makeClient(role, "alice", masterTenant), evt) {
			t.Errorf("role %s received an ownerless take over event", role)
		}
	}
}
