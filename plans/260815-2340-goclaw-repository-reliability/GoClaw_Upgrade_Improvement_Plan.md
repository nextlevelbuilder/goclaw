# GoClaw Upgrade & Reliability Improvement Plan

## 0. Mục tiêu

Nâng cấp `nextlevelbuilder/goclaw` thành một **agent runtime ổn định, model-agnostic và chịu lỗi tốt**, đặc biệt khi dùng các model yếu/free/không ổn định.

### Mục tiêu chính

1. Loại bỏ hoặc giảm mạnh lỗi:
   - `⏳ Too many requests. Please wait a moment and try again.`
   - `❌ Something went wrong. Please try again.`
   - Agent báo lỗi dù task/tool vẫn đang chạy.
   - Agent đứng im vì stream không có token trong thời gian dài.
   - Agent kết thúc sớm trong khi task chưa đạt mục tiêu.
   - Model yếu sinh tool call lỗi, JSON lỗi, tool call không hợp lệ hoặc vòng lặp vô ích.
   - Provider/API timeout khiến toàn bộ run bị đánh dấu failed dù có thể tiếp tục.
2. Tách rõ **Provider Failure / Transport Failure / Runtime Failure / Tool Failure / Model Failure / User Limit**.
3. Biến agent loop thành một **durable state machine** thay vì phụ thuộc vào một request/stream duy nhất.
4. Thêm retry, backoff, rate-limit coordination, circuit breaker, provider health và model fallback.
5. Cho phép model yếu tiếp tục hoàn thành task bằng cơ chế **verification + recovery + continuation**, thay vì coi response đầu tiên là nguồn chân lý.
6. Không phá vỡ các tính năng hiện có của GoClaw: multi-provider, multi-tenant, channels, tools, MCP, teams, memory, PostgreSQL, TUI/UI/API.
7. Giữ GoClaw nhẹ: ưu tiên thay đổi trong core runtime/provider layer, tránh microservice hóa không cần thiết.

---

# 1. Baseline hiện tại

GoClaw hiện đã có nhiều thành phần đúng hướng:

- 8-stage agent pipeline: context → history → prompt → think → act → observe → memory → summarize.
- Provider abstraction với `Chat()` / `ChatStream()`.
- Nhiều provider và OpenAI-compatible endpoints.
- Retry chung cho `429, 500, 502, 503, 504` và một số lỗi network.
- Gateway rate limit/quota.
- `max_tool_iterations`, `max_tool_calls`.
- Streaming status và tool status.
- Built-in LLM tracing/telemetry.

Tuy nhiên retry hiện tại vẫn là retry cấp **LLM request**, chưa phải reliability layer hoàn chỉnh cho **agent run**.

Theo tài liệu hiện tại, retry provider dùng tối đa 3 attempts, initial delay 300ms, max delay 30s, jitter ±10%, retry `429/500/502/503/504`, timeout/reset/broken-pipe/EOF; `Retry-After` được ưu tiên nếu có. Đây là nền tảng tốt nhưng chưa đủ để xử lý các run dài, tool-heavy hoặc model yếu.

---

# 2. Chẩn đoán chính của các lỗi

## 2.1. `Too many requests` không chỉ do spam người dùng

Phải phân biệt tối thiểu 5 loại rate limit:

### A. Gateway rate limit

Request từ client/channel tới GoClaw vượt giới hạn.

### B. User/session quota

User đã dùng hết quota theo hour/day/week.

### C. Provider rate limit

LLM provider trả HTTP 429.

### D. Provider token limit

API giới hạn TPM/RPM; request hợp lệ nhưng output dài hoặc nhiều tool calls làm quota bị dùng nhanh.

### E. Model-induced amplification

Model yếu tạo nhiều tool calls/retries/loops, khiến một user request biến thành nhiều upstream requests.

Ví dụ:

```text
User request
  -> LLM #1
     -> tool
     -> LLM #2
        -> malformed tool call
        -> retry
        -> LLM #3
           -> tool
           -> LLM #4
...
```

Kết quả: một request nhìn bên ngoài nhưng thực tế có thể tạo hàng chục request upstream.

### Yêu cầu

Tách các counter sau:

- `gateway_requests`
- `agent_runs`
- `llm_attempts`
- `llm_retries`
- `tool_calls`
- `tool_retries`
- `provider_429`
- `provider_5xx`
- `provider_timeouts`
- `agent_recoveries`
- `agent_continuations`

Không được hiển thị mọi `429` thành một câu `Too many requests`.

---

# 3. Thiết kế Error Taxonomy mới

Tạo chuẩn lỗi nội bộ thống nhất.

## 3.1. Provider errors

