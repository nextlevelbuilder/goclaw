package oa

// Known Zalo OA error codes. The access-token-invalid family is returned
// with inconsistent sign + magnitude (216, -216, 401, -401) for the same
// cause; all four are treated identically.
//
// Sources:
//   - Social API reference: docs/zalo-error-codes.md (auto-scraped)
//   - OA OpenAPI negative codes (-216, -118, -201, -210, -14003) are
//     production-observed and not documented on the public reference page.
const (
	// Access token invalid/expired → ForceRefresh + one retry.
	codeAccessTokenInvalid216Neg = -216
	codeAccessTokenInvalid216Pos = 216
	codeAccessTokenInvalid401Neg = -401
	codeAccessTokenInvalid401Pos = 401

	// Refresh token dead → operator must re-consent.
	codeInvalidGrant = -118

	// Payload shape rejected (e.g. send endpoint requires template/media
	// shape for images instead of plain attachment_id).
	codeParamsInvalid = -201

	// Upload body exceeds the endpoint cap (image 1MB, file 5MB, gif 5MB).
	codeFileSizeExceeded = -210

	// OAuth: redirect_uri does not match the one registered on Zalo console.
	codeInvalidRedirectURI = -14003
)

// isAccessTokenInvalid reports whether code is in the access-token
// invalid/expired family.
func isAccessTokenInvalid(code int) bool {
	switch code {
	case codeAccessTokenInvalid216Neg, codeAccessTokenInvalid216Pos,
		codeAccessTokenInvalid401Neg, codeAccessTokenInvalid401Pos:
		return true
	}
	return false
}

// Family classifies a Zalo error so the LLM and the channel UI can react
// appropriately. Unknown codes return FamilyUnknown and the catalog falls
// through — the legacy "code N: message" string is still surfaced.
type Family string

const (
	FamilyUnknown    Family = ""
	FamilyAuth       Family = "auth"       // token invalid / refresh dead
	FamilyPermission Family = "permission" // scope, opt-in, 48h window
	FamilyPayload    Family = "payload"    // shape, template, syntax
	FamilySize       Family = "size"       // file/image/gif over cap
	FamilyRate       Family = "rate"       // per-app or per-user quota
	FamilyServer     Family = "server"     // 5xx-equivalent / temporary
	FamilyConfig     Family = "config"     // operator-side misconfig (OAuth)
)

// CodeInfo is what Classify returns. Empty fields mean "use default surfacing".
//
// LLMHint is a single short English sentence the agent reads in a tool result;
// it should describe the cause and the corrective action without leaking the
// raw numeric code (the code is appended separately by APIError.Error()).
//
// OpReason is the i18n key used when MarkFailed shows a reason in the UI.
// One key may serve multiple codes (e.g. all auth codes share MsgZaloOAErrAuth).
type CodeInfo struct {
	Family    Family
	Retryable bool
	LLMHint   string
	OpReason  string
}

