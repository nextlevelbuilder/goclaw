package telegram

import (
	"os"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestOutboundMediaMaxBytes_OfficialAPIUsesUploadLimit(t *testing.T) {
	ch := &Channel{config: config.TelegramConfig{MediaMaxBytes: defaultMediaMaxBytes}}

	if got := ch.outboundMediaMaxBytes(); got != officialAPIOutboundMaxBytes {
		t.Fatalf("outboundMediaMaxBytes() = %d, want %d", got, officialAPIOutboundMaxBytes)
	}
}

func TestValidateOutboundMediaSize_AllowsVideoBelowOfficialUploadLimit(t *testing.T) {
	ch := &Channel{config: config.TelegramConfig{MediaMaxBytes: defaultMediaMaxBytes}}
	path := sparseFile(t, 43_500_000)

	if err := ch.validateOutboundMediaSize(path); err != nil {
		t.Fatalf("validateOutboundMediaSize() returned error for 43.5 MB file: %v", err)
	}
}

func TestValidateOutboundMediaSize_RejectsOfficialAPIUploadAboveLimit(t *testing.T) {
	ch := &Channel{config: config.TelegramConfig{MediaMaxBytes: 100 * 1024 * 1024}}
	path := sparseFile(t, officialAPIOutboundMaxBytes+1)

	err := ch.validateOutboundMediaSize(path)
	if err == nil {
		t.Fatal("validateOutboundMediaSize() returned nil, want size error")
	}
	if !strings.Contains(err.Error(), "outbound media too large for Telegram upload") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateOutboundMediaSize_LocalAPIServerAllowsLargerUploads(t *testing.T) {
	ch := &Channel{config: config.TelegramConfig{APIServer: "http://127.0.0.1:8081"}}
	path := sparseFile(t, 150*1024*1024)

	if err := ch.validateOutboundMediaSize(path); err != nil {
		t.Fatalf("validateOutboundMediaSize() returned error for local API upload: %v", err)
	}
}

func sparseFile(t *testing.T, size int64) string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "telegram-media-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.Truncate(path, size); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	return path
}
