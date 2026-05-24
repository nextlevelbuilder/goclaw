package backends

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/workstation"
)

func init() {
	workstation.Register(store.BackendSSH, newSSHBackend)
}

// SSHBackend implements workstation.Backend over SSH.
// One SSHBackend is created per Workstation record; it owns a clientPool.
type SSHBackend struct {
	ws   *store.Workstation
	meta *store.SSHMetadata
	pool *clientPool
	// keyMaterial holds the decoded private key PEM bytes, cleared on Close.
	keyMaterial []byte
}

// newSSHBackend is the factory registered with workstation.Register.
func newSSHBackend(ws *store.Workstation) (workstation.Backend, error) {
	meta, err := store.UnmarshalSSHMetadata(ws.Metadata)
	if err != nil {
		return nil, fmt.Errorf("ssh[%s]: invalid metadata: %w", ws.WorkstationKey, err)
	}

	km := []byte(meta.PrivateKey) // plaintext PEM; already decrypted by store layer

	return &SSHBackend{
		ws:          ws,
		meta:        meta,
		pool:        newClientPool(),
		keyMaterial: km,
	}, nil
}

// Name returns the backend type identifier.
func (b *SSHBackend) Name() string { return "ssh" }

// HealthCheck dials the workstation, runs "echo ok", and tears down within 5s.
func (b *SSHBackend) HealthCheck(ctx context.Context) error {
	hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	client, release, err := b.pool.Get(hctx, b.ws, b.meta, b.keyMaterial)
	if err != nil {
		return fmt.Errorf("ssh[%s]: health check dial: %w", b.ws.WorkstationKey, err)
	}
	defer release()

	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh[%s]: health check session: %w", b.ws.WorkstationKey, err)
	}
	defer sess.Close()

	out, err := sess.CombinedOutput("echo ok")
	if err != nil {
		return fmt.Errorf("ssh[%s]: health check exec: %w", b.ws.WorkstationKey, err)
	}
	if strings.TrimSpace(string(out)) != "ok" {
		return fmt.Errorf("ssh[%s]: health check: unexpected output %q", b.ws.WorkstationKey, string(out))
	}
	return nil
}

// OpenSession borrows a pooled *ssh.Client and returns an SSHSession.
// The caller must call session.Close to return the client to the pool.
func (b *SSHBackend) OpenSession(ctx context.Context, sessionID string) (workstation.Session, error) {
	client, release, err := b.pool.Get(ctx, b.ws, b.meta, b.keyMaterial)
	if err != nil {
		return nil, fmt.Errorf("ssh[%s]: open session: %w", b.ws.WorkstationKey, err)
	}
	return &SSHSession{
		id:      sessionID,
		client:  client,
		release: release,
		wsKey:   b.ws.WorkstationKey,
	}, nil
}

// CloseSession is a no-op at the backend level; session cleanup is done by SSHSession.Close.
// The session manager (Phase 4) tracks open sessions and calls session.Close directly.
func (b *SSHBackend) CloseSession(_ context.Context, _ string) error { return nil }

// Close shuts down the client pool, terminating all idle SSH connections and the
// prune goroutine. Must be called when the backend is evicted from BackendCache.
func (b *SSHBackend) Close() error {
	b.pool.Close()
	return nil
}
