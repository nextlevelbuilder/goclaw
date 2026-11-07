package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

// Environment variables that configure the keyring.
const (
	EnvEncryptionKey   = "GOCLAW_ENCRYPTION_KEY"
	EnvEncryptionKeys  = "GOCLAW_ENCRYPTION_KEYS"
	EnvEncryptionKeyID = "GOCLAW_ENCRYPTION_KEY_ID"
)

// v3Prefix marks values that name the key that sealed them:
// "v3:aes-gcm:<kid>:<base64url-no-pad(nonce || ciphertext || tag)>".
const v3Prefix = "v3:aes-gcm:"

// ErrUnknownKeyID is returned when a value was sealed with a key that is not in
// the keyring. It never falls back to another key.
var ErrUnknownKeyID = errors.New("crypto: value was sealed with a key id that is not configured")

// UnknownKeyIDError carries the missing key id so operators can find the key.
type UnknownKeyIDError struct{ KID string }

func (e *UnknownKeyIDError) Error() string {
	return fmt.Sprintf("%s: %s (add its key to %s)", ErrUnknownKeyID.Error(), e.KID, EnvEncryptionKeys)
}
func (e *UnknownKeyIDError) Unwrap() error { return ErrUnknownKeyID }

// KeyID returns the identifier of a key: the first 8 hex characters of the
// SHA-256 of the raw 32-byte key. It is deterministic, so it needs no
// configuration of its own, and it reveals nothing usable about the key.
func KeyID(key string) (string, error) {
	raw, err := DeriveKey(key)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:4]), nil
}

// Keyring is a set of keys addressed by key id, one of which is active for new
// writes. It is immutable after construction.
type Keyring struct {
	keys   map[string]string // kid -> key as configured
	active string
}

// NewKeyring builds a keyring from keys (kid derived from each) and the kid of
// the active one. It rejects an empty set, duplicate keys and an unknown or
// missing active kid.
func NewKeyring(keys []string, activeKID string) (*Keyring, error) {
	if len(keys) == 0 {
		return nil, errors.New("crypto: keyring needs at least one key")
	}
	kr := &Keyring{keys: make(map[string]string, len(keys))}
	for _, k := range keys {
		kid, err := KeyID(k)
		if err != nil {
			return nil, err
		}
		if _, dup := kr.keys[kid]; dup {
			return nil, fmt.Errorf("crypto: key id %s appears more than once", kid)
		}
		kr.keys[kid] = k
	}
	if _, ok := kr.keys[activeKID]; !ok {
		return nil, fmt.Errorf("crypto: active key id %q is not in the keyring", activeKID)
	}
	kr.active = activeKID
	return kr, nil
}

// ParseKeyring builds the keyring from the three environment values:
//
//	keysSpec  "kid=key,kid=key" (GOCLAW_ENCRYPTION_KEYS)
//	activeID  kid used for new writes (GOCLAW_ENCRYPTION_KEY_ID)
//	legacyKey the single key (GOCLAW_ENCRYPTION_KEY), loaded under its own kid
//
// With only legacyKey set the behaviour is unchanged for operators: one key,
// active. Errors name the variable at fault and never echo key material.
func ParseKeyring(keysSpec, activeID, legacyKey string) (*Keyring, error) {
	var keys []string
	seen := map[string]bool{}
	add := func(k string) error {
		kid, err := KeyID(k)
		if err != nil {
			return err
		}
		if !seen[kid] {
			seen[kid] = true
			keys = append(keys, k)
		}
		return nil
	}
	if legacyKey != "" {
		if err := add(legacyKey); err != nil {
			return nil, fmt.Errorf("%s: %w", EnvEncryptionKey, err)
		}
	}
	for _, part := range strings.Split(keysSpec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		label, key, ok := strings.Cut(part, "=")
		if !ok || label == "" || key == "" {
			return nil, fmt.Errorf("%s: each entry must be kid=key", EnvEncryptionKeys)
		}
		kid, err := KeyID(key)
		if err != nil {
			return nil, fmt.Errorf("%s entry %q: %w", EnvEncryptionKeys, label, err)
		}
		if kid != label {
			return nil, fmt.Errorf("%s entry %q does not match the id of its key (%s)", EnvEncryptionKeys, label, kid)
		}
		if err := add(key); err != nil {
			return nil, err
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}
	activeID = strings.TrimSpace(activeID)
	if activeID == "" {
		switch {
		case legacyKey != "":
			activeID, _ = KeyID(legacyKey)
		case len(keys) == 1:
			activeID, _ = KeyID(keys[0])
		default:
			return nil, fmt.Errorf("%s is required when %s holds several keys", EnvEncryptionKeyID, EnvEncryptionKeys)
		}
	}
	kr, err := NewKeyring(keys, activeID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvEncryptionKeyID, err)
	}
	return kr, nil
}

