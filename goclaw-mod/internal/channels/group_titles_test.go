package channels

import (
	"context"
	"reflect"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

type groupTitleTestChannel struct {
	*mockChannel
	gotIDs []string
}

func (c *groupTitleTestChannel) ResolveGroupTitles(_ context.Context, ids []string) (map[string]string, error) {
	c.gotIDs = append([]string(nil), ids...)
	return map[string]string{
		"channel-1": "general",
		"channel-2": "ops",
	}, nil
}

func TestManagerResolveGroupTitlesUsesBatchProvider(t *testing.T) {
	t.Parallel()

	mgr := NewManager(bus.New())
	ch := &groupTitleTestChannel{mockChannel: newMockChannel("discord-main", TypeDiscord)}
	mgr.RegisterChannel("discord-main", ch)

	titles, err := mgr.ResolveGroupTitles(context.Background(), "discord-main", []string{"channel-1", "channel-2"})
	if err != nil {
		t.Fatalf("ResolveGroupTitles returned error: %v", err)
	}
	if !reflect.DeepEqual(ch.gotIDs, []string{"channel-1", "channel-2"}) {
		t.Fatalf("batch provider ids = %#v", ch.gotIDs)
	}
	if titles["channel-1"] != "general" || titles["channel-2"] != "ops" {
		t.Fatalf("unexpected titles: %#v", titles)
	}
}