```text
ErrProviderRateLimited
ErrProviderAuth
ErrProviderBadRequest
ErrProviderOverloaded
ErrProviderUnavailable
ErrProviderTimeout
ErrProviderStreamTimeout
ErrProviderConnection
ErrProviderInvalidResponse
ErrProviderContextOverflow
ErrProviderModelUnavailable
```

## 3.2. Model errors

```text
ErrModelEmptyOutput
ErrModelMalformedToolCall
ErrModelInvalidJSON
ErrModelUnsupportedToolCall
ErrModelRepeatedToolCall
ErrModelPrematureCompletion
ErrModelLooping
ErrModelLowSignal
```

## 3.3. Runtime errors

```text
ErrRunCancelled
ErrRunStalled
ErrRunDeadline
ErrRunStateLost
ErrRunRecoveryFailed
ErrRunConflict
```

## 3.4. Tool errors

```text
ErrToolTimeout
ErrToolUnavailable
ErrToolInvalidArgs
ErrToolPermissionDenied
ErrToolTransient
ErrToolPermanent
```

Tất cả error phải có:

- stable error code
- retryability
- user-facing severity
- provider-facing detail
- runId
- sessionId
- attempt
- stage
- cause

---

# 4. Provider Reliability Layer

## 4.1. Retry engine 2 tầng

Không dùng một retry duy nhất cho toàn bộ hệ thống.

### Layer 1 — HTTP/provider retry

Retry request tới provider khi phù hợp.

### Layer 2 — Agent continuation/recovery

Nếu request thất bại sau khi đã có partial progress, agent không quay lại từ đầu.

Ví dụ:

```text
Run R123
  iteration 7
  tool result đã lưu
  LLM request timeout

=> checkpoint state
=> provider retry/fallback
=> resume iteration 7
```

## 4.2. Backoff thông minh

Thay exponential backoff đơn giản bằng:

```text
Retry-After
    ↓
provider-specific hint
    ↓
exponential backoff
    ↓
full jitter
    ↓
bounded max delay
```

Ví dụ mặc định:

```text
attempt 1: 0.5–1.5s
attempt 2: 1–3s
attempt 3: 2–6s
attempt 4: 4–12s
attempt 5: 8–30s
```

Nhưng config theo provider/model.

## 4.3. Rate-limit aware retry

Khi nhận 429:

- đọc `Retry-After`.
- parse rate-limit headers nếu provider có.
- ghi nhận reset time.
- tránh đồng thời retry hàng loạt.
- dùng shared limiter theo provider + credential + model.

Không để 100 goroutine cùng retry sau 2s.

## 4.4. Single-flight / request coalescing

Các request cần cùng credential/provider phải chia sẻ knowledge về cooldown.

Ví dụ:

```text
Provider A = cooldown until 12:31:40

Run1 -> wait
Run2 -> wait
Run3 -> wait

Không:
Run1 -> retry
Run2 -> retry
Run3 -> retry
```

## 4.5. Circuit breaker

Mỗi provider/model pair có trạng thái:

```text
Healthy
  ↓ failures
Degraded
  ↓ repeated failures
Open
  ↓ cooldown
HalfOpen
  ↓ success
Healthy
```

Khi Open:

- không gửi request mới trong thời gian cooldown.
- chuyển fallback nếu có.
- báo user bằng status rõ ràng.

---

# 5. Provider / Model Health Registry

Tạo runtime registry:

```go
type ModelHealth struct {
    Provider            string
    Model               string
    ConsecutiveFailures int
    RateLimitUntil      time.Time
    TimeoutCount        int
    StreamStallCount    int
    ToolErrorRate       float64
    LastSuccessAt       time.Time
    LastFailureAt       time.Time
    CircuitState        CircuitState
}
```

Score model/provider theo:

```text
availability
latency
429 rate
5xx rate
timeout rate
stream stall rate
tool-call success rate
empty-output rate
completion reliability
```

Health score không phải benchmark intelligence. Đây là **runtime reliability score**.

---

# 6. Intelligent Model Fallback

## 6.1. Không fallback chỉ dựa vào model name

Định nghĩa capability profile:

```json
{
  "tool_calling": "strong",
  "structured_output": "strong",
  "reasoning": "medium",
  "long_context": "strong",
  "streaming": "strong",
  "vision": "none",
  "max_context": 128000
}
```

## 6.2. Fallback chain

Ví dụ:

```text
Primary model
   ↓
same provider stronger/similar model
   ↓
secondary provider
   ↓
recovery model
   ↓
fatal only
```

## 6.3. Không fallback bừa

Phân biệt:

