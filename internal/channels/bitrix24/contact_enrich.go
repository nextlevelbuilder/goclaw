package bitrix24

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Contact-name enrichment via Bitrix24 REST `user.get`.
//
// Bitrix24 webhook events do NOT carry display_name / username — the
// EventParams struct only has FromUserID as an integer-shaped string. This
// file adds a lazy, best-effort enrichment path so the Contacts page can
// show real names instead of "—" placeholders.
//
// Design choices:
//   - Lazy + per-process cache: first sight of a user → 1 user.get RPC;
//     every subsequent message from the same user is a cache hit. Bitrix
//     profile data rarely changes, so a 1-hour TTL is plenty.
//   - Negative caching (5min) absorbs Open Channel customer IDs and any
//     other user.get failure mode. Without it, a webhook burst from an
//     untrackable sender would trigger N pointless RPC calls.
//   - Best-effort: any failure is logged at Debug and swallowed —
//     EnsureContact still runs with empty fields, which is the exact
//     pre-enrichment behavior. No regression path.
//   - Skip enrichment entirely when Client()/Portal are not yet ready
//     (before Start() completes) — otherwise a race on startup could call
//     Call() against a nil-bound client and panic.
//
// Scope requirement: the Bitrix app's OAuth scope must include `user` for
// user.get to succeed. If it doesn't, the RPC returns INSUFFICIENT_SCOPE
// and we fall back to empty names (and log the reason once for the
// operator). Document this in the UI's permissions note so admins know
// why names might stay blank.

// nameCacheTTL is how long a successful lookup is cached.
// Bitrix profile fields rarely change — 1h balances freshness vs RPC load.
const nameCacheTTL = 1 * time.Hour

// nameCacheNegativeTTL caches "this user can't be resolved" (user.get
// returned nothing, or failed with a 4xx). Shorter than the happy-path
// TTL because config mistakes (missing scope) are recoverable — the
// operator fixing the scope should see correct names within 5 minutes
// without needing a channel reload.
const nameCacheNegativeTTL = 5 * time.Minute

// userGetTimeout caps how long we'll wait for a user.get response on the
// hot path. Short on purpose — enrichment is nice-to-have, blocking the
// message pipeline behind a slow Bitrix portal is not.
const userGetTimeout = 3 * time.Second

// nameCacheEntry holds a resolved display name + login for a Bitrix user.
// `fetchedAt` is the wall-clock time of the fetch attempt (success or
// failure); TTL comparison uses it directly rather than a separate
// expiresAt field to keep entries compact.
type nameCacheEntry struct {
	name      string
	username  string
	fetchedAt time.Time
	// negative is true when the entry represents a failed / empty lookup.
	// Used to pick the right TTL on subsequent checks without re-fetching
	// the full profile.
	negative bool
}

// bitrixUserProfile is the subset of user.get fields we actually consume.
// Kept separate from the JSON decode struct so the caller doesn't see
// Bitrix's SHOUTY_CASE field names.
type bitrixUserProfile struct {
	ID         string
	Name       string
	LastName   string
	SecondName string
	Login      string
	Email      string
}

// userGetRaw mirrors the subset of Bitrix24's user.get response we decode.
// Bitrix returns a JSON array even for a single-ID lookup, so callers
// unmarshal into []userGetRaw.
type userGetRaw struct {
	ID         string `json:"ID"`
	Name       string `json:"NAME"`
	LastName   string `json:"LAST_NAME"`
	SecondName string `json:"SECOND_NAME"`
	Login      string `json:"LOGIN"`
	Email      string `json:"EMAIL"`
}

