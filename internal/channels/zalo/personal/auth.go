package personal

import (
	"context"
	"encoding/json"
	"errors"
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
	if c.cookiePersist != nil {
		installCookieObserver(sess, c.cookiePersist.MarkDirty)
	}

	// 1. Preloaded credentials (from DB via factory).
	if c.preloadedCreds != nil {
		slog.Info("zalo_personal: attempting login with preloaded credentials")
		if err := protocol.LoginWithCredentials(ctx, sess, *c.preloadedCreds); err != nil {
			// DB-backed credentials have no file/QR fallback by design. Treat a
			// failed preloaded login as terminal auth so the listener loop does
			// not restart ten times with the same rejected cookies.
			return nil, fmt.Errorf("preloaded credentials failed: %w", errors.Join(protocol.ErrAuthExpired, err))
		}
		c.bindCookieSession(sess)
		return sess, nil
	}

	// 2. Saved file credentials (config-based).
	credPath := c.resolveCredentialsPath()
	if cred := loadCredentials(credPath); cred != nil {
		slog.Info("zalo_personal: attempting login with saved credentials", "path", credPath)
		if err := protocol.LoginWithCredentials(ctx, sess, *cred); err != nil {
			slog.Warn("zalo_personal: saved credentials failed, falling back to QR", "error", err)
		} else {
			c.bindCookieSession(sess)
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

	c.bindCookieSession(sess)
	return sess, nil
}

func (c *Channel) bindCookieSession(sess *protocol.Session) {
	if c.cookiePersist == nil {
		return
	}
	c.cookiePersist.Bind(func() *protocol.Credentials {
		return snapshotCredentials(sess)
	})
}

// SetPreloadedCredentials sets credentials from DB factory.
func (c *Channel) SetPreloadedCredentials(cred *protocol.Credentials) {
	c.preloadedCreds = cred
}

// SetCredentialSaver overrides the file fallback with the channel-instance DB
// writer. The loader calls this after SetTenantID so rotated cookies land in
// the same encrypted row the instance was loaded from.
func (c *Channel) SetCredentialSaver(save func(context.Context, []byte) error) {
	if c.cookiePersist == nil || save == nil {
		return
	}
	c.cookiePersist.SetSave(func(ctx context.Context, cred *protocol.Credentials) error {
		blob, err := marshalCredentials(cred)
		if err != nil {
			return err
		}
		return save(ctx, blob)
	})
}

// marshalCredentials serializes the credential JSON shape stored in
// channel_instances (same shape as QR login).
func marshalCredentials(cred *protocol.Credentials) ([]byte, error) {
	if cred == nil {
		return nil, nil
	}
	return json.Marshal(zaloCreds{
		IMEI:      cred.IMEI,
		Cookie:    cred.Cookie,
		UserAgent: cred.UserAgent,
		Language:  cred.Language,
	})
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
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".zalo-creds-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