- `429`: fallback ngay nếu cooldown dài.
- timeout: retry 1–2 lần rồi fallback.
- malformed tool call: retry với repair prompt; nếu lặp lại → fallback.
- context overflow: compact/prune trước, không đổi model ngay.
- tool failure: sửa tool args/retry tool, không đổi provider vô ích.

## 6.4. Fallback phải giữ context

Không tạo một conversation mới mất state.

Fallback request phải nhận:

- session state
- task objective
- current plan
- completed tool calls
- last tool outputs cần thiết
- failure reason ở mức an toàn

---

# 7. Fix lớn nhất: Agent Run phải là Durable State Machine

Đây là phần ưu tiên cao nhất.

## 7.1. State

```text
QUEUED
RUNNING
THINKING
WAITING_PROVIDER
WAITING_TOOL
RECOVERING
CONTINUING
COMPLETING
COMPLETED
FAILED
CANCELLED
```

Không được có tình trạng:

```text
UI = error
backend = still running
```

## 7.2. Run record

Mỗi run cần có:

```text
run_id
session_id
agent_id
parent_run_id
status
current_stage
current_iteration
current_tool
current_provider
current_model
started_at
heartbeat_at
last_progress_at
deadline_at
checkpoint_version
failure_code
failure_message
```

## 7.3. Heartbeat

Agent phải cập nhật progress heartbeat định kỳ.

Ví dụ:

```text
thinking -> heartbeat
LLM stream chunk -> heartbeat
tool started -> heartbeat
tool progress -> heartbeat
tool finished -> heartbeat
checkpoint -> heartbeat
```

## 7.4. Progress != token output

Một model không sinh token 20–60 giây vẫn có thể đang làm việc.

Progress phải tính cả:

- provider request active
- stream active
- tool active
- browser active
- shell active
- subagent active
- waiting retry backoff

---

# 8. Fix `Something went wrong` giả khi agent vẫn đang chạy

## 8.1. Tách UI error khỏi run error

Không cho channel tự kết luận run failed chỉ vì stream/socket đóng.

Ví dụ:

```text
WS disconnected
    !=
Run failed
```

## 8.2. Reconnect + resync

Client reconnect phải gọi:

```text
GET /runs/:id
GET /runs/:id/events?after=<cursor>
```

Server replay event từ checkpoint/event log.

## 8.3. Event sequence

Mỗi event có:

```text
sequence
run_id
timestamp
type
payload
```

Ví dụ:

```text
run.started
llm.started
llm.delta
llm.completed
tool.started
tool.progress
tool.completed
run.checkpoint
run.recovering
run.completed
```

Nếu websocket mất 10 giây, client không cần đoán; chỉ cần resync event từ sequence cuối.

---

# 9. Streaming Reliability

## 9.1. Stream watchdog

Phân biệt:

```text
no token
vs
no network activity
vs
no progress
```

Không timeout stream chỉ vì model không trả text token trong thời gian ngắn.

## 9.2. Adaptive timeouts

Timeout theo model profile:

```text
Fast model:      low first-byte timeout
Reasoning model: high first-byte timeout
Tool-heavy run:  high total deadline
Streaming:       separate idle timeout
```

Response header timeout phải đủ dài cho reasoning models.

## 9.3. Partial stream recovery

Nếu stream bị đứt sau 70% output:

- giữ partial output.
- lưu checkpoint.
- thử resume/reissue nếu provider không hỗ trợ resume.
- không gửi duplicate message.

## 9.4. Deduplication

Mọi external side effect phải có idempotency key:

```text
run_id + iteration + tool_call_id
```

Để retry không gửi lại email / Telegram / API mutation / file write ngoài ý muốn.

---

# 10. Weak Model Resilience Layer

Đây là phần để GoClaw hoạt động tốt hơn đáng kể với model yếu.

## 10.1. Tool-call repair

Khi model trả:

```json
{"name":"read_file", "arg":"x"}
```

trong khi schema yêu cầu:

```json
{"name":"read_file", "arguments":{"path":"x"}}
```

Runtime phải thử normalize/repair trước khi fail.

## 10.2. Invalid JSON repair

Pipeline:

```text
raw output
   ↓
parse
   ↓ fail
strict repair
   ↓ fail
minimal correction model
   ↓ fail
retry original model with compact error
   ↓
fallback
```

Không dùng repair vô hạn.

## 10.3. Empty output recovery

Nếu LLM trả empty:

1. retry cùng request.
2. đổi stream → non-stream nếu phù hợp.
3. gửi continuation.
4. fallback model.

## 10.4. Premature completion detector

Model không được coi là `done` chỉ vì sinh text cuối.

Runtime kiểm tra:

- task có yêu cầu tool không?
- tool calls cần thiết đã hoàn thành chưa?
- các acceptance criteria đã đạt chưa?
- file/output bắt buộc đã tồn tại chưa?
- có step đang pending không?

