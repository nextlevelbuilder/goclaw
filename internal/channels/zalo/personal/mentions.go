package personal

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

const defaultGroupMemberCacheTTL = 10 * time.Minute

type groupMembersEntry struct {
	names   map[string]string // display name -> uid
	expires time.Time
}

type groupMemberCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]groupMembersEntry
}

func newGroupMemberCache(ttl time.Duration, now func() time.Time) *groupMemberCache {
	if ttl <= 0 {
		ttl = defaultGroupMemberCacheTTL
	}
	if now == nil {
		now = time.Now
	}
	return &groupMemberCache{ttl: ttl, now: now, entries: make(map[string]groupMembersEntry)}
}

func (g *groupMemberCache) get(groupID string) (map[string]string, bool) {
	if g == nil {
		return nil, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	entry, ok := g.entries[groupID]
	if !ok || !entry.expires.After(g.now()) {
		delete(g.entries, groupID)
		return nil, false
	}
	return entry.names, true
}

func (g *groupMemberCache) set(groupID string, names map[string]string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.entries[groupID] = groupMembersEntry{names: names, expires: g.now().Add(g.ttl)}
}

// resolveMentions maps @DisplayName occurrences to Zalo mentionInfo ranges.
// The text is the final plain text (after style conversion), and offsets are
// UTF-16 code units. Failures are non-fatal: the message still sends without
// mentions rather than blocking on a profile lookup.
func (c *Channel) resolveMentions(ctx context.Context, groupID, text string) []protocol.MentionInfo {
	if c.groupCache == nil || !strings.Contains(text, "@") {
		return nil
	}
	names, ok := c.groupCache.get(groupID)
	if !ok {
		var err error
		names, err = c.fetchGroupMemberNames(ctx, groupID)
		if err != nil || len(names) == 0 {
			c.groupCache.set(groupID, nil)
			return nil
		}
		c.groupCache.set(groupID, names)
	}

	type candidate struct {
		name string
		uid  string
	}
	candidates := make([]candidate, 0, len(names))
	for name, uid := range names {
		if name == "" || uid == "" {
			continue
		}
		candidates = append(candidates, candidate{name: name, uid: uid})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if len(candidates[i].name) != len(candidates[j].name) {
			return len(candidates[i].name) > len(candidates[j].name)
		}
		return candidates[i].name < candidates[j].name
	})

	var mentions []protocol.MentionInfo
	for i := 0; i < len(text); i++ {
		if text[i] != '@' {
			continue
		}
		rest := text[i+1:]
		if hasMentionBoundary(rest, 3) && strings.HasPrefix(rest, "all") {
			mentions = append(mentions, protocol.MentionInfo{
				Pos:  utf16Len(text[:i]),
				Len:  4,
				UID:  protocol.MentionAllUID,
				Type: int(protocol.MentionAll),
			})
			i += 3
			continue
		}
		for _, cand := range candidates {
			if !strings.HasPrefix(rest, cand.name) {
				continue
			}
			end := len(cand.name)
			if !hasMentionBoundary(rest, end) {
				continue
			}
			mentions = append(mentions, protocol.MentionInfo{
				Pos:  utf16Len(text[:i]),
				Len:  utf16Len("@" + cand.name),
				UID:  cand.uid,
				Type: int(protocol.MentionEach),
			})
			i += end
			break
		}
	}
	return mentions
}

func hasMentionBoundary(s string, n int) bool {
	if n < 0 || n > len(s) {
		return false
	}
	if n == len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[n:])
	return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_'
}

func (c *Channel) fetchGroupMemberNames(ctx context.Context, groupID string) (map[string]string, error) {
	sess := c.session()
	if sess == nil {
		return nil, nil
	}
	info, err := protocol.GetGroupInfo(ctx, sess, groupID)
	if err != nil {
		return nil, err
	}
	if len(info.MemberIDs) == 0 {
		return nil, nil
	}
	members, err := protocol.GetGroupMembersInfo(ctx, sess, info.MemberIDs)
	if err != nil {
		return nil, err
	}

	// A display name that maps to multiple UIDs is ambiguous; mentioning it
	// would ping the wrong person, so drop it from the lookup.
	names := make(map[string]string, len(members))
	duplicates := make(map[string]bool)
	for uid, member := range members {
		for _, raw := range []string{member.DisplayName, member.ZaloName} {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			if existing, ok := names[name]; ok && existing != uid {
				duplicates[name] = true
				continue
			}
			names[name] = uid
		}
	}
	for name := range duplicates {
		delete(names, name)
	}
	return names, nil
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}
