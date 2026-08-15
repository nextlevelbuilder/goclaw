package telegram

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestShouldRetryTelegramStartupWithIPv4ForPostConnectNetworkError(t *testing.T) {
	err := fmt.Errorf(`telego: getMe: internal execution: request call: http do request: Post "https://api.telegram.org/bot123:ABC/getMe": read tcp [2407::1]:39268->[2001:67c:4e8:f004::9]:443: read: connection reset by peer`)

	if !shouldRetryTelegramStartupWithIPv4(err) {
		t.Fatal("expected IPv4 startup retry for connection reset after IPv6 connect")
	}
}

func TestShouldRetryTelegramStartupWithIPv4SkipsDNSFailures(t *testing.T) {
	err := &net.DNSError{Err: "no such host", Name: "api.telegram.org", IsNotFound: true}

	if shouldRetryTelegramStartupWithIPv4(err) {
		t.Fatal("expected DNS failures to avoid IPv4 retry")
	}
}

func TestShouldRetryTelegramStartupWithIPv4SkipsAuthFailures(t *testing.T) {
	err := errors.New(`telego: getMe: api: 401 "Unauthorized"`)

	if shouldRetryTelegramStartupWithIPv4(err) {
		t.Fatal("expected auth failures to avoid IPv4 retry")
	}
}

func TestSanitizeTelegramErrorRedactsTokenAndPreservesCause(t *testing.T) {
	token := "123456:ABC"
	raw := fmt.Errorf(`Post "https://api.telegram.org/bot%s/getMe": %w`, token, context.DeadlineExceeded)
	err := sanitizeTelegramError(raw, token)

	if strings.Contains(err.Error(), token) {
		t.Fatalf("expected token to be redacted, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "<telegram-token>") {
		t.Fatalf("expected redaction marker, got %q", err.Error())
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("expected sanitized error to preserve wrapped cause")
	}
}