Nếu chưa:

```text
CONTINUE
```

thay vì `COMPLETED`.

## 10.5. Repetition / loop detector

Theo dõi:

- repeated tool + same args
- repeated error
- repeated prompt pattern
- repeated output fingerprint

Ví dụ:

```text
tool A(args X)
error E

5 lần liên tiếp
=> model loop
```

Xử lý:

```text
repair prompt
→ alter strategy
→ fallback model
→ abort only when necessary
```

---

# 11. Agent Goal / Completion Verification

Tạo `CompletionVerifier`.

## 11.1. Input

```text
task
plan
current state
artifacts
last actions
```

## 11.2. Output

```go
type CompletionResult struct {
    Complete bool
    Confidence float64
    Missing []string
    Reason string
}
```

## 11.3. Verification levels

### Level 0 — basic

Có output hay chưa.

### Level 1 — tool consistency

Tool calls cần thiết đã complete.

### Level 2 — artifact verification

File/DB/API state thực sự tồn tại.

### Level 3 — task acceptance

Task objective thực sự đạt.

### Level 4 — optional evaluator

Model evaluator/validator kiểm tra sâu.

---

# 12. Context Management nâng cấp

Model yếu thường xuống chất lượng mạnh khi context phình to.

## 12.1. Context budget theo stage

Không chỉ giới hạn tổng context.

Phân ngân sách:

```text
system
task
recent conversation
relevant memory
tool schema
tool output
scratch/reasoning
```

## 12.2. Tool-output compression

Tool output lớn phải được:

- truncate thông minh
- summarize
- giữ phần relevant
- giữ checksum/reference tới dữ liệu đầy đủ

## 12.3. Failure context compaction

Không nhét toàn bộ stack trace/API response vào prompt model.

Chỉ đưa:

```text
error class
important detail
what was attempted
what changed
next possible actions
```

---

# 13. Tool Execution Reliability

## 13.1. Retry classification

### Retry automatically

- network timeout
- connection reset
- temporary unavailable
- 429
- temporary 5xx

### Repair then retry

- invalid args
- malformed path
- missing optional field

### Never blindly retry

- permission denied
- destructive command
- invalid credentials
- permanent 4xx

## 13.2. Tool deadline

Mỗi tool có:

```text
timeout
soft timeout
hard timeout
retry policy
max concurrency
idempotency policy
```

## 13.3. Tool progress events

Tool dài phải phát event:

```text
tool.started
tool.progress
tool.log
tool.completed
```

để UI không nghĩ agent bị treo.

---

# 14. Concurrency & Backpressure

GoClaw có native concurrency nhưng cần backpressure theo tầng.

## 14.1. Global

Giới hạn tổng LLM calls.

## 14.2. Provider

Giới hạn theo provider.

## 14.3. Credential

Giới hạn theo API key/account.

## 14.4. Model

Giới hạn theo model.

## 14.5. Tenant/user

Giới hạn theo tenant/user.

Ví dụ:

```text
Global  = 100
Provider A = 30
Key A1 = 10
Model X = 6
User U1 = 2
```

Admission control phải xảy ra **trước khi** tạo upstream request.

---

# 15. Fair Queueing

Không để một tenant/model yếu chiếm hết worker.

Áp dụng weighted fair queue hoặc token bucket:

```text
priority: interactive
priority: normal
priority: background
```

Task đang retry cũng không được chiếm queue vô hạn.

---

# 16. Run Deadlines và Cancellation

Mỗi run có:

```text
soft deadline
hard deadline
idle deadline
provider deadline
tool deadline
```

Soft deadline:

- chuyển recovery strategy.

Hard deadline:

- terminate safely.
- lưu checkpoint.

Cancellation phải propagate tới:

```text
agent
→ provider
→ HTTP request
→ stream
→ tool
→ subprocess
→ browser
→ subagent
```

---

# 17. Checkpoint & Resume

Checkpoint sau mỗi transition quan trọng:

```text
before LLM
LLM complete
before tool
tool complete
after memory update
before fallback
before retry
```

Checkpoint cần đủ để resume:

```text
session state
iteration
messages
tool results
pending action
provider/model
retry count
```

Nếu process restart:

```text
RUNNING + stale heartbeat
=> RECOVERING
=> resume / mark failed safely
```

---

# 18. Event Log / Run Journal

Thêm durable run journal cho các event quan trọng.

Ví dụ PostgreSQL:

```sql
agent_runs
agent_run_events
agent_run_checkpoints
agent_run_attempts
```

Không cần log toàn bộ token vào DB mặc định.

Có thể cấu hình:

```text
minimal
standard
debug
```

---

