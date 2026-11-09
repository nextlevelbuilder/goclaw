# Zalo Personal (zca) — Kế hoạch nâng cấp v2

> Trạng thái: **DRAFT — đã đối chiếu source zca-js v2.2.0, chờ review**
> Phạm vi chính: `internal/channels/zalo/personal/**` (không đụng Zalo OA `internal/channels/zalo/*.go` trừ `format.go` dùng chung)
> Tham chiếu docs: [zca-js docs](https://tdung.gitbook.io/zca-js) / [zca-js.tdung.com](https://zca-js.tdung.com)
> Tham chiếu source (bắt buộc): [`RFS-ADRENO/zca-js`](https://github.com/RFS-ADRENO/zca-js) — bản `2.2.0`, đúng bản docs đang mô tả
> Ngày soạn: 2026-11-09 · Ngày cập nhật source-verified: 2026-11-09

> **Vì sao phải đọc source, không chỉ docs:** docs (gitbook) mô tả API surface nhưng bỏ qua wire-level. Ví dụ docs ghi `styles` / `mentions`, còn source gửi `textProperties` / `mentionInfo`; docs không nói reaction là cmd `612`, undo nằm trong `content.deleteMsg` của `501/551/521`, và `601` chỉ có `file_done` / `group` / `fr`. Mọi mục dưới đây đã được sửa theo source; xem **Phụ lục A** để có file đối chiếu.

---

## 1. Mục tiêu

1. **Đúng/sai trước, feature sau.** Hiện có những lỗi im lặng khiến agent trả lời sai hoặc trả lời hai lần (replay sau reconnect) — xử lý trước.
2. **Giảm rủi ro khoá tài khoản.** Channel gọi API không chính thức, hiện không có rate limit / không phân loại lỗi / không persist cookie xoay vòng.
3. **Nâng chất lượng hội thoại.** Tận dụng `styles`, `quote`, `mentions`, `reaction` — những thứ Zalo Personal thực sự hỗ trợ nhưng channel đang bỏ.
4. **Mở khoá phần còn lại của giao thức** mà không phải build lại Go mỗi lần thêm một endpoint.

Không nằm trong mục tiêu: đổi kiến trúc channel layer, đổi Zalo OA, thêm zBusiness/catalog/auto-reply.

---

## 2. Hiện trạng (đã kiểm chứng trong code + source zca-js)

### 2.1 Bản đồ file

| File | Vai trò |
|---|---|
| `personal/channel.go` | lifecycle, `ListGroups`, warm approved-groups cache |
| `personal/auth.go` | credentials: DB preload → file → QR |
| `personal/listen.go` | listen loop, restart backoff |
| `personal/handlers.go` | `handleDM`, `handleGroupMessage`, mention gating, typing |
| `personal/send.go` | `Send()` outbound: text chunk, image, file |
| `personal/content-media.go` | `extractContentAndMedia`, `downloadFile`, quote media |
| `personal/reply-context.go` | `replyCache` (chỉ lưu ID/quote đã chuẩn hoá — **không** lưu raw `TMessage`) |
| `personal/factory.go` | DB instance: `zaloInstanceConfig` + `preloadedCreds` |
| `protocol/listener.go` | WS dial, frame dispatch, ping, read deadline |
| `protocol/listener_handlers.go` | decrypt + parse `1_501_0` / `1_521_0` / `1_601_0` |
| `protocol/send.go` | `SendMessage`, `SendTypingEvent`, `getServiceURL` |
| `protocol/send_image.go`, `send_file.go` | upload 1 chunk + send |
| `protocol/message_content.go` | `Content` (string \| object), `Attachment` |

### 2.2 Năng lực hiện có

| Lớp | Đang có |
|---|---|
| Auth | Cookie (DB/file) + QR qua gateway method; credentials lưu JSON `0600` |
| Inbound | Chỉ dispatch `1_501_0` (DM), `1_521_0` (group), `1_601_0` (control); control **chỉ** xử lý `act_type=file_done` |
| Content | text hoặc **1 attachment**; tải file: SSRF check, timeout 30s, cap 20MB cứng, sniff 512B để sửa extension |
| Outbound | text (chunk 2000 sau `StripMarkdown`), ảnh, file, typing (keepalive 4s, max 60s) |
| Resilience | ping loop, read-deadline phát hiện silent disconnect, code 3000 chờ 60s, backoff 10 lần rồi bỏ |

### 2.3 Khoảng trống đã xác nhận

| # | Vấn đề | Bằng chứng |
|---|---|---|
| G1 | Không dedupe `msgId` → reconnect replay = agent trả lời 2 lần | `handleUserMessages`/`handleGroupMessages` emit mọi msg |
| G2 | Không rate limit, không jitter, không cap/ngày | không có limiter trong `personal/` |
| G3 | Control event khác `file_done` bị bỏ | `handleControlEvents` bỏ qua `act_type != file_done`. **Lưu ý source:** reaction là cmd `612`; undo là `content.deleteMsg` trong `501/551/521`; `601` chỉ có `file_done` / `group` / `fr` |
| G4 | Mọi non-zero `error_code` là fail như nhau | `SendMessage` trả `error code %d` duy nhất; chưa check `error_code` lớp trong `data` đã decrypt |
| G5 | Cookie xoay vòng trong phiên không được persist lại | `saveCredentials` chỉ gọi sau QR login; instance DB không có credential writer |
| G6 | `downloadFile` dùng `http.Client` trần: không cookie, không UA, không Referer | `content-media.go` |
| G7 | `Attachment.URL()` ưu tiên `href` → ảnh/video/link có thể tải nhầm HTML | `message_content.go`; chưa có task riêng trong plan cũ |
| G8 | `msgType` không được dùng; sticker/link/location/poll/card mất trắng | `MsgType` có trên `TMessage` nhưng channel không đọc |
| G9 | `maxMediaBytes = 20MB` hard-code, không theo `media_max_mb` | `content-media.go`; `ZaloPersonalConfig` **chưa có** `MediaMaxMB` |
| G10 | Outbound: không `styles`, không `quote`, không `mentions`, không `reaction`, không `undo` | `protocol.SendMessage` chỉ nhận text |
| G11 | Hết 10 lần restart là chết im lặng | `restartWithBackoff` |
| G12 | Temp file media inbound không dọn | `downloadFile` để lại temp khi thành công; `persistMedia` copy nhưng không xoá source; `claimTempMedia` chỉ áp dụng outbound |
| G13 | Listener thiếu frame so với source | Go chỉ có `1_501_0`/`1_521_0`/`1_601_0`; source còn `551`, `612`, `610/611`, `502`, `602`, `510/511` |
| G14 | Thiếu surface config | Field mới phải có ở `config.ZaloPersonalConfig`, `zaloInstanceConfig` (DB), `ui/web/.../channel-schemas.ts`, docs |
| G15 | Quote không thể dựng lại từ cache hiện tại | `replycontext.Message` chỉ có IDs/ParentIDs/ParentQuote/Sender/Body; `TMessage` thiếu `propertyExt` + `ttl` |
| G16 | Listener không có kênh event cho reaction/undo/group event | `Listener` chỉ có `messageCh`/`disconnectedCh`/`closedCh`/`errorCh` |

**Ghi chú quan trọng:** zca-js **không có API tải media** (đã kiểm tra toàn bộ `llms.txt` + source). Inbound media chỉ là URL thô trong `content`. Việc tải là của app — channel hiện tại đã làm tốt hơn zca-js ở khoản này (`fixDownloadedExt`, quote media, SSRF). Các task dưới đây chỉ vá phần thiếu, không port từ JS.

---

## 3. Nguyên tắc thực thi

- **Backward compatible:** mỗi thay đổi outbound nằm sau cờ config, **default giữ hành vi cũ** (`rich_text=false`, `quote_reply=false`, `mentions_enabled=false`). Riêng dedupe/rate-limit bật mặc định (an toàn hơn). Nếu muốn đổi default sang bật, phải ghi rõ là breaking trong PR.
- **Không đổi signature public** của `channels.Channel` interface; mọi thứ mới là method nội bộ hoặc config field có default.
- **Config surface parity:** mọi field mới phải thêm đồng thời:
  1. `internal/config/config_channels.go` → `ZaloPersonalConfig`
  2. `internal/channels/zalo/personal/factory.go` → `zaloInstanceConfig` (instance DB/Web UI)
  3. `ui/web/src/pages/channels/channel-schemas.ts` → mục `zalo_personal`
  4. docs/config mẫu
- **Source pin:** mọi hành vi wire-level bám `zca-js` v2.2.0. Khi source đổi version, chạy lại Phụ lục A trước khi sửa.
- **Test trước khi merge:** unit test cho mọi parser mới + `tests/zalo_e2e` cho luồng gửi/nhận thật.
- **Không log secrets:** cookie/IMEI/secretKey tuyệt đối không vào log, kể cả `%v` của struct.
- **Một PR = một nhóm task**, không gộp P0 với P1.

---

## 4. P0 — Đúng/sai và an toàn tài khoản

Mục tiêu: hết trả lời trùng, hết gửi rác khi bị chặn, không chết im lặng. Ước lượng lại: **~7–8 ngày** (tăng do P0.3/P0.4 cần thiết kế credential writer + single-flight).

### P0.1 — Dedupe inbound theo `msgId`

**Vấn đề:** Zalo replay tin gần nhất khi WS reconnect. Không có dedupe → agent trả lời hai lần cho cùng một tin.

**Thay đổi:**
- Thêm `internal/channels/zalo/personal/dedupe.go`: LRU 2048 entry, TTL 5 phút, key `msgId` (fallback `cliMsgId` nếu rỗng).
- Gọi ở `handleMessage` **sau** `msg.IsSelf()` và **trước** `handleDM`/`handleGroupMessage`; không chặn ở protocol layer để giữ listener thuần.

```go
type msgDedupe struct {
    mu   sync.Mutex
    seen map[string]time.Time
    ttl  time.Duration
    max  int
}

func (d *msgDedupe) SeenOrMark(id string, now time.Time) bool
```

**Acceptance:**
- Test đơn vị: mark → seen; hết TTL → không seen; vượt `max` → evict entry cũ nhất.
- Test tích hợp: emit cùng `msgId` hai lần → chỉ 1 lần `HandleMessage` được gọi (dùng fake bus).
- Ghi chú: TTL 5 phút là giả định; nếu replay sau offline dài vượt TTL, cân nhắc tăng hoặc persist last-N msgId. Không chặn ở protocol layer.

---

### P0.2 — Rate limiter outbound

**Vấn đề:** `sendChunkedText` bắn N chunk liên tiếp không delay; không có giới hạn global. Đây là đường ngắn nhất tới checkpoint của Zalo.

**Thay đổi:**
- `internal/channels/zalo/personal/ratelimit.go`: token bucket **per-thread** (mặc định 1 msg / 1.2s) + **global** (mặc định 30 msg / 60s) + jitter 200–600ms giữa các chunk.
- Config mới trong `config.ZaloPersonalConfig` (và 2 surface còn lại — §3):
  ```go
  SendRatePerMinute int  // 0 = default 30
  SendDelayMS       int  // 0 = default 1200
  DailySendCap      int  // 0 = default 500; -1 = unlimited
  ```
- **Mọi API call outbound** đều qua limiter: upload ảnh/file, `SendImage`, `SendFile`, `SendMessage`, typing (typing có thể bỏ qua hoặc bucket riêng). Không chỉ text chunk.
- Chunk đầu tiên không delay per-thread; mọi call đều phải acquire global bucket và check `DailySendCap`.
- Per-thread bucket map cần **eviction** (LRU hoặc TTL) để không phình theo số thread.
- Không dùng `time.Sleep` thật trong test; inject `now func() time.Time` hoặc fake clock.

**Acceptance:**
- Test: 5 chunk → khoảng cách ≥ delay, tổng thời gian nằm trong biên mong đợi (fake clock).
- Test: vượt `DailySendCap` → trả lỗi rõ ràng (`ErrDailyCapReached`), không silent drop.
- Test: media + text trong cùng `Send` đều bị tính vào global bucket.

---

### P0.3 — Phân loại lỗi gửi + xử lý auth

**Vấn đề:** cookie hết hạn giữa phiên → mọi send fail với `error code N`, agent không biết, user không biết. Source `ZaloApiError` **chỉ mang `code`**, không có bảng phân loại; docs cũng không có.

**Thay đổi:**
- `protocol/errors.go`: `type APIError struct { Code int; Op string }` + phân loại:
  - `ErrAuthExpired` — code thuộc nhóm cookie/session hết hạn. **Không restart mù**: vì không có refresh token, `authenticate()` với preloaded creds lỗi sẽ return ngay (`personal/auth.go`), không fallback file/QR. Đúng hành vi: `MarkFailed(..., ChannelFailureKindAuth, false)` + remediation `reauth` (UI đã có `ZaloPersonalQRDialog`), trả lỗi rõ cho caller.
  - `ErrTransient` — mạng/5xx/timeout → retry 2 lần, backoff 1s/3s.
  - `ErrPolicy` — bị chặn, nội dung không hợp lệ → không retry, trả lỗi cho agent để nó đổi cách trả lời.
- **Check cả 2 lớp `error_code`:** source kiểm tra `error_code` ở envelope ngoài **và** trong `data` đã decrypt. Hiện Go chỉ check envelope ngoài → thêm check lớp trong trước khi parse `{msgId}`.
- Nếu vẫn cần restart (ví dụ listener chết), dùng **single-flight**: một mutex/`sync.Once` quanh `restart` để `Send` và `listenLoop` không restart đồng thời (tránh 2 session/code 3000). Không gọi `restart` từ `Send` khi chưa có single-flight.
- Danh sách code: ghi comment các code **quan sát thực tế** (kèm ngày/log), không hard-fail code lạ (mặc định `ErrTransient`).

**Acceptance:**
- Test bảng: mỗi nhóm code map đúng loại.
- Test: lỗi auth → health `failed` + remediation `reauth` đúng 1 lần, **không** restart loop.
- Test: inner `error_code != 0` trong `data` → trả `APIError`, không parse `msgId` rỗng.

---

### P0.4 — Persist cookie sau khi xoay vòng

**Vấn đề:** cookie jar tự refresh trong phiên nhưng không được ghi lại → restart là phải login lại (hoặc tệ hơn: dùng cookie cũ đã rotate → bị đá).

**Thay đổi:**
- Hook cookie jar: wrap `http.CookieJar` (delegate + notify khi `SetCookies`) hoặc wrap `RoundTripper` để đọc `resp.Cookies()`. Debounce 30s.
- **Per-host:** `LoginWithCredentials` seed cookie cho từng host service-map (`seedServiceMapCookies`). Khi persist, phải capture cookie của **tất cả host** đã seed, không chỉ `DefaultBaseURL`; nếu không sẽ mất cookie rotate riêng của `tt-group-poll-wpa.chat.zalo.me` v.v.
- **DB instance:** `personal/factory.go` hiện chỉ nhận `preloadedCreds`, không có writer. Thêm callback `CredentialSaver func(ctx, *protocol.Credentials) error` (hoặc interface store) và truyền từ `cmd/gateway.go`; file chỉ là fallback dev.
- **Flush on stop:** debounce 30s có thể mất rotation cuối; `Stop()` phải flush ngay nếu dirty.
- Chỉ ghi khi credentials thực sự đổi (so sánh hash); file `0600`, DB dùng cơ chế mã hoá hiện có; không log cookie.

**Acceptance:**
- Test: jar thay đổi → file/DB được cập nhật, không ghi lặp lại khi không đổi.
- Test: cookie rotate ở host service-map → persist đủ host đó.
- Test: `Stop()` flush dirty state.

---

### P0.5 — Health state khi channel bỏ cuộc

**Vấn đề:** `restartWithBackoff` hết 10 lần → `slog.Error` rồi im. Không ai biết bot đã chết.

**Thay đổi:**
- **Không có health event bus và không có state `dead`.** Health là snapshot: `ChannelHealthStateFailed` (`internal/channels/health.go:19`) + `BaseChannel.MarkFailed` (`internal/channels/channel.go:541`). Dùng:
  ```go
  c.MarkFailed("Zalo Personal stopped", "gave up after 10 restart attempts", channels.ChannelFailureKindAuth, false)
  ```
- Remediation `reauth`/`open_credentials` đã có trong `ChannelHealth`; UI đã có `ZaloPersonalQRDialog` cho re-auth.
- **Bỏ** "gửi tin nhắn cảnh báo tới owner qua channel khác" — channel layer không có quyền truy cập channel khác; đây là scope creep. Nếu cần notify, làm ở gateway layer sau.

**Acceptance:**
- Test: giả lập auth fail 10 lần → health state `failed`, `retryable=false`, remediation `reauth`, đúng 1 lần.
- Test: `MarkHealthy` sau restart thành công reset failure counters (theo `mergeChannelHealth`).

---

### P0.6 — Vá `downloadFile` (auth + cap + dọn temp)

**Thay đổi:**
- Đổi thành method trên `*Channel` (hoặc nhận `sess *protocol.Session`), dùng `sess.Client` thay client mới.
- Header: `User-Agent: sess.UserAgent`, `Referer: https://chat.zalo.me/` (API đã có `setDefaultHeaders`, nhưng `downloadFile` đang dùng client trần).
- Thêm field `MediaMaxMB int` vào `ZaloPersonalConfig` (**hiện chưa có**; chỉ `ZaloConfig` OA có) + 2 surface còn lại; default 20.
- **Dọn temp đúng tầng:** `downloadFile` đã xoá temp trên error path; temp còn lại sau success được `persistMedia` copy vào workspace nhưng **không xoá source** (`internal/agent/media.go:190-280`). `claimTempMedia`/`releaseTempMedia` chỉ xử lý **outbound** media (`internal/channels/dispatch.go:197-233`).
  - Fix: sau khi `persistMedia` copy thành công, xoá source nếu source nằm trong `os.TempDir()` (áp dụng chung cho mọi channel, không chỉ Zalo).
  - Cleanup lúc `Stop()` **không đủ** (process chạy lâu vẫn tích luỹ). Nếu không muốn đụng agent layer, phải copy ngay trong channel vào workspace — nhưng channel không có `ToolWorkspaceFromCtx`; phương án agent-layer là đúng.
- Cập nhật scope file (§12) để cho phép `internal/agent/media.go`.

**Acceptance:**
- Test: server giả yêu cầu `Referer` → download thành công (trước đó fail).
- Test: file > cap → trả lỗi rõ, không để lại temp.
- Test: sau `persistMedia` thành công, source temp trong `os.TempDir()` bị xoá.

---

## 5. P1 — Chất lượng hội thoại

Mục tiêu: tin nhắn bot gửi trông như người thật gửi, và agent đọc được nhiều loại nội dung hơn. Ước lượng lại: **~9–10 ngày** (styles offset + quote plumbing + mention resolution).

### P1.1 — Markdown → Zalo styles (thay vì strip)

**Vấn đề:** `zalo.StripMarkdown` xoá sạch định dạng. Zalo Personal hỗ trợ style, nhưng **wire format không phải** field `styles` như docs.

**Thay đổi:**
- `internal/channels/zalo/styles.go` (mới, dùng chung được về sau):
  ```go
  type StyleSpan struct { Start, Len int; St string } // "b", "i", "s", "c_db342e", "f_13", "f_18", "lst_1", "lst_2", "ind_$"
  func MarkdownToZaloStyles(md string) (text string, spans []StyleSpan)
  ```
- Wire thực tế (source `sendMessage.ts` `handleStyles`):
  ```json
  { "textProperties": "{\"styles\":[{\"start\":0,\"len\":5,\"st\":\"b\"}],\"ver\":0}" }
  ```
  Không gửi `styles` top-level. `ind_$` phải thay bằng `ind_10`, `ind_20`… theo `indentSize` (default 1).
- Hỗ trợ tối thiểu: `**bold**`, `*italic*`, `~~strike~~`, dòng `- ` → `lst_1`, `1. ` → `lst_2`, `# heading` → bold + `f_18`. **Không** cố dịch bảng markdown — chuyển thành list.
- **Offset là UTF-16 code unit** (JS string index), không phải byte/runes. Tiếng Việt có dấu: 1 code point precomposed = 1 UTF-16 unit nhưng 2–3 byte trong Go. Viết helper `utf16Len(s)` / `byteToUTF16Offset(s, byteOff)` và test với tiếng Việt + emoji (emoji ngoài BMP = 2 UTF-16 units).
- **Chunking:** `channels.ChunkMarkdown` trim whitespace và chèn fence khi force-split (`internal/channels/chunking.go:11-64`) → offset không map tuyến tính. Cách an toàn: **chunk trước, tính styles trên từng chunk** (không tính spans trên toàn văn rồi cắt). Nếu vẫn muốn cắt spans, phải có map offset sau trim/fence.
- `Send()`: `cfg.RichText` default **false** (giữ hành vi cũ); bật true mới dùng styles; false → `StripMarkdown`.
- Zalo OA giữ nguyên `StripMarkdown`.

**Acceptance:**
- Snapshot test: 10 mẫu markdown thực tế → (text, spans) ổn định.
- Golden test tiếng Việt có dấu + emoji: `start/len` đúng UTF-16, không lệch.
- Property test: spans không overlap quá biên; tổng `len` ≤ `utf16Len(text)`.
- Test chunk boundary: câu dài có bold/list → style đúng trong từng chunk.

---

### P1.2 — Outbound quote (reply)

**Vấn đề:** agent trả lời không gắn với tin gốc → trong nhóm đông người rất khó theo dõi.

**Thay đổi:**
- `protocol.SendMessage` nhận `SendOptions{ Text, Styles, Quote, Mentions, TTL }`.
- **Cache hiện tại không đủ:** `replycontext.Message` chỉ có `IDs/ParentIDs/ParentQuote/Sender/Body` (`replycontext/reply-context-cache.go:25-33`), không có raw `TMessage`. `TMessage` Go cũng thiếu `propertyExt` và `ttl` (source `TMessage` có cả hai). Phải:
  - Thêm `PropertyExt json.RawMessage` + `TTL int` vào `TMessage` (hoặc lưu raw JSON của quote).
  - Thêm struct `quoteSource{ Content, MsgType, PropertyExt, UIDFrom, MsgID, CliMsgID, TS, TTL }` lưu theo `msgId` (cache riêng hoặc mở rộng `replycontext.Cache`).
- **Ai chọn tin để quote?** `Channel.Send` chỉ có `chatID`; phải truyền target qua `bus.OutboundMessage.Metadata["quote_msg_id"]` (agent/reply pipeline set khi trả lời). Fallback "last inbound per thread" chỉ bật khi config cho phép; không mặc định.
- Wire quote (source `sendMessage.ts`):
  - `qmsgOwner`, `qmsgId`, `qmsgCliId`, `qmsgType` (map `getClientMessageType`), `qmsgTs`, `qmsgTTL`.
  - `qmsg`: nếu `content` là string → chính string; nếu object → `""` (trừ `chat.todo` có nhánh riêng).
  - `qmsgAttach`: **chỉ group**, dựng từ object content: `{...content, thumbUrl: thumb, oriUrl: href, normalUrl: href}`; `chat.todo` có `properties` riêng.
  - `qmsgType` map: `webchat=1`, `chat.voice=31`, `chat.photo=32`, `chat.sticker=36`, `chat.doodle=37`, `chat.recommended=38`, `chat.link=38`, `chat.video.msg=44`, `share.file=46`, `chat.gif=49`, `chat.location.new=43`, default `1`.
- `cfg.QuoteReply` default **false** (giữ hành vi cũ). Chỉ quote khi có target + trong cửa sổ thời gian (server có thể từ chối; source không enforce — phải E2E verify, đừng hard-code 10 phút khi chưa có bằng chứng).

**Acceptance:**
- Test: cache hit → payload đúng các key `qmsg*`; cache miss → gửi không quote, không lỗi.
- Test: text quote vs photo quote vs `chat.todo` → `qmsg`/`qmsgAttach` đúng.
- Test: user vs group → `qmsgAttach` chỉ có ở group.
- E2E: reply vào tin user trong nhóm → Zalo hiển thị trích dẫn.

---

### P1.3 — Outbound mentions

**Vấn đề:** agent không thể tag ai trong nhóm (`@anh A` chỉ là text, người đó không nhận thông báo).

**Thay đổi:**
- **Cần 2 API, không phải 1:** source dùng
  1. `getGroupInfo(groupId)` → service `group`, `POST /api/group/getmg-v2`, body `gridVerMap: {"<groupId>":0}` → `gridInfoMap[groupId].memVerList` (member IDs).
  2. `getGroupMembersInfo(memberIds)` → service `profile`, `GET /api/social/group/members?params=...`, body `friend_pversion_map: ["<id>_0", ...]` → `profiles`.
  `ListGroups`/`FetchGroups` hiện chỉ trả GroupID/Name/Avatar/TotalMember, **không có member list**.
- Cache member map theo group (TTL, ví dụ 10 phút) để tránh gọi API mỗi tin.
- Parse `@Tên` trong output, so khớp **chính xác** với `displayName`/`zaloName`; không fuzzy. Không resolve được → để text, log debug.
- Wire thực tế: `mentionInfo: JSON.stringify([{pos, uid, len, type}])`, `type=1` khi `uid == "-1"` (@all); **không** gửi field `mentions`.
- Offset `pos/len` là **UTF-16 code unit**; dùng cùng helper với P1.1. Tính **sau khi chunk** (mỗi chunk một mảng mention); mention vắt qua ranh giới chunk → bỏ mention đó, giữ text.
- **Phụ thuộc P2.1:** `GetGroupInfo`/`GetGroupMembersInfo` phải được implement **trước** task này (kéo lên P1 hoặc defer mentions sang P2). Không để P1.3 chờ P2.
- `cfg.MentionsEnabled` default **false** (giữ hành vi cũ) + 2 surface config.

**Acceptance:**
- Test: tên có dấu, emoji, tên trùng → chọn đúng/không chọn; `len` đúng UTF-16.
- Test: 2 mention cùng tin → 2 span không overlap.
- Test: mention vắt chunk boundary → không sinh mention lỗi.
- Test: `uid == "-1"` → `type=1`.

---

### P1.4 — Inbound content router theo `msgType`

**Vấn đề:** sticker/link/location/poll/card/video bị flatten thành "file" hoặc mất trắng.

**Thay đổi:**
- **`MsgType` nằm trên `TMessage`, không phải `Content`** (`protocol/message.go:24`). Không thêm `MsgType()` vào `message_content.go` như plan cũ. Phải thread `msgType` + `notify` qua `handleDM`/`handleGroupMessage` → `extractContentAndMedia(content, msgType, notify)` và `cacheBodyForContent`.
- `content-media.go` đổi thành bảng route (msgType lấy từ source `getClientMessageType` + payload thực tế):
  | msgType | Hành vi |
  |---|---|
  | `webchat`, `chat.photo` | giữ nguyên (text / tải ảnh) |
  | `chat.sticker` | `[sticker id=<id>]` + optional fetch thumb (P2.2) |
  | `chat.voice` | tải về → `[voice <duration>s]`; STT chỉ khi có config STT (hiện Zalo Personal chưa có) |
  | `chat.video`, `chat.video.msg` | `[video <duration>s]` + thumbnail nếu có, chỉ tải file nếu < cap |
  | `share.link`, `chat.link` | `[link] <url>` — **không tải** |
  | `chat.location.new` | `[location lat,lon] <desc>` |
  | `chat.poll`, `chat.todo`, `chat.card`, `chat.bankcard` | JSON gọn trong tag `[zalo:<type>]` |
  | `chat.doodle`, `chat.gif`, `chat.recommended`, `share.file` | tag + metadata, không tải nếu không cần |
  | unknown | `notify` field nếu có, else `[unsupported: <msgType>]` + raw JSON ≤ 512B |
- **Sửa G7:** đổi `Attachment.URL()` ưu tiên: với ảnh/video ưu tiên `OriURL` → `NormalURL` → `Href`; với link ưu tiên `Href`. Không để `Href` luôn thắng.
- Nguyên tắc: **không bao giờ trả chuỗi rỗng** — luôn có gì đó để model quyết định.

**Acceptance:**
- Table test cho từng msgType với payload thật đã lưu trong `testdata/`.
- Test G7: payload có `href` là trang HTML + `oriUrl` là ảnh → tải `oriUrl`, không tải HTML.
- Không regression: ảnh + caption giữ nguyên hành vi hiện tại.

---

### P1.5 — Listener reaction + undo + group event

**Vấn đề:** user thả ❤️ hoặc thu hồi tin → agent mù; history còn nội dung đã thu hồi.

**Thay đổi:**
- **Sửa carrier theo source (`listen.ts`):**
  - `501` / `551` subCmd 0: DM messages; nếu `content` là object có `deleteMsg` → **undo**, ngược lại message.
  - `521` subCmd 0: group messages; `content.deleteMsg` → undo.
  - `601` subCmd 0: `file_done`, `group` (group event), `fr` (friend event).
  - `612`: reactions (`reacts`, `reactGroups`); `610/611`: old reactions.
  - `502`: seen/delivered; `602`: typing; `510/511`: old messages.
  - Go hiện chỉ có `1_501_0`/`1_521_0`/`1_601_0` → thêm `1_551_0`, `612` (và optional `610/611`, `502`, `602`, `510/511`).
- **Listener plumbing:** `Listener` chỉ có `messageCh`/`disconnectedCh`/`closedCh`/`errorCh`. Thêm channel event (ví dụ `reactionCh`, `undoCh`, `groupEventCh`) + accessor + `select` trong `listenLoop`; đảm bảo `Stop()` đóng/không block.
- Event types:
  - `ReactionEvent{ThreadID, MsgID, CliMsgID, UIDFrom, Icon, IsGroup}`; source `TReaction.content = {rMsg:[{gMsgID,cMsgID,msgType}], rIcon, rType, source}`; threadId = `idTo` nếu group hoặc `uidFrom=="0"`, ngược lại `uidFrom`.
  - `UndoEvent{ThreadID, MsgID, CliMsgID, IsGroup}`; source `TUndo.content = {globalMsgId, cliMsgId, deleteMsg, srcId, destId}`.
  - Group event: `GroupEventEvent{Type, GroupID, ActorIDs, Extra}`; source `act_type="group"`, `act` string, `data` object hoặc JSON string; bỏ `join_reject` (Zalo gửi trùng join).
- `personal/handlers.go`:
  - `undo` → xoá entry khỏi `GroupHistory` + `replyCache` theo `msgId` (thêm `Delete(msgID)` cho cả hai; hiện chỉ có `Clear()`).
  - `react` → log + chèn ghi chú nhẹ vào history (không đánh thức LLM), trừ khi `cfg.ReactionWakeup` bật. Field này cần thêm vào 3 surface config.
- Không đánh thức LLM cho reaction/undo mặc định.

**Acceptance:**
- Test parse cho `deleteMsg` (501/551/521), `612`, `601` group/fr (fixture JSON thật).
- Test: undo tin đã record → history không còn entry đó; reply vào tin đã thu hồi → không crash.
- Test: listener `Stop()` không deadlock khi event channel còn dữ liệu.

---

## 6. P2 — Mở rộng năng lực

Mục tiêu: mỗi API mới không cần một lần build Go. Ước lượng lại: **~10–12 ngày**.

### P2.1 — `protocol.Call` escape hatch

**Vấn đề:** mỗi endpoint Zalo = viết Go + release. Còn ~100 endpoint chưa dùng.

**Thay đổi:**
```go
// protocol/call.go
func Call(ctx context.Context, sess *Session, service, path string,
          method string, params map[string]any) (json.RawMessage, error)
```
- Tự encrypt `params`, set header, decrypt `data` field, trả raw JSON.
- **Check `error_code` cả envelope ngoài và lớp trong `data`** (source `utils.ts` `handleZaloResponse`).
- Hỗ trợ GET (query `params`) và POST (form `params`) vì source dùng cả hai.
- Build sẵn 5 method trên nền nó, dùng đúng service/path của source:

  | Method | Service | Path | Ghi chú |
  |---|---|---|---|
  | `GetGroupInfo` | `group` | `POST /api/group/getmg-v2` | body `gridVerMap: {"<id>":0}`; trả `memVerList` |
  | `GetGroupMembersInfo` | `profile` | `GET /api/social/group/members` | query `params`; body `friend_pversion_map: ["<id>_0"]` |
  | `GetStickers` | `sticker` | `POST /api/message/sticker` | cần thêm service key `sticker` |
  | `SendLink` | `chat` / `group` | user `/api/message/link`; group `/api/group/sendlink` | — |
  | `SendVoice` | `file` | `POST /api/message/forward` (user) / `/api/group/forward` (group) | cần `voiceUrl` public |
- **Service map:** `ZpwServiceMapV3` hiện chỉ có `chat/group/file/profile/group_poll`; source còn dùng `sticker` và `reaction`. Phải thêm field + case trong `getServiceURL` trước khi implement `GetStickers`/`addReaction`.
- Lưu ý: `custom` trong zca-js **không phải** raw HTTP escape hatch — nó là callback đăng ký vào API object. `protocol.Call` là thiết kế mới của Go, không phải port.
- Chỉ expose ra agent qua tool ở bước sau (whitelist path — **không** cho agent gọi path tuỳ ý).

**Acceptance:**
- Test: mock server nhận đúng envelope, trả JSON → parse đúng.
- Test: inner `error_code != 0` → trả `APIError`.
- Whitelist test: path ngoài danh sách → `ErrPathNotAllowed`.

---

### P2.2 — Gửi thêm loại nội dung

Theo thứ tự giá trị/chí phí:

| Loại | API | Ghi chú |
|---|---|---|
| Link rich preview | `sendLink` | Rẻ nhất, hữu ích nhất cho agent hay dẫn nguồn |
| Voice (TTS) | `sendVoice` | **Cần URL public**: source HEAD `voiceUrl` để lấy `content-length`, rồi gửi `msgInfo: {voiceUrl, m4aUrl, fileSize}` vào `/api/message/forward` (user) hoặc `/api/group/forward` (group). TTS local phải upload/host trước; plan cũ thiếu bước này |
| Sticker | `getStickers` + `sendSticker` | Cá tính hoá; cần cache danh mục |
| Reaction | `addReaction` | Ack nhẹ, tránh spam "OK" |
| Undo/delete | `undo` | **Van an toàn** khi bot gửi sai nhóm |
| Seen/delivered | `sendSeenEvent` / `sendDeliveredEvent` | Đánh dấu đã đọc, tránh "bot lờ tin" |

Service/path chính xác theo source:

| Loại | User | Group |
|---|---|---|
| `sendLink` | `chat[0] /api/message/link` | `group[0] /api/group/sendlink` |
| `sendSticker` | `chat[0] /api/message/sticker` | `group[0] /api/group/sticker` |
| `getStickers` | `sticker /api/message/sticker` | — |
| `addReaction` | `reaction[0] /api/message/reaction` | `reaction[0] /api/group/reaction` |
| `undo` | `chat[0] /api/message/undo` | `group[0] /api/group/undomsg` |
| `sendSeenEvent` | `chat[0] /api/message/seenv2` | `group[0] /api/group/seenv2` |
| `sendVoice` | `file[0] /api/message/forward` | `file[0] /api/group/forward` |

**Lưu ý thiết kế:** reaction/undo cần `msgId + cliMsgId` của tin **đích** → phải lưu map `chatID → lastOutboundMsgID` trong `Send()`; undo còn cần `globalMsgId` nếu có. Không chỉ lưu `msgId`.

**Acceptance:** mỗi loại có test integration với mock; undo phải kiểm tra cửa sổ thời gian hợp lệ; voice phải test với URL public thật hoặc mock HEAD.

---

### P2.3 — Group events

**Thay đổi:**
- Parse `601` nhánh `act_type="group"` → `GroupEventEvent{Type, GroupID, ActorIDs, Extra}`.
- Source có **22 type**, không phải 20: `JOIN_REQUEST, JOIN, LEAVE, REMOVE_MEMBER, BLOCK_MEMBER, UPDATE_SETTING, UPDATE, NEW_LINK, ADD_ADMIN, REMOVE_ADMIN, NEW_PIN_TOPIC, UPDATE_PIN_TOPIC, REORDER_PIN_TOPIC, UPDATE_BOARD, REMOVE_BOARD, UPDATE_TOPIC, UNPIN_TOPIC, REMOVE_TOPIC, ACCEPT_REMIND, REJECT_REMIND, REMIND_TOPIC, UPDATE_AVATAR, UNKNOWN` (mapping `act` → type trong `utils.ts`).
- Bỏ `join_reject` (Zalo gửi trùng join); bỏ qua `UNKNOWN` nhưng log.
- Hành vi mặc định: log + cập nhật cache tên nhóm. Không tự gửi tin.
- Hook tuỳ chọn: `cfg.WelcomeOnJoin` (default false) → gửi tin chào (template cấu hình được); cần thêm vào 3 surface config.
- `JOIN_REQUEST` → expose cho tool duyệt thành viên (`reviewPendingMemberRequest`).
- Friend event (`act_type="fr"`) ngoài scope; nếu parse thì chỉ log, bỏ `req` (Zalo gửi trùng `req_v2`).

**Acceptance:** test parse đủ 22 type (fixture thật); rename nhóm → `ListGroups` trả tên mới; `join_reject` không tạo event.

---

### P2.4 — Multi-account / proxy / vault

- `cfg.ProxyURL` (cần thêm vào 3 surface config) → set cả:
  - `sess.Client.Transport = &http.Transport{Proxy: http.ProxyURL(u)}`
  - `websocket.Dialer{Proxy: http.ProxyURL(u)}` — hiện `ws_client.go:26` dùng `websocket.Dialer` không có proxy.
- Credentials: nếu instance có DB thì ưu tiên DB (đã có `preloadedCreds`); file chỉ là fallback dev. Việc "chuyển sang secret vault" là **optional/deferred** — credentials là cookie, không phải API key; vault hiện có thể không phù hợp. Nếu làm, tách plan riêng.
- Nhiều account = nhiều channel instance (đã hoạt động) — tài liệu hoá thay vì thêm code.

---

## 7. Thiết kế chi tiết — thứ tự gọi trong `Send()`

```
Send(ctx, msg)
 ├─ resolve threadType (approvedGroups → metadata.group_id)
 ├─ resolve quote: metadata["quote_msg_id"] → raw quote cache
 │    (không có target → không quote; không fallback last-inbound mặc định)
 ├─ text: StripMarkdown hoặc MarkdownToZaloStyles (UTF-16 spans)
 ├─ resolve mentions (group only, sau chunk; exact match)
 ├─ chunk 2000 ký tự TRƯỚC, rồi tính styles/mentions trên từng chunk
 ├─ for each outbound API call (upload ảnh/file, SendImage, SendFile, SendMessage):
 │    global limiter + daily cap + per-thread delay (chunk đầu không delay)
 ├─ protocol.SendMessage(opts) với wire keys:
 │    message, clientId, ttl, toid|grid, visibility,
 │    mentionInfo (JSON string), textProperties (JSON string),
 │    qmsgOwner/qmsgId/qmsgCliId/qmsgType/qmsgTs/qmsg/qmsgAttach/qmsgTTL
 ├─ lưu lastOutboundMsgID + cliMsgId (cho reaction/undo)
 └─ lỗi: APIError → auth/transient/policy
      auth → MarkFailed + remediation reauth (không restart mù)
```

**Điểm dễ sai:**
- Offset `styles`/`mentions` là **UTF-16 code unit**, không phải byte.
- `ChunkMarkdown` trim/fence-repair làm lệch offset → chunk trước, style/mention sau.
- `qmsgAttach` chỉ group; `qmsg` rỗng với object content (trừ `chat.todo`).
- `mentionInfo`/`textProperties` là JSON **string**, không phải object.
- Media upload/send cũng phải qua limiter.

---

## 8. Test plan

| Lớp | Nội dung | File |
|---|---|---|
| Unit | dedupe, rate limiter (fake clock), error classify, style compiler, UTF-16 helper, span split | `personal/*_test.go` |
| Parser | fixture JSON thật cho `501/551/521/601/612/502/602/510/511` + mọi `msgType` | `protocol/testdata/` |
| Contract | `SendOptions` → envelope đúng shape (mock server), gồm `textProperties`/`mentionInfo`/`qmsg*` | `protocol/send_test.go` |
| Contract | inner `error_code != 0` → `APIError` | `protocol/errors_test.go` |
| Config | field mới có mặt ở `ZaloPersonalConfig` + `zaloInstanceConfig` + UI schema | `personal/factory_test.go`, UI test nếu có |
| E2E | gửi text/ảnh/file + reply + undo trên account thật | `tests/zalo_e2e` |
| Regression | ảnh + caption, quote ảnh, group history/mention gating, G7 URL priority | `personal/handlers_test.go` |

Ngưỡng merge: `go test ./internal/channels/zalo/... -race` xanh, `go vet` sạch.

---

## 9. Rủi ro & rollback

| Rủi ro | Mức | Giảm thiểu |
|---|---|---|
| Zalo đổi giao thức / field | Cao | `APIError` không panic; unknown `msgType` vẫn có fallback text |
| Source zca-js đổi version | Trung bình | Pin v2.2.0; chạy lại Phụ lục A khi bump |
| Docs và source lệch (styles/mentions) | Cao | Luôn bám source; test contract với mock |
| Rate limit quá chặt → bot chậm | Trung bình | config `send_delay_ms`, default 1.2s; tắt được |
| Style/mention offset lệch (UTF-16) | Cao | helper UTF-16 + golden test tiếng Việt/emoji; `rich_text=false`/`mentions_enabled=false` rollback |
| Mention sai người | Trung bình | chỉ mention khi khớp **chính xác** tên; không fuzzy |
| Reaction/undo parse sai carrier | Cao | bám `612` + `deleteMsg` trong `501/551/521`; fixture thật |
| Khoá tài khoản | Cao | xem §10 |

**Rollback:** mỗi feature sau một cờ config; tắt cờ = hành vi cũ. Không có migration dữ liệu.

---

## 10. Chính sách chống khoá tài khoản (bắt buộc)

1. **Tài khoản riêng**, không dùng số chính. Không đăng nhập cùng account trên app + bot cùng lúc (code 3000).
2. **IP ổn định**: bật `proxy` nếu host khác vùng thường dùng.
3. **Hạn mức**: ≤ 30 msg/phút, ≤ 500 msg/ngày cho một instance; không gửi hàng loạt/không broadcast. `DailySendCap` default **500** (không phải 0 = unlimited); `-1` mới là unlimited.
4. **Không thay đổi UA** giữa các phiên — UA lạ là tín hiệu mạnh.
5. **Không tự động kết bạn / vào nhóm / mời** bằng bot.
6. **Dừng ngay** khi gặp lỗi nhóm policy; không retry mù.

Văn bản gốc của tác giả zca-js cũng nêu rõ: dùng thư viện là trái chính sách Zalo, tài khoản có thể bị vô hiệu hoá.

---

## 11. Ngoài phạm vi (deferred)

- zBusiness: `createAutoReply`, catalog, product.
- `createPoll`, `createReminder`, note/board.
- Label/pin/mute/archive conversation management.
- Forward nhiều thread một lúc.
- Friend events (`act_type="fr"`), old messages (`510/511`), typing event (`602`), seen/delivered (`502`) — chỉ làm nếu có nhu cầu cụ thể.
- Secret vault cho credentials — tách plan riêng.

Lý do: hiếm dùng cho agent hội thoại, thêm bề mặt rủi ro mà không tăng giá trị.

---

## 12. Checklist PR

- [ ] `go test ./internal/channels/zalo/... -race` xanh
- [ ] `go vet ./...` sạch
- [ ] Cờ config mới có default an toàn, có trong **3 surface** (`ZaloPersonalConfig`, `zaloInstanceConfig`, `channel-schemas.ts`) + docs/config mẫu
- [ ] Không log cookie/IMEI/secretKey
- [ ] Có fixture testdata cho mọi parser/frame mới (`501/551/521/601/612/502/602/510/511`, `msgType`)
- [ ] Check `error_code` cả envelope ngoài và lớp trong `data`
- [ ] UTF-16 offset test với tiếng Việt có dấu + emoji
- [ ] Temp media cleanup đúng tầng (`internal/agent/media.go` nếu cần)
- [ ] Mô tả PR nêu rõ hành vi **trước/sau** và cách rollback
- [ ] File được phép đụng: `internal/channels/zalo/**`, `internal/config/config_channels.go`, `internal/agent/media.go` (chỉ temp cleanup), `ui/web/src/pages/channels/channel-schemas.ts`, `tests/zalo_e2e`, `docs/**`

---

## Phụ lục A — Source-verified corrections (zca-js 2.2.0)

Nguồn: `RFS-ADRENO/zca-js` @ `main` (package `2.2.0`), docs mirror `https://zca-js.tdung.com` / gitbook `tdung.gitbook.io/zca-js`. Docs chỉ dùng để tra tên API; mọi wire-level dưới đây lấy từ source.

### A.1 Listener frame matrix (`src/apis/listen.ts`)

| cmd | subCmd | Ý nghĩa | Ghi chú |
|---|---|---|---|
| `501`, `551` | 0 | DM messages | `content` object có `deleteMsg` → **undo**, ngược lại message |
| `521` | 0 | Group messages | `content.deleteMsg` → **undo** |
| `601` | 0 | Controls | `act_type=file_done` → upload callback; `act_type=group` → group event; `act_type=fr` → friend event |
| `612` | — | Reactions | `{reacts, reactGroups}`; `content` là JSON string |
| `610`, `611` | — | Old reactions | `reacts` (DM) / `reactGroups` (group) |
| `502` | 0 | Seen/delivered | `{delivereds, seens}` |
| `602` | 0 | Typing | `actions[].act_type="typing"`, `act="typing"` hoặc `"gtyping"` |
| `510`, `511` | 1 | Old messages | DM/group history response |

### A.2 `sendMessage` wire (`src/apis/sendMessage.ts`)

- User: `toid`, `imei`, `ttl`, `visibility` undefined.
- Group: `grid`, `visibility: 0`, `imei` undefined.
- Chung: `message`, `clientId: Date.now()`, `ttl: ttl ?? 0`.
- Mentions: `mentionInfo: JSON.stringify([{pos, uid, len, type}])`; `type=1` nếu `uid=="-1"`; chỉ group.
- Styles: `textProperties: JSON.stringify({styles, ver:0})`; `ind_$` → `ind_10`/`ind_20`… (`indentSize` default 1).
- Quote:
  - `qmsgOwner`, `qmsgId`, `qmsgCliId`, `qmsgType`, `qmsgTs`, `qmsgTTL`.
  - `qmsg`: string content, hoặc `""` nếu content object (trừ `chat.todo`).
  - `qmsgAttach`: chỉ group; object content → `{...content, thumbUrl: thumb, oriUrl: href, normalUrl: href}`; `chat.todo` → `properties` riêng.
- `getClientMessageType`: `webchat=1`, `chat.voice=31`, `chat.photo=32`, `chat.sticker=36`, `chat.doodle=37`, `chat.recommended=38`, `chat.link=38`, `chat.video.msg=44`, `share.file=46`, `chat.gif=49`, `chat.location.new=43`, default `1`.
- Response wire: `data` decrypt ra `SendMessageResult = {msgId}` (source `resolveResponse<SendMessageResult>`). zca-js bọc thành `{message, attachment}` ở lớp JS; parser Go hiện tại `{msgId}` là đúng.

### A.3 `sendVoice` (`src/apis/sendVoice.ts`)

- Service `file`: user `POST /api/message/forward`, group `POST /api/group/forward`.
- **HEAD `voiceUrl`** để lấy `content-length` trước khi gửi.
- Params: `toId`/`grid`, `ttl`, `zsource:-1`, `msgType:3`, `clientId`, `msgInfo: JSON.stringify({voiceUrl, m4aUrl, fileSize})`, `imei`.
- Hệ quả: TTS local phải có URL public (hoặc upload trước), không gửi file path.

### A.4 Group APIs

- `getGroupInfo(groupId)`: service `group`, `POST /api/group/getmg-v2`, body `gridVerMap: {"<id>":0}`; trả `gridInfoMap[groupId].memVerList`.
- `getGroupMembersInfo(memberIds)`: service `profile`, `GET /api/social/group/members?params=...`, body `friend_pversion_map: ["<id>_0"]`; trả `profiles`.
- `ListGroups`/`FetchGroups` hiện tại **không** trả member list.

### A.5 Event payloads

- `TReaction.content`: `{ rMsg: [{gMsgID, cMsgID, msgType}], rIcon, rType, source }`; `threadId = idTo` nếu group hoặc `uidFrom=="0"`, ngược lại `uidFrom`.
- `TUndo.content`: `{ globalMsgId, cliMsgId, deleteMsg, srcId, destId }`.
- `TGroupEvent`: `act_type="group"`, `act` string, `data` object hoặc JSON string; `join_reject` bị bỏ; type mapping trong `utils.ts` (22 type, gồm `UPDATE_AVATAR`, `UNKNOWN`).
- Friend event: `act_type="fr"`, `act="req"` bị bỏ (Zalo gửi trùng `req_v2`).

### A.6 Error handling (`src/utils.ts`)

- `handleZaloResponse` kiểm tra `error_code` ở envelope ngoài; nếu `isEncrypted`, decrypt rồi kiểm tra `error_code` lớp trong.
- `ZaloApiError` chỉ có `code`; **không có bảng phân loại** auth/transient/policy → P0.3 phải tự xây từ code quan sát được.

### A.7 Docs vs source — các điểm docs gây hiểu nhầm

| Docs viết | Source thực tế |
|---|---|
| `styles: [{start,len,st}]` top-level | `textProperties: JSON.stringify({styles, ver:0})` |
| `mentions: [{pos,len,uid}]` | `mentionInfo: JSON.stringify([{pos,uid,len,type}])` |
| `quote: message` (truyền cả object) | Wire cần `qmsgOwner/qmsgId/qmsgCliId/qmsgType/qmsgTs/qmsg/qmsgAttach/qmsgTTL` |
| `sendVoice({voiceUrl})` | HEAD URL lấy content-length + gửi `msgInfo` qua service `file` |
| Listener chỉ có message/reaction/undo | Reaction = `612`, undo = `deleteMsg` trong `501/551/521`, group = `601 act_type=group` |
