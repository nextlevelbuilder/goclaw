# Brainstorm: Discord Guild-User Memory Sharing

**Date:** 2026-03-12
**Branch:** feat/discord-guild-user-memory
**Status:** Đề xuất — chờ implement

---

## Vấn đề

GoClaw bot khi được add vào Discord server hiện tại có bộ nhớ tách biệt theo từng user trong cùng server: mỗi khi user chuyển sang channel khác, bot không nhớ tên, cách xưng hô, ngôn ngữ, preferences. User phải tự giới thiệu lại từ đầu.

**Discord khác các platform khác:**
- Telegram: 1 bot = nhiều DM chat riêng biệt, mỗi chat có identity độc lập → không bị vấn đề này
- Discord: 1 server → nhiều channels → cùng 1 danh sách members → user mong muốn bot nhớ họ xuyên suốt các channels

---

## Root Cause (Cập nhật sau phân tích sâu)

Ban đầu nghĩ vấn đề là per-channel scoping. Sau khi đọc `cmd/gateway_consumer.go:97-110`, phát hiện code hiện tại **đã** có Discord-specific guild_id handling:

```go
// Group-scoped UserID — For Discord: use guild_id so all channels
// in the same server share context files, memory, and seeding.
userID := msg.UserID
if peerKind == string(sessions.PeerGroup) && msg.ChatID != "" {
    groupID := msg.ChatID
    if guildID := msg.Metadata["guild_id"]; guildID != "" {
        groupID = guildID  // Discord: dùng guild_id thay vì channelID
    }
    userID = fmt.Sprintf("group:%s:%s", msg.Channel, groupID)
}
```

→ Discord groups hiện đang dùng `userID = "group:discord:{guildID}"` — context files đã được **share across channels trong cùng server**.

**Root cause thực sự:** `"group:discord:{guildID}"` là scope **shared bởi tất cả users** trong server. Không có per-individual separation. User A setup → User B ghi đè USER.md → mất thông tin cá nhân.

**Scope mapping hiện tại:**

| Platform | Group userID | Per-individual? |
|---|---|---|
| Telegram | `group:telegram:{chatID}` | ✗ shared bởi all users |
| Discord | `group:discord:{guildID}` | ✗ shared bởi all users |
| Zalo, Slack... | `group:{channel}:{chatID}` | ✗ shared bởi all users |

**Files liên quan:**
- `cmd/gateway_consumer.go:97-110` — nơi build userID cho group sessions (điểm thay đổi chính)
- `internal/tools/context_file_interceptor.go:250` — permission check dùng `"group:"` prefix
- `internal/store/context.go` — `UserIDKey`, `SenderIDKey` context keys

---

## Yêu cầu đã xác định

- **Core problem:** Phải tự giới thiệu lại, bot không nhớ tên/preferences khi sang channel khác trong cùng server
- **Memory scope:** Per-user trong cùng guild (Server A ≠ Server B, User A ≠ User B)
- **Conversation history:** Giữ riêng từng channel (không share)
- **Profile loading:** Active sender (mỗi message load profile của người đang gửi)
- **DM ↔ Group:** Share cùng profile (Phase 2)

---

## Giải pháp đề xuất

### Phase 1: Guild-User scoped userID (Core Fix)

**Thay đổi duy nhất:** `cmd/gateway_consumer.go` (~3 dòng), chỉ ảnh hưởng Discord:

```go
if peerKind == string(sessions.PeerGroup) && msg.ChatID != "" {
    if guildID := msg.Metadata["guild_id"]; guildID != "" && msg.SenderID != "" {
        // Discord: per-guild-user (cá nhân, không shared)
        userID = fmt.Sprintf("guild:%s:user:%s", guildID, msg.SenderID)
    } else {
        groupID := msg.ChatID
        userID = fmt.Sprintf("group:%s:%s", msg.Channel, groupID)
    }
}
```

Thêm update nhỏ trong `context_file_interceptor.go:250` để handle `"guild:"` prefix song song với `"group:"` trong permission check.

**Behavior mới:**

| Scenario | Session History | Profile (USER.md) |
|---|---|---|
| User A ở #general | `agent:X:discord:group:{channelA}` | `guild:G1:user:A` ← shared |
| User A ở #tech | `agent:X:discord:group:{channelB}` | `guild:G1:user:A` ← same ✓ |
| User B ở #general | `agent:X:discord:group:{channelA}` | `guild:G1:user:B` ← isolated ✓ |
| User A ở Server khác | `agent:X:discord:group:{channelX}` | `guild:G2:user:A` ← isolated ✓ |

**Zero new infrastructure** — tận dụng hoàn toàn `user_context_files` table và `ContextFileInterceptor` hiện có.

**Ảnh hưởng các channel khác: KHÔNG** — Telegram, Zalo, Feishu, Slack không có `guild_id` trong metadata → luồng else → giữ nguyên `"group:{channel}:{chatID}"`.

---

### Phase 2: Session Participants Index (Enhancement)

Mỗi message trong group channel chỉ inject USER.md của active sender. Để bot biết về **tất cả users đã chat** trong session, cần thêm:

- `participants []string` vào session metadata, cập nhật mỗi khi có message từ user mới
- Khi build system context: batch-load USER.mds của tất cả participants → inject dạng:

```
[Known participants in this channel]
- Alice (user:123): Người Việt, prefer tiếng Việt, senior developer
- Bob (user:456): English speaker, prefer concise answers
```

**Tại sao cần Phase 2 riêng:** `providers.Message` không có `SenderID` field → không thể extract users từ session history. Cần participants metadata riêng.

**Context overhead:** ~10 users × 300 tokens = 3000 tokens (negligible với 200k context window)

---

## Security Analysis

| Risk | Mitigation |
|---|---|
| User X đọc profile của User Y | Impossible: key include `senderID` |
| Server A leak sang Server B | `guild:{guildID}` prefix đảm bảo isolation |
| Unauthorized writes to SOUL.md | `protectedFileSet` check vẫn giữ — thêm `"guild:"` prefix vào điều kiện |
| Agent A leak sang Agent B | `agent_id` vẫn là primary key trong `user_context_files` |

---

## Unresolved Questions

1. **Backward compatibility:** Users hiện tại đã setup trong Discord groups → data cũ lưu theo `"group:discord:{guildID}"` (shared). Sau khi đổi sang per-user scope, data cũ sẽ bị "orphan". Cần migration script hoặc fallback lookup?

2. **DM ↔ Group bridging (Phase 2):** Discord DMs không có `guildID` → userID vẫn là `senderID` cho DMs. Hai scope khác nhau (DM vs group) nhưng user mong muốn nhất quán. Approach: lookup guild profile khi DM?

3. **Multi-agent Discord:** Cùng 1 Discord server có 2 agents → per-agent isolation via `agent_id` → user phải giới thiệu lại với mỗi agent. Acceptable?