# 19. Better User-Facing Status

Thay vì chỉ:

```text
❌ Something went wrong
```

dùng trạng thái có nghĩa:

```text
⏳ Waiting for provider capacity…
🔄 Retrying model request…
🧠 Model is still reasoning…
🔧 Recovering from a malformed tool call…
⚡ Switching to fallback model…
🛠️ Tool is still running…
🔁 Resuming task from checkpoint…
✅ Task completed
❌ Task failed
```

## Quy tắc

Không hiển thị internal error detail nhạy cảm.

Nhưng phải có correlation ID:

```text
Run #A8F2C1
```

để debug support.

---

# 20. Provider-specific Capability Profiles

Mỗi provider/model khai báo:

```text
streaming
reasoning
thinking tokens
function calling
parallel tool calls
JSON schema
vision
audio
max context
max output
first-byte characteristics
rate limit behavior
```

Runtime dùng capability profile để:

- chọn timeout.
- chọn retry.
- chọn fallback.
- chọn prompt mode.
- chọn tool schema.
- chọn stream/non-stream.

---

# 21. Reasoning Model Support

Các model reasoning thường cần thời gian lâu trước token đầu tiên.

Không dùng một `ResponseHeaderTimeout` thấp cho toàn bộ model.

Thiết kế:

```text
fast_chat_timeout
reasoning_header_timeout
stream_idle_timeout
stream_total_timeout
```

Có thể adaptive theo lịch sử:

```text
P95 first-token latency
P95 total latency
```

Sau đó tự điều chỉnh timeout trong giới hạn an toàn.

---

# 22. LLM Attempt Record

Mỗi lần gọi model cần lưu metadata:

```text
attempt_id
run_id
provider
model
started_at
first_byte_at
completed_at
status
http_status
retry_number
input_tokens
output_tokens
cached_tokens
reasoning_tokens
error_code
```

Không lưu secret hoặc raw prompt nếu cấu hình privacy không cho phép.

---

# 23. Observability mới

## Metrics

### Provider

```text
llm_requests_total
llm_success_total
llm_429_total
llm_5xx_total
llm_timeout_total
llm_stream_stall_total
llm_latency_ms
llm_first_token_latency_ms
```

### Agent

```text
agent_runs_total
agent_completed_total
agent_failed_total
agent_recovered_total
agent_continued_total
agent_premature_completion_total
agent_loop_detected_total
```

### Model quality/runtime

```text
empty_output_rate
malformed_tool_rate
tool_success_rate
tool_retry_rate
completion_verification_fail_rate
```

## Tracing

Span hierarchy:

```text
agent.run
  ├─ stage.context
  ├─ stage.prompt
  ├─ llm.attempt
  │    └─ stream
  ├─ tool.call
  ├─ llm.attempt
  ├─ recovery
  └─ completion.verify
```

---

# 24. Error Budget / Reliability SLO

Đặt SLO để không đánh giá bằng cảm giác.

Ví dụ mục tiêu:

```text
successful runs >= 99%
provider-induced recovery success >= 95%
false failure while backend still running = 0
message duplication = 0
unhandled stream disconnect < 0.1%
429-caused hard failures < 0.5%
```

Các con số trên là mục tiêu ban đầu, cần hiệu chỉnh bằng production telemetry.

---

# 25. Test Matrix bắt buộc

## Provider tests

- 200 success
- 429
- 429 + Retry-After
- 500
- 502
- 503
- timeout
- connection reset
- partial stream
- empty stream
- invalid SSE

## Model tests

- valid text
- valid tool call
- malformed tool call
- invalid JSON
- empty output
- repeated tool call
- premature final answer
- long reasoning before first token
- tool-heavy loop

## Runtime tests

- websocket disconnect
- reconnect
- process restart
- database unavailable
- tool process crash
- cancellation during LLM
- cancellation during tool
- provider switch during recovery

## Load tests

- 1 user / 1 run
- 100 users / concurrent runs
- same provider saturation
- one noisy tenant
- many weak-model loops
- repeated 429 storm

---

# 26. Chaos Testing

Tạo test harness inject:

```text
429 storm
random 5xx
slow first token
random stream close
random tool timeout
random DB latency
random websocket disconnect
```

Acceptance criteria:

> Không được có run bị chuyển sang `FAILED` nếu hệ thống vẫn có thể chứng minh nó đang active hoặc có checkpoint để resume.

---

# 27. Regression Tests cho lỗi đang gặp

Tạo test cases cụ thể:

## Case A — Too many requests

```text
Provider returns 429 x N
↓
Retry-After respected
↓
no retry storm
↓
circuit breaker
↓
fallback provider/model
↓
run continues
```

## Case B — Something went wrong

