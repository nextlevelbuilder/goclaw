// Package clisession holds a long-lived, bidirectional conversation with a
// coding CLI running inside the sandbox: the user's messages go in on stdin,
// the CLI's narration and tool activity stream out on stdout, and every tool it
// wants to run can be gated on the user's approval.
//
// The package is deliberately dependency-light — sandbox + cliagent + stdlib,
// no store, no tools, no gateway. Obtaining the sandbox, applying credentials
// and deciding who may approve what are all the CALLER's job, passed in via
// SessionOpts. That is what makes the session logic unit-testable against a
// fake InteractiveSession with no Docker daemon in sight.
//
// ---------------------------------------------------------------------------
// protocol.go is THE ONE PLACE to correct if the CLI's wire format changes.
// ---------------------------------------------------------------------------
//
// Every JSON shape of the stdio protocol lives in this file and nowhere else.
// Each type below is derived from Claude Code's streaming (`--input-format
// stream-json` / `--output-format stream-json`) control protocol, and each says
// whether it was CONFIRMED or INFERRED.
//
// Provenance of the shapes here: they were read out of the shipped Claude Code
// binary's embedded Zod schemas (version 2.1.220, macOS arm64 native build) and
// then EXERCISED END-TO-END against that binary on 2026-07-29 — a real session
// was driven over stdin/stdout and observed to (a) emit a `can_use_tool`
// control_request, (b) honour an allow decision including a rewritten
// `updatedInput`, and (c) honour a deny decision by turning our message into the
// tool's is_error result. Anything NOT covered by that exercise is marked
// INFERRED below.
//
// Tolerance is a hard requirement, not politeness: a future CLI release may add
// fields, rename optional ones, or emit line types we have never seen. So every
// decoder here ignores unknown fields, treats every optional field as
// genuinely optional, and reports "not for me" rather than failing — a line
// this file cannot make sense of must never take the session down.
package clisession

import (
	"encoding/json"
	"strings"
)

// Line "type" discriminators on the stdio protocol.
//
// CONFIRMED (observed on the wire): "control_request", "control_response",
// "user", "assistant", "stream_event", "system", "result", "rate_limit_event".
// Only the two control ones matter here — everything else is narration and is
// handed to cliagent's parser untouched.
const (
	typeControlRequest  = "control_request"
	typeControlResponse = "control_response"
	typeUser            = "user"
)

// Control-request subtypes the CLI may ask us to handle. CONFIRMED to include
// "can_use_tool"; the CLI also uses "request_user_dialog", "hook_callback",
// "mcp_message", "elicitation" and others, which we deliberately do NOT claim to
// support (see replyUnsupported).
const subtypeCanUseTool = "can_use_tool"

// control_response subtypes. CONFIRMED: "success" carries a handler result,
// "error" says the handler itself failed.
//
// NOTE the distinction that is easy to get wrong: a DENIED tool is still a
// "success" — the handler ran and its answer was "no". "error" means we could
// not produce an answer at all, and the CLI surfaces it differently.
const (
	respSuccess = "success"
	respError   = "error"
)

// Permission verdicts inside a can_use_tool response. CONFIRMED.
const (
	behaviorAllow = "allow"
	behaviorDeny  = "deny"
)

// ---------------------------------------------------------------------------
// Inbound: what the CLI writes on stdout
// ---------------------------------------------------------------------------

// inboundLine is the minimal envelope every stdout line shares. We decode ONLY
// the discriminator here and re-decode the interesting lines into their specific
// type, so an unrecognised line costs one cheap unmarshal and is then handed to
// the narration parser unchanged.
//
// CONFIRMED: `type` is present on every line of the stream.
type inboundLine struct {
	Type string `json:"type"`
}

// controlRequest is a request from the CLI that expects exactly one
// control_response carrying the same RequestID.
//
// CONFIRMED shape, from the binary's schema and observed verbatim on the wire:
//
//	{"type":"control_request","request_id":"<uuid>","request":{"subtype":"can_use_tool", …}}
type controlRequest struct {
	Type      string             `json:"type"`
	RequestID string             `json:"request_id"`
	Request   controlRequestBody `json:"request"`
}

// controlRequestBody is the payload of a control_request. The fields below are
// the can_use_tool ones; other subtypes put different fields here, which is
// harmless because unknown JSON fields are ignored and we only act on
// subtypeCanUseTool.
//
// CONFIRMED (all observed on a live can_use_tool line):
//
//	subtype, tool_name, display_name, input, description,
//	permission_suggestions, decision_reason_type, tool_use_id
//
// Also CONFIRMED present in the binary's schema but not exercised here:
// blocked_path, decision_reason, matched_ask_rule, classifier_approvable,
// agent_id, suppress_always_allow_rule, requires_user_interaction. Only the ones
// this package actually uses are decoded; the rest are intentionally dropped so
// there is no half-supported surface to maintain.
type controlRequestBody struct {
	Subtype  string          `json:"subtype"`
	ToolName string          `json:"tool_name"`
	Input    json.RawMessage `json:"input"`

	// DisplayName / Description are the CLI's own human-readable labels for the
	// tool and the pending action. They exist so an approval prompt can say
	// "Bash — Fetch example.com" instead of dumping raw JSON at the user.
	DisplayName string `json:"display_name"`
	Description string `json:"description"`

	// ToolUseID ties the ask to the tool_use block in the assistant message, so
	// an approval prompt can be rendered against the tool chip already on screen.
	ToolUseID string `json:"tool_use_id"`
}

