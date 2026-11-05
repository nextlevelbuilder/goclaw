package store

import (
	"encoding/json"
	"fmt"
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
	if err := ValidateUserID(e.CreatedBy); err != nil {
		return err
	}
	return applySecretVaultOrigin(e)
}

// applySecretVaultOrigin derives status and origin from CreatedByAgent so a
// caller can never create an agent entry that skips approval, nor mark a user
// entry pending.
func applySecretVaultOrigin(e *SecretVaultEntry) error {
	if e.CreatedByAgent == nil {
		e.CreatedVia, e.Status = SecretVaultCreatedViaUser, SecretVaultStatusActive
		return nil
	}
	if *e.CreatedByAgent == uuid.Nil {
		return fmt.Errorf("%w: creating agent id is empty", ErrSecretVaultInvalid)
	}
	e.CreatedVia, e.Status = SecretVaultCreatedViaAgent, SecretVaultStatusPending
	return nil
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

// EncodeSecretPayload encrypts a payload for storage. It fails closed when the
// key is empty because crypto.Encrypt would otherwise return plaintext.
func EncodeSecretPayload(p SecretVaultPayload, key string) (string, error) {
	if key == "" {
		return "", ErrSecretVaultNoKey
	}
	raw, err := json.Marshal(payloadWire{Username: p.Username, Secret: p.Secret})
	if err != nil {
		return "", fmt.Errorf("secret vault: encode payload: %w", err)
	}
	enc, err := crypto.Encrypt(string(raw), key)
	if err != nil {
		return "", fmt.Errorf("secret vault: encrypt payload: %w", err)
	}
	if !crypto.IsEncrypted(enc) {
		return "", ErrSecretVaultNotEncrypted
	}
	return enc, nil
}

// DecodeSecretPayload decrypts a stored payload. A stored value without the
// encryption prefix is rejected rather than returned as-is.
func DecodeSecretPayload(stored, key string) (SecretVaultPayload, error) {
	if key == "" {
		return SecretVaultPayload{}, ErrSecretVaultNoKey
	}
	if !crypto.IsEncrypted(stored) {
		return SecretVaultPayload{}, ErrSecretVaultNotEncrypted
	}
	plain, err := crypto.Decrypt(stored, key)
	if err != nil {
		return SecretVaultPayload{}, fmt.Errorf("secret vault: decrypt payload: %w", err)
	}
	var w payloadWire
	if err := json.Unmarshal([]byte(plain), &w); err != nil {
		return SecretVaultPayload{}, fmt.Errorf("secret vault: decode payload: %w", err)
	}
	return SecretVaultPayload{Username: w.Username, Secret: w.Secret}, nil
}
