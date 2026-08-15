package personal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// authenticate resolves credentials and returns an authenticated session.
// Priority: preloaded (DB) > saved file > QR login.
func (c *Channel) authenticate(ctx context.Context) (*protocol.Session, error) {
	sess := protocol.NewSession()

	// 1. Preloaded credentials (from DB via factory).
	if c.preloadedCreds != nil {
		slog.Info("zalo_personal: attempting login with preloaded credentials")
		if err := protocol.LoginWithCredentials(ctx, sess, *c.preloadedCreds); err != nil {
			return nil, fmt.Errorf("preloaded credentials failed: %w", err)
		}
		return sess, nil
	}

	// 2. Saved file credentials (config-based).
	credPath := c.resolveCredentialsPath()
	if cred := loadCredentials(credPath); cred != nil {
		slog.Info("zalo_personal: attempting login with saved credentials", "path", credPath)
		if err := protocol.LoginWithCredentials(ctx, sess, *cred); err != nil {
			slog.Warn("zalo_personal: saved credentials failed, falling back to QR", "error", err)
		} else {
			return sess, nil
		}
	}

	// 3. QR login (interactive).
	slog.Info("zalo_personal: starting QR login. Scan the QR code with your Zalo app.")
	cred, err := protocol.LoginQR(ctx, sess, func(qrPNG []byte) {
		slog.Info("zalo_personal: QR code generated. Scan with Zalo app.", "size", len(qrPNG))
	})
	if err != nil {
		return nil, fmt.Errorf("QR login failed: %w", err)
	}

	// Save credentials for future re-login.
	if err := saveCredentials(credPath, cred); err != nil {
		slog.Warn("zalo_personal: failed to save credentials", "error", err, "path", credPath)
	} else {
		slog.Info("zalo_personal: credentials saved", "path", credPath)
	}

	return sess, nil
}

// SetPreloadedCredentials sets credentials from DB factory.
func (c *Channel) SetPreloadedCredentials(cred *protocol.Credentials) {
	c.preloadedCreds = cred
}

func (c *Channel) resolveCredentialsPath() string {
	if c.config.CredentialsPath != "" {
		return config.ExpandHome(c.config.CredentialsPath)
	}
	return filepath.Join(config.ResolvedDataDirFromEnv(), "zalo-personal-credentials.json")
}

func loadCredentials(path string) *protocol.Credentials {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cred protocol.Credentials
	if err := json.Unmarshal(data, &cred); err != nil {
		slog.Warn("zalo_personal: invalid credentials file", "path", path, "error", err)
		return nil
	}
	if !cred.IsValid() {
		return nil
	}
	return &cred
}

func saveCredentials(path string, cred *protocol.Credentials) error {
	if cred == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
