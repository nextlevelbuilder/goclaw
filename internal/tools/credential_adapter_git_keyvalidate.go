package tools

// SSH key validation for the git adapter (Phase 4, issue #82).
//
// Why this lives in tools and not store: the store layer is type-agnostic
// (it only writes encrypted bytes). The "what's a valid SSH key for this
// adapter?" rule belongs to the adapter itself. Callers (HTTP/WS save
// handlers) invoke ValidateSSHKey BEFORE encrypting and persisting.
//
// v1 scope: passphrase-protected keys are rejected. Reason: the runtime has
// nowhere to hold a passphrase for unattended `git fetch` and storing it
// alongside the key defeats the purpose. Phase 6 docs explain how operators
// can strip the passphrase via `ssh-keygen -p` before saving.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/crypto/ssh"
)

// ErrSSHKeyPassphraseUnsupported is returned when the supplied PEM is
// passphrase-protected. Sentinel so HTTP/WS handlers can map it to the
// localized i18n message without string-matching.
var ErrSSHKeyPassphraseUnsupported = errors.New("ssh key: passphrase-protected keys not supported in v1")

// ValidatePATToken is the exported alias of validateTokenShape — HTTP/WS
// save handlers call this BEFORE encrypting so a malformed token (empty,
// oversize, control chars) is rejected with a localized error before any
// DB write or audit emit. Phase 3's runtime check still defends in depth.
func ValidatePATToken(tok string) error { return validateTokenShape(tok) }

// ValidateSSHKeyForStorage runs the normal parser plus an OpenSSH parser check.
// The second check catches keys that Go accepts but the runtime ssh client would
// reject later with opaque errors such as "error in libcrypto".
func ValidateSSHKeyForStorage(ctx context.Context, pem []byte) error {
	if err := ValidateSSHKey(pem); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return openSSHKeyCompatibilityCheck(ctx, pem)
}

var openSSHKeyCompatibilityCheck = validateSSHKeyOpenSSHCompatible

func validateSSHKeyOpenSSHCompatible(ctx context.Context, pem []byte) error {
	sshKeygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		return nil
	}
	keyPath, cleanup, err := materializeEphemeral(ctx, pem, "gitkey-validate")
	if err != nil {
		return err
	}
	defer func() { _ = cleanup() }()

	out, err := exec.CommandContext(ctx, sshKeygen, "-y", "-f", keyPath).CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = err.Error()
	}
	msg = strings.ReplaceAll(msg, keyPath, "[keyfile]")
	return fmt.Errorf("ssh key openssh parse: %s", msg)
}

// ValidateSSHKey parses the supplied PEM with x/crypto/ssh. Any non-nil
// error blocks the save: a key that fails to parse here will also fail at
// `ssh -i` time, and we want the diagnostic to happen at save (where the
// user can fix it) not at exec (where the agent stalls).
func ValidateSSHKey(pem []byte) error {
	if len(pem) == 0 {
		return errors.New("ssh key: empty PEM")
	}
	_, err := ssh.ParsePrivateKey(pem)
	if err == nil {
		return nil
	}
	var pme *ssh.PassphraseMissingError
	if errors.As(err, &pme) {
		return ErrSSHKeyPassphraseUnsupported
	}
	return fmt.Errorf("ssh key parse: %w", err)
}