// ActiveKID is the key id used for new writes.
func (k *Keyring) ActiveKID() string { return k.active }

// ActiveKey is the key material used for new writes.
func (k *Keyring) ActiveKey() string { return k.keys[k.active] }

// KIDs lists every key id, sorted.
func (k *Keyring) KIDs() []string {
	out := make([]string, 0, len(k.keys))
	for kid := range k.keys {
		out = append(out, kid)
	}
	sort.Strings(out)
	return out
}

func (k *Keyring) key(kid string) (string, bool) {
	if k == nil {
		return "", false
	}
	v, ok := k.keys[kid]
	return v, ok
}

var (
	ringMu sync.RWMutex
	ring   *Keyring
)

// SetKeyring installs the process keyring. nil removes it.
func SetKeyring(k *Keyring) {
	ringMu.Lock()
	ring = k
	ringMu.Unlock()
}

// CurrentKeyring returns the installed keyring, or nil.
func CurrentKeyring() *Keyring {
	ringMu.RLock()
	defer ringMu.RUnlock()
	return ring
}

// ConfigureKeyringFromEnv reads the encryption environment, installs the
// keyring and returns the active key, which callers pass to the stores as
// before. It returns "" with no error when no key is configured at all.
func ConfigureKeyringFromEnv() (string, error) {
	kr, err := ParseKeyring(os.Getenv(EnvEncryptionKeys), os.Getenv(EnvEncryptionKeyID), os.Getenv(EnvEncryptionKey))
	if err != nil {
		return "", err
	}
	SetKeyring(kr)
	if kr == nil {
		return "", nil
	}
	return kr.ActiveKey(), nil
}

// IsEncryptedV3 reports whether value carries the "v3:aes-gcm:" prefix.
func IsEncryptedV3(value string) bool { return strings.HasPrefix(value, v3Prefix) }

// EnvelopeKeyID returns the key id a v3 value names.
func EnvelopeKeyID(value string) (string, bool) {
	rest, ok := strings.CutPrefix(value, v3Prefix)
	if !ok {
		return "", false
	}
	kid, _, ok := strings.Cut(rest, ":")
	return kid, ok && kid != ""
}

// sealingKID returns the kid to stamp when sealing with key: only when a
// keyring is installed and holds that key.
func sealingKID(key string) (string, bool) {
	kr := CurrentKeyring()
	if kr == nil {
		return "", false
	}
	kid, err := KeyID(key)
	if err != nil {
		return "", false
	}
	_, ok := kr.key(kid)
	return kid, ok
}

// keyForKID finds the key a v3 value names: the keyring first, then hint (the
// key the caller passed) when it is that key.
func keyForKID(kid, hint string) (string, error) {
	if k, ok := CurrentKeyring().key(kid); ok {
		return k, nil
	}
	if hint != "" {
		if hk, err := KeyID(hint); err == nil && hk == kid {
			return hint, nil
		}
	}
	return "", &UnknownKeyIDError{KID: kid}
}

// otherKeys lists keyring keys except the one with kid skip, for opening values
// that predate key ids.
func otherKeys(skip string) []string {
	kr := CurrentKeyring()
	if kr == nil {
		return nil
	}
	var out []string
	for _, kid := range kr.KIDs() {
		if kid != skip {
			out = append(out, kr.keys[kid])
		}
	}
	return out
}

// ActiveKeyID returns the kid that Encrypt would stamp when sealing with key,
// and false when no installed keyring holds key (values stay v1/v2).
func ActiveKeyID(key string) (string, bool) { return sealingKID(key) }

// ActiveKeyFromEnv re-reads the encryption environment, installs the keyring
// and returns the active key. It returns "" when no key is configured or the
// configuration is invalid; ConfigureKeyringFromEnv reports that error at
// startup, so callers that only need the key can ignore it.
func ActiveKeyFromEnv() string {
	key, err := ConfigureKeyringFromEnv()
	if err != nil {
		return ""
	}
	return key
}

// KeyFor returns the key with the given id, for callers that must seal under a
// specific key (key rotation).
func (k *Keyring) KeyFor(kid string) (string, bool) { return k.key(kid) }
