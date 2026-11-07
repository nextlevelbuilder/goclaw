package store

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
)

const (
	secretVaultMaxDomains = 20
	secretVaultMaxHints   = 500
)

var (
	secretVaultNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	// Host or *.host made of DNS labels. No scheme, port, path or lone wildcard.
	secretVaultDomainRe = regexp.MustCompile(`^(\*\.)?[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
)

// payloadWire is the JSON shape encrypted at rest. It is separate from
// SecretVaultPayload so the public type never needs a MarshalJSON.
type payloadWire struct {
	Username string `json:"u,omitempty"`
	Secret   string `json:"s"`
}

// NormalizeSecretDomains lowercases, trims and de-duplicates allowed domains
// and validates each one. At least one domain is required.
func NormalizeSecretDomains(domains []string) ([]string, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("%w: at least one allowed domain is required", ErrSecretVaultInvalid)
	}
	if len(domains) > secretVaultMaxDomains {
		return nil, fmt.Errorf("%w: at most %d allowed domains", ErrSecretVaultInvalid, secretVaultMaxDomains)
	}
	seen := make(map[string]struct{}, len(domains))
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if len(d) > 253 || !secretVaultDomainRe.MatchString(d) {
			return nil, fmt.Errorf("%w: allowed domain %q is not valid", ErrSecretVaultInvalid, d)
		}
		if _, dup := seen[d]; dup {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	return out, nil
}

// ValidateSecretVaultEntry checks and normalizes entry metadata in place.
func ValidateSecretVaultEntry(e *SecretVaultEntry) error {
	e.Name = strings.TrimSpace(e.Name)
	if !secretVaultNameRe.MatchString(e.Name) {
		return fmt.Errorf("%w: secret name %q is invalid", ErrSecretVaultInvalid, e.Name)
	}
	if e.Kind != SecretKindAPIKey && e.Kind != SecretKindLogin {
		return fmt.Errorf("%w: secret kind %q is not supported", ErrSecretVaultInvalid, e.Kind)
	}
	domains, err := NormalizeSecretDomains(e.AllowedDomains)
	if err != nil {
		return err
	}
	e.AllowedDomains = domains
	if err := ValidateSecretHints(e.Hints); err != nil {
		return err
	}
	return ValidateUserID(e.CreatedBy)
}

// ValidateSecretHints bounds the free-text hints shown to the agent.
func ValidateSecretHints(h string) error {
	if len(h) > secretVaultMaxHints {
		return fmt.Errorf("%w: hints longer than %d characters", ErrSecretVaultInvalid, secretVaultMaxHints)
	}
	return nil
}

// ValidateSecretVaultPayload checks the payload shape for a kind.
func ValidateSecretVaultPayload(kind string, p SecretVaultPayload) error {
	if p.Secret == "" {
		return fmt.Errorf("%w: secret value is missing", ErrSecretVaultInvalid)
	}
	switch kind {
	case SecretKindLogin:
		if p.Username == "" {
			return fmt.Errorf("%w: login requires a username", ErrSecretVaultInvalid)
		}
	case SecretKindAPIKey:
		if p.Username != "" {
			return fmt.Errorf("%w: api_key does not take a username", ErrSecretVaultInvalid)
		}
	default:
		return fmt.Errorf("%w: secret kind %q is not supported", ErrSecretVaultInvalid, kind)
	}
	return nil
}

// EncodeSecretPayloadAAD encrypts a payload for storage as a v2 envelope bound
// to the entry (see SecretPayloadAAD). e must be the entry as it will be
// stored, including the revision being written. It fails closed when the key
// is empty.
func EncodeSecretPayloadAAD(p SecretVaultPayload, key string, e *SecretVaultEntry) (string, error) {
	if key == "" {
		return "", ErrSecretVaultNoKey
	}
	raw, err := json.Marshal(payloadWire{Username: p.Username, Secret: p.Secret})
	if err != nil {
		return "", fmt.Errorf("secret vault: encode payload: %w", err)
	}
	enc, err := crypto.EncryptWithAAD(string(raw), key, SecretPayloadAAD(e))
	if err != nil {
		return "", fmt.Errorf("secret vault: encrypt payload: %w", err)
	}
	return enc, nil
}

// IsLegacySecretCiphertext reports whether a stored payload is a pre-binding
// "aes-gcm:" value that has not been rewrapped yet.
func IsLegacySecretCiphertext(stored string) bool {
	return crypto.IsEncrypted(stored)
}

// DecodeSecretPayload decrypts a stored payload for entry e. A v2 value is
// opened with the entry's AAD, so it only decrypts in the row, tenant, name,
// kind, policy and revision it was written for. A legacy "aes-gcm:" value is
// still readable until `goclaw secret-vault rewrap` converts it. A value
// without either prefix is rejected rather than returned as-is.
func DecodeSecretPayload(stored, key string, e *SecretVaultEntry) (SecretVaultPayload, error) {
	if key == "" {
		return SecretVaultPayload{}, ErrSecretVaultNoKey
	}
	var plain string
	var err error
	switch {
	case crypto.IsEncryptedAAD(stored):
		plain, err = crypto.DecryptWithAAD(stored, key, SecretPayloadAAD(e))
	case crypto.IsEncrypted(stored):
		slog.Warn("security.secret_vault.legacy_ciphertext", "entry_id", e.ID)
		plain, err = crypto.Decrypt(stored, key)
	default:
		return SecretVaultPayload{}, ErrSecretVaultNotEncrypted
	}
	if err != nil {
		return SecretVaultPayload{}, fmt.Errorf("secret vault: decrypt payload: %w", err)
	}
	var w payloadWire
	if err := json.Unmarshal([]byte(plain), &w); err != nil {
		return SecretVaultPayload{}, fmt.Errorf("secret vault: decode payload: %w", err)
	}
	return SecretVaultPayload{Username: w.Username, Secret: w.Secret}, nil
}

// SecretRewrapOutcome is the result of examining one stored payload.
type SecretRewrapOutcome int

const (
	// SecretRewrapCurrent: already a bound envelope that opens for this entry.
	SecretRewrapCurrent SecretRewrapOutcome = iota
	// SecretRewrapUpgrade: a legacy value; the returned ciphertext replaces it.
	SecretRewrapUpgrade
	// SecretRewrapUnreadable: cannot be opened with the key for this entry.
	SecretRewrapUnreadable
)

// SecretRewrapReport counts the rows a rewrap examined. Unreadable holds entry
// IDs only; no name, value or ciphertext leaves the store.
type SecretRewrapReport struct {
	Total      int
	Rewrapped  int
	Current    int
	Unreadable []uuid.UUID
}

// RewrapSecretCiphertext decides what to do with one stored payload. A legacy
// value is decrypted and sealed again bound to entry e at its current
// revision. A bound value is only verified. Anything else, or a value the key
// cannot open, is reported unreadable and left untouched.
func RewrapSecretCiphertext(stored, key string, e *SecretVaultEntry) (string, SecretRewrapOutcome) {
	switch {
	case crypto.IsEncryptedAAD(stored):
		if _, err := DecodeSecretPayload(stored, key, e); err != nil {
			return "", SecretRewrapUnreadable
		}
		return "", SecretRewrapCurrent
	case crypto.IsEncrypted(stored):
		p, err := DecodeSecretPayload(stored, key, e)
		if err != nil {
			return "", SecretRewrapUnreadable
		}
		enc, err := EncodeSecretPayloadAAD(p, key, e)
		if err != nil {
			return "", SecretRewrapUnreadable
		}
		return enc, SecretRewrapUpgrade
	default:
		return "", SecretRewrapUnreadable
	}
}
