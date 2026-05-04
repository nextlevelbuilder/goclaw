package oa

import (
	"strings"
	"testing"
)

func TestClassify_KnownCodes(t *testing.T) {
	tests := []struct {
		code         int
		wantFamily   Family
		wantHintNon  bool // LLMHint must be non-empty
		wantOpReason string
	}{
		// Auth family — every variant of the access-token-invalid code.
		{-216, FamilyAuth, true, "MsgZaloOAErrAuth"},
		{216, FamilyAuth, true, "MsgZaloOAErrAuth"},
		{-401, FamilyAuth, true, "MsgZaloOAErrAuth"},
		{401, FamilyAuth, true, "MsgZaloOAErrAuth"},
		// Auth family — refresh token dead.
		{-118, FamilyAuth, true, "MsgZaloOAErrRefreshExpired"},
		// Payload family.
		{-201, FamilyPayload, true, "MsgZaloOAErrPayload"},
		{100, FamilyPayload, true, "MsgZaloOAErrPayload"},
		{2500, FamilyPayload, true, "MsgZaloOAErrPayload"},
		// Size family.
		{-210, FamilySize, true, "MsgZaloOAErrSize"},
		// Permission family — extended permission required.
		{289, FamilyPermission, true, "MsgZaloOAErrPermission"},
		// Permission family — user/recipient outside the messaging window.
		{12007, FamilyPermission, true, "MsgZaloOAErrInteractionWindow"},
		{12008, FamilyPermission, true, "MsgZaloOAErrInteractionWindow"},
		{12009, FamilyPermission, true, "MsgZaloOAErrInteractionWindow"},
		// Rate family — daily/weekly/monthly quotas.
		{12000, FamilyRate, true, "MsgZaloOAErrRate"},
		{12002, FamilyRate, true, "MsgZaloOAErrRate"},
		{12003, FamilyRate, true, "MsgZaloOAErrRate"},
		{12004, FamilyRate, true, "MsgZaloOAErrRate"},
		{12010, FamilyRate, true, "MsgZaloOAErrRate"},
		// Server family — generic exceptions.
		{10000, FamilyServer, true, "MsgZaloOAErrServer"},
		{10002, FamilyServer, true, "MsgZaloOAErrServer"},
		// Permission family — app disabled / user not visible.
		{210, FamilyPermission, true, "MsgZaloOAErrUserNotVisible"},
		{11004, FamilyPermission, true, "MsgZaloOAErrAppDisabled"},
		// Config family — OAuth misconfiguration.
		{-14003, FamilyConfig, true, "MsgZaloOAErrRedirectURI"},
	}

	for _, tt := range tests {
		got := Classify(tt.code)
		if got.Family != tt.wantFamily {
			t.Errorf("Classify(%d).Family = %q, want %q", tt.code, got.Family, tt.wantFamily)
		}
		if tt.wantHintNon && got.LLMHint == "" {
			t.Errorf("Classify(%d).LLMHint is empty, want non-empty", tt.code)
		}
		if got.OpReason != tt.wantOpReason {
			t.Errorf("Classify(%d).OpReason = %q, want %q", tt.code, got.OpReason, tt.wantOpReason)
		}
	}
}

func TestClassify_UnknownCode(t *testing.T) {
	got := Classify(99999)
	if got.Family != FamilyUnknown {
		t.Errorf("Classify(99999).Family = %q, want FamilyUnknown", got.Family)
	}
	if got.LLMHint != "" || got.OpReason != "" {
		t.Errorf("Classify(99999) should be zero value, got %+v", got)
	}
}

func TestAPIError_Error_AppendsHintWhenKnown(t *testing.T) {
	e := &APIError{Code: -210, Message: "file too big"}
	got := e.Error()
	if !strings.Contains(got, "-210") || !strings.Contains(got, "file too big") {
		t.Errorf("Error() must include code+message, got %q", got)
	}
	if !strings.Contains(got, "1MB") {
		t.Errorf("Error() should include the size LLMHint, got %q", got)
	}
}

func TestAPIError_Error_FallbackForUnknown(t *testing.T) {
	e := &APIError{Code: 99999, Message: "??"}
	got := e.Error()
	want := "zalo api error 99999: ??"
	if got != want {
		t.Errorf("Error() unknown-code = %q, want %q", got, want)
	}
}

func TestAPIError_Info(t *testing.T) {
	if (&APIError{Code: -210}).Info().Family != FamilySize {
		t.Errorf("Info() for -210 should be FamilySize")
	}
	if (*APIError)(nil).Info().Family != FamilyUnknown {
		t.Errorf("Info() on nil receiver should return zero CodeInfo")
	}
}

func TestIsAccessTokenInvalid_StillWorks(t *testing.T) {
	// The legacy helper must keep working — send.go and poll.go branch on it
	// directly to drive the one-shot token refresh retry.
	for _, code := range []int{-216, 216, -401, 401} {
		if !isAccessTokenInvalid(code) {
			t.Errorf("isAccessTokenInvalid(%d) = false, want true", code)
		}
	}
	for _, code := range []int{-118, -201, -210, 12000, 12009, 99999, 0} {
		if isAccessTokenInvalid(code) {
			t.Errorf("isAccessTokenInvalid(%d) = true, want false", code)
		}
	}
}