```text
stream disconnect
↓
backend run remains RUNNING
↓
client reconnect
↓
resync events
↓
final result delivered once
```

## Case C — Weak model malformed tool call

```text
bad tool JSON
↓
repair
↓
retry
↓
continue
```

## Case D — Weak model premature completion

```text
model says done
↓
CompletionVerifier = incomplete
↓
CONTINUING
↓
missing work completed
```

## Case E — Long reasoning

```text
no token for 30s
but connection active
↓
no false stall failure
↓
continue waiting
```

---

# 28. API / Protocol Improvements

Các API cần expose run lifecycle rõ ràng:

```text
POST   /runs
GET    /runs/:id
POST   /runs/:id/cancel
POST   /runs/:id/resume
GET    /runs/:id/events
GET    /runs/:id/attempts
GET    /runs/:id/checkpoints
```

WebSocket events phải có `sequence` để replay.

---

# 29. CLI / Debug commands

Thêm các command:

```bash
goclaw run status <run-id>
goclaw run inspect <run-id>
goclaw run events <run-id>
goclaw run attempts <run-id>
goclaw run recover <run-id>
goclaw provider health
goclaw provider cooldowns
goclaw model health
```

Mục tiêu: debug production mà không cần đọc log thủ công.

---

# 30. Configuration đề xuất

Ví dụ config mới:

```json
{
  "runtime": {
    "max_run_duration": "45m",
    "idle_timeout": "5m",
    "checkpoint_interval": "5s",
    "event_retention": "24h"
  },
  "reliability": {
    "provider_retry": {
      "max_attempts": 5,
      "initial_delay": "500ms",
      "max_delay": "30s",
      "jitter": true,
      "respect_retry_after": true
    },
    "circuit_breaker": {
      "enabled": true,
      "failure_threshold": 5,
      "cooldown": "30s"
    },
    "fallback": {
      "enabled": true,
      "max_hops": 2
    },
    "stream": {
      "idle_timeout": "2m",
      "default_header_timeout": "2m",
      "reasoning_header_timeout": "10m"
    }
  },
  "agent": {
    "completion_verification": true,
    "tool_repair": true,
    "loop_detection": true,
    "premature_completion_detection": true
  }
}
```

Các default thực tế cần benchmark trước khi release.

---

# 31. Implementation Architecture

Đề xuất package boundaries:

```text
internal/
  agent/
    runtime/
    state/
    recovery/
    completion/
    loopdetect/
    checkpoint/

  providers/
    retry/
    ratelimit/
    health/
    circuitbreaker/
    fallback/
    streaming/
    capabilities/

  tools/
    executor/
    retry/
    idempotency/
    progress/

  gateway/
    runs/
    events/
    reconnect/

  observability/
    metrics/
    tracing/
    logging/
```

Không nên để provider-specific retry logic rải khắp từng provider.

---

# 32. Migration strategy

## Phase 0 — Baseline & instrumentation

- [x] Chốt commit/tag làm baseline. (commits `5100087d`/`def1a9e6`, 2026-08-15)
- [x] Ghi nhận tất cả lỗi hiện có. (§2 plan, 2026-08-15)
- [x] Thêm runId/attemptId/correlationId. (runId trên runs record + trace correlation, 2026-08-16)
- [x] Thêm metrics cơ bản. (`internal/reliability/metrics.go`, phase-03, 2026-08-15)
- [ ] Thu thập P50/P95/P99 latency. (deferred — cần production telemetry)

## Phase 1 — Error taxonomy

- [x] Chuẩn hóa error codes. (phase-02, 2026-08-15)
- [x] Phân biệt retryable/permanent. (phase-02, 2026-08-15)
- [x] Phân biệt provider/runtime/tool/model error. (phase-02, 2026-08-15)
- [x] Chuẩn hóa user-facing error mapping. (severity trong taxonomy, phase-02, 2026-08-15)

## Phase 2 — Provider reliability

- [ ] Retry engine mới. (giữ retry đã có sẵn — plan không re-implement)
- [ ] Retry-After parser. (Retry-After đã được retry hiện tại ưu tiên; chưa tách parser riêng)
- [x] Distributed/shared cooldown. (RateLimitCoordinator, phase-03, 2026-08-15)
- [ ] Provider/model limiter. (backpressure/admission chưa wire — deferred)
- [x] Circuit breaker. (circuitbreaker.go, phase-03, 2026-08-15)
- [x] Provider health registry. (health.go, phase-03, 2026-08-15)

## Phase 3 — Durable Agent Run

