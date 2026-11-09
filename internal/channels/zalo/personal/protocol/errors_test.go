package protocol

import (
	"errors"
	"testing"
)

func TestClassifyCode(t *testing.T) {
	cases := []struct {
		code int
		want error
	}{
		{401, ErrAuthExpired},
		{3000, ErrAuthExpired},
		{400, ErrPolicy},
		{403, ErrPolicy},
		{-201, ErrPolicy},
		{408, ErrTransient},
		{429, ErrTransient},
		{500, ErrTransient},
		{503, ErrTransient},
		{9999, ErrTransient},
	}
	for _, tc := range cases {
		err := NewAPIError("send", tc.code)
		if !errors.Is(err, tc.want) {
			t.Errorf("code %d classified as %v, want %v", tc.code, err, tc.want)
		}
	}
}

func TestAPIErrorRetryable(t *testing.T) {
	if !NewAPIError("send", 500).Retryable() {
		t.Fatal("500 should be retryable")
	}
	if NewAPIError("send", 403).Retryable() {
		t.Fatal("403 should not be retryable")
	}
	if NewAPIError("send", 401).Retryable() {
		t.Fatal("401 should not be retryable")
	}
}

func TestParseSendResult(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		wantID  string
		wantErr error
	}{
		{"flat msgId", `{"msgId":123}`, "123", nil},
		{"nested msgId", `{"error_code":0,"data":{"msgId":456}}`, "456", nil},
		{"inner error", `{"error_code":-201}`, "", ErrPolicy},
		{"inner transient", `{"error_code":503}`, "", ErrTransient},
		{"empty", `{}`, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSendResult([]byte(tc.payload))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if got != "" {
					t.Fatalf("msgId = %q on error, want empty", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got != tc.wantID {
				t.Fatalf("msgId = %q, want %q", got, tc.wantID)
			}
		})
	}
}