// ---------------------------------------------------------------------------
// Outbound: what we write on stdin
// ---------------------------------------------------------------------------

// userMessage is one user turn.
//
// CONFIRMED (accepted by the live binary, and matching its schema):
//
//	{"type":"user","message":{"role":"user","content":"…"},"parent_tool_use_id":null}
//
// ParentToolUseID is NOT omitempty on purpose. The CLI's schema declares it
// NULLABLE-but-present (`.nullable()`, not `.optional()`), so the field has to
// be on the wire as an explicit null; dropping it risks a schema rejection on a
// build that validates input strictly.
type userMessage struct {
	Type            string          `json:"type"`
	Message         userMessageBody `json:"message"`
	ParentToolUseID *string         `json:"parent_tool_use_id"`
}

// userMessageBody is an Anthropic Messages API message param. A plain string
// content is CONFIRMED to work; the block-array form is equally valid on the API
// but we have no need for it, so we do not offer a half-tested version of it.
type userMessageBody struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// newUserMessage builds one user turn.
func newUserMessage(text string) userMessage {
	return userMessage{
		Type:            typeUser,
		Message:         userMessageBody{Role: "user", Content: text},
		ParentToolUseID: nil,
	}
}

// controlResponse answers a control_request.
//
// CONFIRMED (accepted by the live binary, for both allow and deny):
//
//	{"type":"control_response","response":{"subtype":"success","request_id":"…","response":{…}}}
//	{"type":"control_response","response":{"subtype":"error","request_id":"…","error":"…"}}
//
// Note the doubly-nested `response`: the outer one is the envelope, the inner
// one is the handler's return value. Getting that nesting wrong is the most
// likely way to break this file, which is why both levels are named types.
type controlResponse struct {
	Type     string              `json:"type"`
	Response controlResponseBody `json:"response"`
}

type controlResponseBody struct {
	Subtype   string `json:"subtype"`
	RequestID string `json:"request_id"`
	// Response carries the handler's result on success. Typed as the permission
	// result because can_use_tool is the only subtype this package answers.
	Response *permissionResult `json:"response,omitempty"`
	// Error is set instead when Subtype is respError.
	Error string `json:"error,omitempty"`
}

// permissionResult is the can_use_tool handler's verdict.
//
// READ FROM THE SHIPPED BINARY (2.1.197, the version in the sandbox image). The
// control-protocol schema is `$rn = union([S1m, E1m])`:
//
//	S1m: {behavior:"allow", updatedInput:record(string,unknown),  ← REQUIRED
//	      updatedPermissions?, toolUseID?, decisionClassification?}
//	E1m: {behavior:"deny",  message:string,                        ← REQUIRED
//	      interrupt?, toolUseID?, decisionClassification?}
//
// `updatedInput` being REQUIRED on the allow branch is the trap. There is a
// second, hook-shaped schema in the same binary where it IS optional, and an
// earlier version of this file was written against that one — so every allow we
// sent omitted the field, failed validation, and the CLI turned it into
// "Tool permission request failed: …", which reached the model as a permissions
// error while the user saw their Approve click succeed. newAllowResponse
// therefore ALWAYS sends updatedInput, echoing the tool's original input when the
// approver did not rewrite it (which is what the CLI's own internal handlers do:
// `checkPermissions: e => ({behavior:"allow", updatedInput:e})`).
//
// `decisionClassification` ("user_temporary"|"user_permanent"|"user_reject") is
// deliberately NOT sent. It is optional and the CLI infers conservatively without
// it; sending "user_permanent" for an approve-always would ask the CLI to persist
// a rule INSIDE the sandbox, which is the same overreach `updatedPermissions`
// is refused for below.
//
// `updatedPermissions` is deliberately NOT modelled: it would let the CLI
// persist an always-allow rule inside the sandbox, which is a policy decision
// that belongs to the platform's own approval records, not to a container that
// gets recycled. Adding it here would silently widen what a single "allow"
// means.
type permissionResult struct {
	Behavior string `json:"behavior"`
	// UpdatedInput is the input the CLI will actually run. Required by the schema
	// on the allow branch, so it carries NO omitempty — an absent field fails
	// validation and the tool call dies with a permissions error. It also lets the
	// approver hand back a MODIFIED input: a Bash command replaced here is the one
	// that executes.
	UpdatedInput json.RawMessage `json:"updatedInput,omitempty"`
	// Message is the reason shown to the model as the tool's error result.
	Message string `json:"message,omitempty"`
}

