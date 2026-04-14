# 2026-04-12 Pancake Comment Auto-Reply + First Inbox

## Summary

Extended Pancake (pages.fm) channel to handle comment-on-post conversations and first-inbox auto-replies in parallel with existing inbox flows. Implemented 5 phases: types/API client, post fetcher with singleflight, comment filtering/routing, webhook router split, and send path refactor. All flows remain isolated; zero impact on inbox stability.

## Key Changes

- **Types:** Added `Features.FirstInbox`, `CommentReplyOptions` (filter/keywords/includePostContext), `PostID` to conversations, `PancakePost` type
- **API Client:** New `ReplyComment()`, `PrivateReply()`, `GetPosts()` methods with action-based routing
- **Post Fetcher:** New `post_fetcher.go` with `sync.Map` cache + `singleflight` dedup (key: `"posts"` because Pancake requires list-all, not per-post lookup)
- **Comment Handler:** New `comment_handler.go` with feature gate, self-reply prevention, staff skip, dedup by `comment:{msgID}`, keyword/all filter, echo dedup, post context enrichment
- **Webhook Router:** `webhook_handler.go` split `if convType != "INBOX"` into `switch` cases for INBOX vs COMMENT with PostID normalization
- **Send Path:** `pancake.go` refactored `Send()` into `sendInboxReply()` / `sendCommentReply()` based on `metadata["pancake_mode"]`; added `sendFirstInbox()` with `sync.Map` dedup

## Design Decisions

- **ChatID for comments = ConversationID:** Pancake groups comment conversations per sender per post internally (unlike Facebook where we constructed `postID:senderID` ourselves)
- **sendFirstInbox error handling:** Delete `firstInboxSent` entry on error to allow retry, not the stored time. Allows operator to re-trigger without waiting for TTL
- **Singleflight key scope:** All posts fetched with key `"posts"` (not per-postID) because Pancake API requires `GetPosts()` list-all, not per-post lookup. Prevents API stampede on concurrent comment arrivals for different posts
- **Additive architecture:** INBOX flow completely unchanged. Comment flow runs parallel. Feature gate ensures zero blast radius

## Test Coverage

- 72 tests pass with `-race` (integration + unit)
- Both `go build ./...` (PG) and `go build -tags sqliteonly ./...` (SQLite) clean
- `go vet ./...` clean
- Post fetcher, comment handler, send path all tested with dedup/concurrency/negative cache scenarios

## Follow-up Fixes

Code review fixes applied (PR #841 feedback from @mrgoonie):
- Added `slog.Debug` on post context fetch failure in `buildCommentContent()`
- Documented `GetPosts()` bypass of `doRequest()` (response body parsing requirement)
- Added 72h TTL eviction in `runDedupCleaner()` for `firstInboxSent` sync.Map (prevents unbounded growth)
- Wrapped `sendCommentReply()` with `context.WithTimeout(30s)` (bounds API hang risk)
- Replaced custom `containsStr` test helper with `strings.Contains`

All non-blocking, no architecture changes.
