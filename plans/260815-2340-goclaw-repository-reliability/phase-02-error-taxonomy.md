# Phase 02 — Error Taxonomy thống nhất

> **Status: ✅ DONE** — `internal/reliability/errors.go` + tests, classification precedence đã fix (Review Fixes #3).

## Context

Hiện tại lỗi là các struct rời (`HTTPError` trong `providers/retry.go`, `FailoverSummaryError` trong `failover.go`, `tools.Result`), mỗi nơi tự định nghĩa ý nghĩa. Chưa có một chuẩn chung phân biệt: **retryable vs permanent**, **provider/runtime/tool/model**, **severity hiển thị cho user**.

## Requirements

Tạo package `internal/reliability` cung cấp:

```go
type ErrorCode string   // ví dụ: "provider.rate_limited", "model.malformed_tool_call", "runtime.stalled", "tool.timeout"

type ReliabilityError struct {
    Code        ErrorCode
    Message     string
    Retryable   bool
    Severity    Severity     // info / warning / error / fatal
    Cause       string
    RetryAfter  time.Duration // khi code là rate_limited
    RunID       string        // gắn khi chạy trong run
    Attempt     int
    Stage       string
}

type Severity int
const ( SeverityInfo; SeverityWarning; SeverityError; SeverityFatal )

func (e *ReliabilityError) Error() string
func (e *ReliabilityError) IsRetryable() bool
func (e *ReliabilityError) Unwrap() error
func (e *ReliabilityError) WithRunContext(runID, stage string, attempt int) *ReliabilityError

func Classify(err error) *ReliabilityError      // map mọi error về chuẩn
func IsRetryable(err error) bool                // tiện dụng
```

**Map phân loại** (khớp với `DefaultClassifier` hiện có + mở rộng):
- provider.rate_limited (429 có Retry-After) → retryable, severity warning
- provider.overloaded (529) → retryable
- provider.server_error (5xx) → retryable
- provider.timeout → retryable
- provider.auth / provider.bad_request / provider.model_not_found → permanent
- model.malformed_tool_call / model.invalid_json / model.empty_output → retryable với repair
- tool.timeout / tool.transient → retryable
- tool.permission_denied / tool.permanent → permanent
- run.cancelled / run.stalled / runtime.not_ready → không retry (hoặc điều hướng khác)

## Files to create/modify

- Mới `internal/reliability/errors.go` (types + classify + constructors cho từng nhóm)
- Mới `internal/reliability/errors_test.go`
- (Không sửa các provider hiện tại phase này — taxonomy chỉ là nền; wiring vào provider/agent ở phase sau nếu cần thiết, phạm vi này ưu tiên nền cho circuit breaker/health)

## Implementation Steps

1. Tạo package với các type.
2. Viết `Classify` map dựa trên `error_classify.go` semantics.
3. Unit tests cho từng class.
4. Build/test bằng Docker `golang:1.22-alpine` (Go workspace module path `github.com/nextlevelbuilder/goclaw`).

## Tests / Validation

- `go test ./internal/reliability/...` pass.
- Test: 429+RetryAfter → rate_limited+retryable; 401 → auth permanent; context deadline → timeout retryable; tools.Result IsError → tool error.

## Risks / Rollback

- Không chạm public contracts hiện có → risk thấp. Package mới, ko nối dây → không phá vỡ build.
- Rollback: xóa package.