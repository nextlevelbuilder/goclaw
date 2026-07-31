package mcp

import (
	"slices"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// The prefix rule decides which tools become addressable as a toolkit, so it is
// pinned tightly: too loose and every lower_snake_case MCP server sprouts junk
// groups, too tight and a real toolkit silently cannot be granted.
func TestToolkitPrefix(t *testing.T) {
	cases := []struct {
		name string
		want string
		ok   bool
	}{
		// Composio's shape: TOOLKIT_ACTION.
		{"GOOGLESLIDES_CREATE_SLIDES_MARKDOWN", "googleslides", true},
		{"GMAIL_SEND_EMAIL", "gmail", true},
		{"TWITTER_CREATION_OF_A_POST", "twitter", true},
		{"OUTLOOK_SEND_EMAIL", "outlook", true},
		// Digits are legitimate inside a toolkit action name.
		{"GOOGLEDOCS_CREATE_DOCUMENT2", "googledocs", true},

		// NOT toolkit-shaped: anything that is not SHOUTING_SNAKE is left alone so
		// a non-Composio bridge does not get a group per first word.
		{"get_weather", "", false},
		{"getWeather", "", false},
		{"Some_Tool", "", false},
		{"lowercase_prefix_UPPER", "", false},
		// Degenerate underscore positions.
		{"NOUNDERSCORE", "", false},
		{"_LEADING", "", false},
		{"TRAILING_", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := toolkitPrefix(c.name)
		if ok != c.ok || got != c.want {
			t.Errorf("toolkitPrefix(%q) = (%q, %v), want (%q, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

// A grant is written as `group:mcp:<server>:<toolkit>`, so the group must contain
// exactly that toolkit's tools — no more (or a grant leaks capability) and no
// fewer (or it silently under-grants).
func TestRegisterToolkitGroupsPartitionsByToolkit(t *testing.T) {
	reg := tools.NewRegistry()
	m := &Manager{registry: reg}

	names := []string{
		"GOOGLESLIDES_CREATE_PRESENTATION",
		"GOOGLESLIDES_PRESENTATIONS_GET",
		"GMAIL_SEND_EMAIL",
		"get_weather", // not toolkit-shaped: must not create a group
	}
	m.registerToolkitGroups("composio", names)

	slides := reg.ExpandToolGroups(names, []string{"group:mcp:composio:googleslides"})
	slices.Sort(slides)
	want := []string{"GOOGLESLIDES_CREATE_PRESENTATION", "GOOGLESLIDES_PRESENTATIONS_GET"}
	if !slices.Equal(slides, want) {
		t.Errorf("slides group = %v, want %v", slides, want)
	}

	mail := reg.ExpandToolGroups(names, []string{"group:mcp:composio:gmail"})
	if !slices.Equal(mail, []string{"GMAIL_SEND_EMAIL"}) {
		t.Errorf("gmail group = %v, want just GMAIL_SEND_EMAIL", mail)
	}

	// The non-toolkit tool must not have produced a group.
	if got := reg.ExpandToolGroups(names, []string{"group:mcp:composio:get"}); len(got) != 0 {
		t.Errorf("a lower_snake_case tool created group 'get' = %v", got)
	}

	// A toolkit that does not exist must resolve to nothing rather than everything
	// — an unknown group silently meaning "all tools" would be a privilege leak.
	if got := reg.ExpandToolGroups(names, []string{"group:mcp:composio:notion"}); len(got) != 0 {
		t.Errorf("unknown toolkit resolved to %v, want empty", got)
	}
}

// Disconnecting a bridge must remove its toolkit groups, or a policy entry keeps
// resolving against tools that are no longer registered.
func TestUnregisterToolkitGroups(t *testing.T) {
	reg := tools.NewRegistry()
	m := &Manager{registry: reg}

	names := []string{"GMAIL_SEND_EMAIL", "GOOGLESLIDES_PRESENTATIONS_GET"}
	m.registerToolkitGroups("composio", names)
	if got := reg.ExpandToolGroups(names, []string{"group:mcp:composio:gmail"}); len(got) != 1 {
		t.Fatalf("precondition failed: gmail group = %v", got)
	}

	m.unregisterToolkitGroups("composio", names)
	for _, g := range []string{"group:mcp:composio:gmail", "group:mcp:composio:googleslides"} {
		if got := reg.ExpandToolGroups(names, []string{g}); len(got) != 0 {
			t.Errorf("%s still resolves to %v after unregister", g, got)
		}
	}
}

// Two bridges may expose the same toolkit name; the group key includes the server
// so one cannot shadow the other.
func TestToolkitGroupsAreScopedPerServer(t *testing.T) {
	reg := tools.NewRegistry()
	m := &Manager{registry: reg}

	a := []string{"GMAIL_SEND_EMAIL"}
	b := []string{"GMAIL_LIST_MESSAGES"}
	m.registerToolkitGroups("composio", a)
	m.registerToolkitGroups("other", b)

	all := append(append([]string{}, a...), b...)
	if got := reg.ExpandToolGroups(all, []string{"group:mcp:composio:gmail"}); !slices.Equal(got, a) {
		t.Errorf("composio gmail = %v, want %v", got, a)
	}
	if got := reg.ExpandToolGroups(all, []string{"group:mcp:other:gmail"}); !slices.Equal(got, b) {
		t.Errorf("other gmail = %v, want %v", got, b)
	}
}
