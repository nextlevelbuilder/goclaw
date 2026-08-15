package channels

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

type groupListTestChannel struct {
	*mockChannel
	groups []GroupInfo
}

func (c *groupListTestChannel) ListGroups(_ context.Context) ([]GroupInfo, error) {
	return c.groups, nil
}

func TestManagerListGroupsDelegatesToProvider(t *testing.T) {
	t.Parallel()

	mgr := NewManager(bus.New())
	ch := &groupListTestChannel{
		mockChannel: newMockChannel("bunny-zalo-personal", TypeZaloPersonal),
		groups: []GroupInfo{
			{GroupID: "747300108647389888", Name: "Ban Điều Hành", TotalMember: 8},
		},
	}
	mgr.RegisterChannel("bunny-zalo-personal", ch)

	groups, err := mgr.ListGroups(context.Background(), "bunny-zalo-personal")
	if err != nil {
		t.Fatalf("ListGroups returned error: %v", err)
	}
	if len(groups) != 1 || groups[0].Name != "Ban Điều Hành" || groups[0].GroupID != "747300108647389888" {
		t.Fatalf("unexpected groups: %#v", groups)
	}
}

func TestManagerListGroupsUnsupportedChannel(t *testing.T) {
	t.Parallel()

	mgr := NewManager(bus.New())
	ch := newMockChannel("telegram-main", TypeTelegram) // does not implement GroupListProvider
	mgr.RegisterChannel("telegram-main", ch)

	if _, err := mgr.ListGroups(context.Background(), "telegram-main"); err == nil {
		t.Fatal("expected error for channel without GroupListProvider support")
	}
}

func TestManagerListGroupsUnknownChannel(t *testing.T) {
	t.Parallel()

	mgr := NewManager(bus.New())
	if _, err := mgr.ListGroups(context.Background(), "does-not-exist"); err == nil {
		t.Fatal("expected error for unknown channel")
	}
}