- [x] Run state machine. (P0, 2026-08-16)
- [x] Heartbeat. (P0, 2026-08-16)
- [ ] Checkpoint. (deferred — resume = future phase; `checkpoint` column reserved)
- [x] Recovery worker. (P0 `RecoverStaleRuns`, 2026-08-16)
- [x] Event sequence/replay. (`runs.events` + `afterSeq`, 2026-08-16)

## Phase 4 — Streaming reliability

- [x] Adaptive timeout. (`reliability.stream.*` + `ModelSpec.StreamTimeoutMs`, 2026-08-16)
- [x] Stream watchdog. (idle/first-byte per-event reset, 2026-08-16)
- [ ] Partial-stream recovery. (deferred — cần checkpoint/resume từ Phase 3 trước)
- [x] WS reconnect/resync. (`runs.events` + `afterSeq`, merged Phase 3, 2026-08-16)
- [x] Duplicate suppression. (`FailoverStreamed` + `emitted` guards + UI seq dedup, 2026-08-16)

## Phase 5 — Weak-model resilience

- [x] Tool-call repair. (`repairToolCallArgs` — field-name normalize + LRU-gated schema-verified, 2026-08-17)
- [x] JSON repair. (`repairJSON` strict-safe, 2026-08-17)
- [x] Empty output recovery. (`ErrModelEmptyOutput` observe + `RecordLLMEmptyOutput`, 2026-08-17)
- [x] Repetition detector. (toolloop.go đã ship Phase P0 — metrics + classification wired 2026-08-17)
- [x] Premature completion detector. (`ContinuationGate` opt-in, 2026-08-17)
- [x] Completion verifier. (L0/L1 record-only, 2026-08-17)

## Phase 6 — Intelligent fallback

- [x] Model capability profile. (phase-06, 2026-08-17)
- [x] Fallback chain. (upstream orderByHealth + phase-06 policy, 2026-08-17)
- [x] Health-based routing. (strategy `health_order` + min_attempts_for_health, 2026-08-17)
- [x] Cooldown-aware routing. (skip diagnostics + CooldownTracker existing, 2026-08-17)
- [ ] Cost/latency/reliability policy. (deferred — cần production telemetry, 2026-08-17)

## Phase 7 — Tool reliability

- [ ] Tool retry classes. (phase-07, in progress, 2026-08-17)
- [ ] Tool deadlines. (phase-07, in progress, 2026-08-17)
- [ ] Tool progress. (phase-07, in progress, 2026-08-17)
- [ ] Idempotency. (phase-07, in progress 2026-08-17; metric-based classification gate, key-chain deferred)
- [ ] Side-effect safety. (phase-07, in progress 2026-08-17; classification gate, no auto-retry on destructive)

## Phase 8 — Context optimization

- [ ] Per-section token budgets.
- [ ] Tool-output compaction.
- [ ] Failure context compression.
- [ ] Long-session stabilization.

## Phase 9 — Testing

- [x] Unit tests. (12 cho reliability layer phase-03 + durable run records phase §7 + Phase 5 module tests, 2026-08-15/16)
- [ ] Integration tests.
- [ ] Provider simulation.
- [ ] Chaos tests.
- [ ] Load tests.
- [ ] Regression tests.

## Phase 10 — Production hardening

- [ ] SLOs.
- [ ] Alerts.
- [ ] dashboards.
- [ ] Runbook.
- [ ] automatic stale-run recovery.
- [ ] migration docs.

---

# 33. Commit strategy

Không gom toàn bộ thành một commit lớn.

Đề xuất:

```text
feat(runtime): introduce durable agent run state machine
feat(providers): add unified retry and rate-limit coordinator
feat(providers): add model health and circuit breaker
fix(streaming): make stream lifecycle resumable
fix(gateway): separate client disconnect from run failure
feat(agent): add tool-call repair and loop detection
feat(agent): add completion verification
feat(agent): add model-aware fallback routing
feat(tools): add idempotent retry and progress events
feat(observability): add reliability metrics and tracing
feat(test): add provider chaos and regression suite
```

---

# 34. Definition of Done

## Reliability

- [ ] Không còn hiển thị `Something went wrong` khi backend run vẫn đang active.
- [ ] 429 không còn lập tức biến thành hard failure nếu có khả năng chờ/fallback.
- [ ] Retry không tạo request storm.
- [ ] Provider unhealthy được cooldown/circuit-break.
- [ ] Run có thể resume từ checkpoint.
- [ ] Client disconnect không làm mất run.
- [ ] Duplicate final output = 0 trong test suite.

## Weak-model resilience

- [ ] Malformed tool calls được repair hoặc recovery.
- [ ] Empty output được recovery.
- [ ] Repeated tool loops được phát hiện.
- [ ] Premature completion được phát hiện.
- [ ] Task chỉ được đánh dấu complete khi verifier chấp nhận.

## Performance