// catalog maps a Zalo error code to its classification. Only curated codes
// belong here — anything not listed falls through as FamilyUnknown.
var catalog = map[int]CodeInfo{
	// Auth — access token invalid/expired (4 sign/magnitude variants).
	codeAccessTokenInvalid216Neg: authTokenInfo,
	codeAccessTokenInvalid216Pos: authTokenInfo,
	codeAccessTokenInvalid401Neg: authTokenInfo,
	codeAccessTokenInvalid401Pos: authTokenInfo,

	// Auth — refresh token dead, operator must re-consent.
	codeInvalidGrant: {
		Family:    FamilyAuth,
		Retryable: false,
		LLMHint:   "Zalo refresh token has expired; the operator must re-authorize the OA before sending will resume.",
		OpReason:  "MsgZaloOAErrRefreshExpired",
	},

	// Payload — shape/template/syntax rejected.
	codeParamsInvalid: payloadInfo,
	100:               payloadInfo, // Invalid parameter
	2500:              payloadInfo, // Syntax error

	// Size — body over the per-endpoint cap.
	codeFileSizeExceeded: {
		Family:    FamilySize,
		Retryable: false,
		LLMHint:   "Attachment exceeds the Zalo cap (image 1MB, file 5MB, gif 5MB); recompress or resize before retrying.",
		OpReason:  "MsgZaloOAErrSize",
	},

	// Permission — extended scope required.
	289: {
		Family:    FamilyPermission,
		Retryable: false,
		LLMHint:   "The OA app is missing an extended permission required for this call; the operator must grant the additional scope.",
		OpReason:  "MsgZaloOAErrPermission",
	},

	// Permission — interaction window / opt-in (Zalo's user-must-have-spoken-recently rule).
	12007: interactionWindowInfo, // user inactive 30+ days
	12008: interactionWindowInfo, // recipient hit per-window receive quota
	12009: interactionWindowInfo, // sender and recipient not friends

	// Permission — user not visible / app disabled.
	210: {
		Family:    FamilyPermission,
		Retryable: false,
		LLMHint:   "The target user is not visible to this OA (not opted-in or has hidden their profile); skip and inform the caller.",
		OpReason:  "MsgZaloOAErrUserNotVisible",
	},
	11004: {
		Family:    FamilyPermission,
		Retryable: false,
		LLMHint:   "The Zalo app is disabled or banned; the operator must contact Zalo support before any send will succeed.",
		OpReason:  "MsgZaloOAErrAppDisabled",
	},

	// Rate — quota exhausted (app- or user-scoped). Retry only after the
	// quota window resets; the agent loop should not loop on this.
	12000: rateInfo, // app-wide quota
	12002: rateInfo, // daily quota
	12003: rateInfo, // weekly quota
	12004: rateInfo, // monthly quota
	12010: rateInfo, // per-user daily quota

	// Server — generic call failure / unknown exception. Safe to retry once
	// at a higher layer; treat as transient.
	10000: serverInfo,
	10002: serverInfo,

	// Config — OAuth misconfig (redirect_uri mismatch).
	codeInvalidRedirectURI: {
		Family:    FamilyConfig,
		Retryable: false,
		LLMHint:   "Zalo rejected the OAuth redirect_uri; the operator must update the redirect URI in the Zalo console to match the channel config.",
		OpReason:  "MsgZaloOAErrRedirectURI",
	},
}

// Shared CodeInfo values reused by multiple codes — declared at file scope
// so the catalog map stays a literal (no init() side-effects).
var (
	authTokenInfo = CodeInfo{
		Family:    FamilyAuth,
		Retryable: true, // one retry after ForceRefresh — handled in send.go/poll.go
		LLMHint:   "Zalo access token was rejected; the channel will refresh and retry once automatically.",
		OpReason:  "MsgZaloOAErrAuth",
	}
	payloadInfo = CodeInfo{
		Family:    FamilyPayload,
		Retryable: false,
		LLMHint:   "Zalo rejected the request payload; verify the message shape (template vs. plain), required fields, and recipient ID format before retrying.",
		OpReason:  "MsgZaloOAErrPayload",
	}
	interactionWindowInfo = CodeInfo{
		Family:    FamilyPermission,
		Retryable: false,
		LLMHint:   "Zalo only allows messaging users who have interacted with the OA recently; the recipient is outside that window. Wait for the user to message first or use a paid template.",
		OpReason:  "MsgZaloOAErrInteractionWindow",
	}
	rateInfo = CodeInfo{
		Family:    FamilyRate,
		Retryable: false, // not within this request — wait for quota reset
		LLMHint:   "Zalo quota for this OA or user has been exhausted; wait for the quota window to reset before retrying.",
		OpReason:  "MsgZaloOAErrRate",
	}
	serverInfo = CodeInfo{
		Family:    FamilyServer,
		Retryable: true,
		LLMHint:   "Zalo returned a temporary server error; retrying after a short backoff is safe.",
		OpReason:  "MsgZaloOAErrServer",
	}
)

// Classify returns the CodeInfo for the given Zalo error code. Unknown codes
// return CodeInfo{Family: FamilyUnknown}.
func Classify(code int) CodeInfo {
	return catalog[code]
}
