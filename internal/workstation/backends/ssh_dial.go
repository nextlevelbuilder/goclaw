package backends

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"golang.org/x/crypto/ssh"
)

// dialSSH establishes a new *ssh.Client using the provided metadata and key material.
// Context cancellation aborts the dial; the spawned goroutine cleans up on its own.
func dialSSH(ctx context.Context, meta *store.SSHMetadata, keyMaterial []byte) (*ssh.Client, error) {
	timeout := time.Duration(meta.ConnectTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	hostKeyCB, err := buildHostKeyCallback(meta)
	if err != nil {
		return nil, err
	}

	auth, err := buildAuthMethods(meta, keyMaterial)
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            meta.User,
		Auth:            auth,
		HostKeyCallback: hostKeyCB,
		Timeout:         timeout,
	}

	addr := net.JoinHostPort(meta.Host, strconv.Itoa(meta.Port))

	type result struct {
		client *ssh.Client
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		c, e := ssh.Dial("tcp", addr, cfg)
		ch <- result{c, e}
	}()

	select {
	case r := <-ch:
		return r.client, r.err
	case <-ctx.Done():
		// Background goroutine will finish and its nascent connection will be discarded.
		return nil, ctx.Err()
	}
}

// buildHostKeyCallback returns an ssh.HostKeyCallback that enforces fingerprint pinning.
// TOFU policy: if KnownHostsFingerprint is empty, accept the key and log it so the
// operator can record it. Subsequent connects must match the pinned fingerprint.
// NOTE: InsecureIgnoreHostKey is never used — this is enforced by CI grep check.
func buildHostKeyCallback(meta *store.SSHMetadata) (ssh.HostKeyCallback, error) {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		fp := ssh.FingerprintSHA256(key)
		if meta.KnownHostsFingerprint == "" {
			slog.Info("workstation.ssh_host_key_tofu",
				"host", meta.Host,
				"fingerprint", fp,
				"hint", "persist this fingerprint to knownHostsFingerprint for security",
			)
			return nil
		}
		if fp != meta.KnownHostsFingerprint {
			slog.Warn("security.ssh_host_key_changed",
				"host", meta.Host,
				"expected", meta.KnownHostsFingerprint,
				"actual", fp,
			)
			return fmt.Errorf("host key mismatch for %s: expected %s got %s",
				meta.Host, meta.KnownHostsFingerprint, fp)
		}
		return nil
	}, nil
}

// buildAuthMethods constructs SSH auth methods from metadata.
// Prefers public-key auth when keyMaterial is non-empty; falls back to password.
func buildAuthMethods(meta *store.SSHMetadata, keyMaterial []byte) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if len(keyMaterial) > 0 {
		signer, err := ssh.ParsePrivateKey(keyMaterial)
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if meta.Password != "" {
		methods = append(methods, ssh.Password(meta.Password))
	}
	if len(methods) == 0 {
		return nil, errors.New("no auth method available: provide privateKey or password")
	}
	return methods, nil
}