- [ ] Không thêm latency đáng kể cho happy path.
- [ ] Retry/fallback chỉ kích hoạt khi cần.
- [ ] Memory/DB overhead được benchmark.
- [ ] Concurrency được kiểm soát bằng backpressure.

## Compatibility

- [ ] Existing providers vẫn hoạt động.
- [ ] Existing channels vẫn hoạt động.
- [ ] Existing tools/MCP vẫn hoạt động.
- [ ] Existing agent configs vẫn backward compatible hoặc có migration.

---

# 35. Kiến trúc mục tiêu

```text
                         ┌─────────────────────┐
                         │ Client / Channel    │
                         └──────────┬──────────┘
                                    │
                              Gateway / API
                                    │
                          Admission + Rate Limit
                                    │
                             ┌──────▼──────┐
                             │ Agent Run    │
                             │ State Machine│
                             └──────┬──────┘
                                    │
                    ┌───────────────┼────────────────┐
                    │               │                │
               Checkpoint       Event Log       Heartbeat
                    │               │                │
                    └───────────────┼────────────────┘
                                    │
                              Agent Executor
                                    │
                  ┌─────────────────┼─────────────────┐
                  │                 │                 │
               LLM Router      Tool Executor     Completion
                  │                 │              Verifier
                  │                 │                 │
        ┌─────────┴──────────┐      │                 │
        │                    │      │                 │
   Provider Health       Fallback   │            Recovery
   Rate Limit             Chain     │                 │
   Retry                  Circuit   │                 │
        │                 Breaker   │                 │
        └─────────┬──────────┘      │                 │
                  │                 │                 │
              LLM Providers      Tools/MCP            │
                                                     │
                                              CONTINUE / DONE
```

---

# 36. Nguyên tắc thiết kế cốt lõi

1. **429 không đồng nghĩa với agent failure.** Nó trước hết là một trạng thái scheduling/cooldown.
2. **Stream disconnect không đồng nghĩa run failure.** Run state là nguồn chân lý.
3. **Model output không phải nguồn chân lý duy nhất.** Runtime phải kiểm tra tool state và task state.
4. **Model yếu phải được runtime bảo vệ.** Không thể giả định mọi model đều tạo tool call/JSON/reasoning đúng.
5. **Retry request khác retry task.** LLM request có thể retry; agent run phải resume từ checkpoint.
6. **Không retry side effect một cách mù quáng.** Tool execution phải có idempotency.
7. **Không dùng một timeout cho mọi model.** Reasoning model và fast model có latency profile khác nhau.
8. **Health-aware routing tốt hơn static fallback.** Runtime phải biết provider/model nào đang có vấn đề.
9. **Backpressure trước retry.** Khi provider quá tải, giảm request mới thay vì tạo retry storm.
10. **Observability là một phần của correctness.** Không thể sửa reliability nếu không biết run chết ở stage nào.

---

# 37. Ưu tiên thực hiện

### P0 — bắt buộc

- [x] Durable run state machine. (2026-08-16)
- [x] Heartbeat + stale-run recovery. (2026-08-16)
- [x] Error taxonomy. (phase-02, 2026-08-15)
- [x] Retry/Retry-After/rate-limit coordinator. (phase-03, 2026-08-15)
- [x] Circuit breaker. (phase-03, 2026-08-15)
- [x] Stream timeout/watchdog. (phase-04, 2026-08-16)
- [x] WS reconnect/resync. (`runs.events` + `afterSeq`, 2026-08-16)
- [x] Provider/model health. (phase-03, 2026-08-15)
- [ ] Regression tests cho 429 + stream disconnect + false error. (deferred to future integration)
- [x] Premature-completion gate. (phase-05, 2026-08-17)

### P1 — rất nên làm

- Model fallback.
- Completion verifier.
- Premature completion detector.
- Tool-call repair.
- Loop detector.
- Tool idempotency.
- Checkpoint/resume.

### P2 — nâng chất lượng

- Adaptive timeout theo model.
- Intelligent context compression.
- Reliability-aware routing theo cost/latency/quality.
- Chaos/load testing framework.
- Advanced dashboards.

---

# 38. Kỳ vọng sau khi hoàn thành

GoClaw không cần bắt model yếu trở thành model mạnh. Mục tiêu là:

```text
Model mạnh
→ chạy tốt

Model trung bình
→ vẫn hoàn thành phần lớn task

Model yếu
→ có thể sai nhiều hơn về chất lượng,
   nhưng runtime không dễ crash/fail giả,
   biết retry / repair / continue / fallback,
   và không bỏ task giữa chừng một cách im lặng.
```

Đây là khác biệt giữa một chatbot gọi LLM API và một **production-grade agent runtime**.