// newAllowResponse builds the control_response for an approved tool call.
//
// updatedInput is the approver's rewrite and may be nil, in which case the tool's
// ORIGINAL input is echoed back — the field is required, and echoing is how "run
// it as proposed" is expressed. A malformed rewrite is ignored in favour of the
// original rather than sent: the CLI validates it against the tool's schema and
// would turn a bad one into a denial, the opposite of what the approver decided.
//
// Returns ok=false when neither input is a JSON object, because the only ways to
// respond then are both wrong: omitting the field fails validation, and sending
// {} would run the tool with NO arguments — a silent misfire the user would read
// as "it approved and did something else". The caller denies instead.
func newAllowResponse(requestID string, updatedInput, originalInput json.RawMessage) (controlResponse, bool) {
	res := &permissionResult{Behavior: behaviorAllow}
	switch {
	case isJSONObject(updatedInput):
		res.UpdatedInput = updatedInput
	case isJSONObject(originalInput):
		res.UpdatedInput = originalInput
	default:
		return controlResponse{}, false
	}
	return controlResponse{
		Type: typeControlResponse,
		Response: controlResponseBody{
			Subtype:   respSuccess,
			RequestID: requestID,
			Response:  res,
		},
	}, true
}

// newDenyResponse builds the control_response for a refused tool call. reason is
// what the model is told, so an empty one is replaced with something honest
// rather than leaving the model to guess why it was blocked.
func newDenyResponse(requestID, reason string) controlResponse {
	if strings.TrimSpace(reason) == "" {
		reason = "The user did not approve this action."
	}
	return controlResponse{
		Type: typeControlResponse,
		Response: controlResponseBody{
			Subtype:   respSuccess,
			RequestID: requestID,
			Response:  &permissionResult{Behavior: behaviorDeny, Message: reason},
		},
	}
}

// newErrorResponse reports that we could not handle a control_request at all —
// used for subtypes this package does not implement. It is NOT the way to say
// "no": that is newDenyResponse. Answering with an error still unblocks the CLI,
// which is the whole point; leaving a control_request unanswered would wedge the
// session waiting on us forever.
func newErrorResponse(requestID, msg string) controlResponse {
	return controlResponse{
		Type: typeControlResponse,
		Response: controlResponseBody{
			Subtype:   respError,
			RequestID: requestID,
			Error:     msg,
		},
	}
}

// ---------------------------------------------------------------------------
// Decoding
// ---------------------------------------------------------------------------

// parsedLine is the result of classifying one stdout line.
type parsedLine struct {
	// Control is set when the line is a control_request that needs an answer.
	Control *controlRequest
	// Narration is true when the line should go to cliagent's event parser.
	// Control lines are NOT narration: they are protocol traffic, and feeding
	// them to the parser would render internal plumbing as chat text.
	Narration bool
}

// classifyLine decides what one stdout line is.
//
// It never returns an error. A line that is not JSON, is JSON but not an
// object, or is an object we don't recognise, is simply routed to the narration
// parser (which itself ignores what it cannot use). That is the tolerance
// requirement: garbage on stdout — a CLI warning, a progress bar, a truncated
// line — must cost us nothing more than a skipped line.
func classifyLine(line string) parsedLine {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return parsedLine{}
	}
	// Cheap gate: only a JSON object can be a control frame. Everything else is
	// narration by definition, so don't pay for an unmarshal attempt.
	if trimmed[0] != '{' {
		return parsedLine{Narration: true}
	}

	var env inboundLine
	if json.Unmarshal([]byte(trimmed), &env) != nil {
		// Malformed JSON. Hand it to the narration parser, which will also reject
		// it — the point is that neither path treats it as fatal.
		return parsedLine{Narration: true}
	}

	switch env.Type {
	case typeControlRequest:
		var req controlRequest
		if json.Unmarshal([]byte(trimmed), &req) != nil || req.RequestID == "" {
			// Shaped like a control_request but unusable — we cannot answer a request
			// with no id, and guessing one would produce a response the CLI drops.
			// Don't narrate protocol plumbing either; just skip it.
			return parsedLine{}
		}
		return parsedLine{Control: &req}
	case typeControlResponse:
		// An answer to a request WE sent. This package sends no control_requests,
		// so this is either an echo or a newer CLI's traffic — not narration.
		return parsedLine{}
	default:
		return parsedLine{Narration: true}
	}
}

// isJSONObject reports whether raw is a non-empty JSON object, which is what
// the CLI requires of a tool input.
func isJSONObject(raw json.RawMessage) bool {
	t := strings.TrimSpace(string(raw))
	if len(t) < 2 || t[0] != '{' {
		return false
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal([]byte(t), &probe) != nil {
		return false
	}
	return len(probe) > 0
}

// encodeLine marshals one outbound frame to a single line. Marshalling these
// structs cannot fail (no channels, funcs or NaNs), and the one field that
// carries caller-supplied JSON — permissionResult.UpdatedInput — is gated by
// isJSONObject before it gets here, so a marshal error is unreachable rather
// than merely unlikely.
func encodeLine(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