// resolveContactName returns display name + username for a Bitrix user,
// caching the result per-channel. Safe to call from the hot message
// handler — any failure path returns ("", "") so the caller can pass
// empty strings to EnsureContact without special-casing errors.
//
// Called from handleMessage BEFORE EnsureContact so the contact row is
// created with populated fields on first sight; subsequent messages read
// from cache and stay lock-free beyond the map access.
func (c *Channel) resolveContactName(ctx context.Context, userID string) (name, username string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", ""
	}

	// Cache check. Take the lock once, decide whether to use the cached
	// value, return early if so. Holding the lock across the RPC would
	// serialize all first-sight users behind one in-flight call; the map
	// write at the end of this function is the only other lock acquisition
	// and it doesn't dedupe concurrent lookups, which is a deliberate
	// trade-off — duplicate RPCs for the same new user are rare (Bitrix
	// typically fires events serially per chat) and cheaper than a
	// per-key mutex map.
	c.nameCacheMu.Lock()
	if entry, ok := c.nameCache[userID]; ok {
		ttl := nameCacheTTL
		if entry.negative {
			ttl = nameCacheNegativeTTL
		}
		if time.Since(entry.fetchedAt) < ttl {
			c.nameCacheMu.Unlock()
			return entry.name, entry.username
		}
	}
	c.nameCacheMu.Unlock()

	// Cache miss or stale. Gate on client availability — if the channel
	// is mid-Start() or mid-Stop(), the client/portal binding may not be
	// live yet, and Call() will error anyway. Skip the RPC rather than
	// log a spurious warning.
	client := c.Client()
	if client == nil {
		return "", ""
	}

	rpcCtx, cancel := context.WithTimeout(ctx, userGetTimeout)
	defer cancel()

	profile, err := fetchBitrixUser(rpcCtx, client, userID)
	if err != nil {
		// Debug level: this is best-effort. Operators investigating "why
		// are my contact names empty?" will search for this exact log
		// line; keep the wording stable so runbook examples hold up.
		slog.Debug("bitrix24: user.get enrichment failed",
			"channel", c.Name(), "user_id", userID, "err", err)
		c.putNameCache(userID, "", "", true)
		return "", ""
	}

	name = buildDisplayName(profile)
	username = strings.TrimSpace(profile.Login)

	// If both ended up empty (e.g. user.get returned a profile with only
	// an EMAIL set), cache as negative so we don't refetch for 5 min —
	// positive cache of "" is indistinguishable from "not cached yet"
	// for the TTL check, and we want the shorter TTL anyway.
	negative := name == "" && username == ""
	c.putNameCache(userID, name, username, negative)
	return name, username
}

// putNameCache writes the lookup result to the channel cache. Separated
// from resolveContactName so the happy path doesn't re-take the lock
// inline and stays readable at a glance.
func (c *Channel) putNameCache(userID, name, username string, negative bool) {
	c.nameCacheMu.Lock()
	defer c.nameCacheMu.Unlock()
	if c.nameCache == nil {
		c.nameCache = make(map[string]nameCacheEntry)
	}
	c.nameCache[userID] = nameCacheEntry{
		name:      name,
		username:  username,
		fetchedAt: time.Now(),
		negative:  negative,
	}
}

// fetchBitrixUser calls user.get?ID=<uid> and decodes the first (and
// typically only) entry in the result array. Empty result array is NOT
// an error — it means the user id isn't known to the portal (common for
// Open Channel customer IDs). Returns an empty profile in that case so
// the caller can negative-cache without branching on error type.
func fetchBitrixUser(ctx context.Context, client *Client, userID string) (*bitrixUserProfile, error) {
	res, err := client.Call(ctx, "user.get", map[string]any{"ID": userID})
	if err != nil {
		return nil, err
	}
	// Bitrix wraps single-ID lookups in a JSON array. Guard the decode
	// against an unexpected object shape just in case the portal returns
	// a single object (cheaper than maintaining two decode paths — if
	// either works, we accept it).
	var list []userGetRaw
	if len(res.Result) > 0 {
		if err := json.Unmarshal(res.Result, &list); err != nil {
			// Fall back to single-object decode. Some older Bitrix
			// deployments have been observed to return a bare object
			// rather than a length-1 array.
			var one userGetRaw
			if err2 := json.Unmarshal(res.Result, &one); err2 != nil {
				return nil, fmt.Errorf("decode user.get result: %w", err)
			}
			if one.ID != "" {
				list = []userGetRaw{one}
			}
		}
	}
	if len(list) == 0 {
		return &bitrixUserProfile{}, nil
	}
	u := list[0]
	return &bitrixUserProfile{
		ID:         u.ID,
		Name:       u.Name,
		LastName:   u.LastName,
		SecondName: u.SecondName,
		Login:      u.Login,
		Email:      u.Email,
	}, nil
}

// buildDisplayName assembles a user-visible name from the profile fields.
// Preference order:
//  1. "NAME LAST_NAME" (most common for real Bitrix users)
//  2. LAST_NAME alone (some portals fill only surname)
//  3. NAME alone
//  4. LOGIN (fallback — at least gives the operator something clickable)
//  5. EMAIL (absolute last resort)
//
// SECOND_NAME (patronymic, common in RU portals) is omitted from the
// display name on purpose — we want the Contacts column to stay
// readable in the common case where NAME + LAST_NAME is already the
// expected label.
func buildDisplayName(p *bitrixUserProfile) string {
	name := strings.TrimSpace(p.Name)
	last := strings.TrimSpace(p.LastName)
	switch {
	case name != "" && last != "":
		return name + " " + last
	case last != "":
		return last
	case name != "":
		return name
	}
	if login := strings.TrimSpace(p.Login); login != "" {
		return login
	}
	return strings.TrimSpace(p.Email)
}

// Compile-time nudge: the cache maps are guarded by nameCacheMu, and
// nameCacheMu is zero-initializable like any sync.Mutex. Explicit asserts
// kept minimal — the contract is "never touch nameCache without the mutex".
var _ sync.Mutex = sync.Mutex{}
