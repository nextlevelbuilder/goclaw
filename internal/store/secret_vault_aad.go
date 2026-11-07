package store

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

const secretVaultAADLabel = "goclaw/secret-vault/v2"

// SecretPayloadAAD builds the additional authenticated data that binds an
// encrypted payload to the entry it lives in: tenant, entry id, name, kind,
// allowed domains, approval flag and revision. A ciphertext copied into another
// row, restored after a newer write, or kept after its egress policy was
// widened no longer authenticates.
//
// Each field is length-prefixed so no value can be confused with a delimiter.
// hints is deliberately excluded: it is advisory text, while every egress
// decision is enforced by allowed_domains, which is bound.
//
// Entry must carry Revision >= 1. The layout is shared by the PG store, the
// SQLite store and the rewrap command and is pinned by a golden test vector.
func SecretPayloadAAD(e *SecretVaultEntry) []byte {
	approval := "0"
	if e.RequireApproval {
		approval = "1"
	}
	aad := []byte(secretVaultAADLabel)
	for _, f := range []string{
		strings.ToLower(e.TenantID.String()),
		strings.ToLower(e.ID.String()),
		e.Name,
		e.Kind,
		secretDomainsDigest(e.AllowedDomains),
		approval,
		strconv.FormatInt(e.Revision, 10),
	} {
		aad = append(aad, 0x00)
		aad = binary.BigEndian.AppendUint32(aad, uint32(len(f)))
		aad = append(aad, f...)
	}
	return aad
}

// secretDomainsDigest is the hex sha256 of the sorted, lowercased domains
// joined by newlines.
func secretDomainsDigest(domains []string) string {
	norm := make([]string, len(domains))
	for i, d := range domains {
		norm[i] = strings.ToLower(strings.TrimSpace(d))
	}
	sort.Strings(norm)
	sum := sha256.Sum256([]byte(strings.Join(norm, "\n")))
	return hex.EncodeToString(sum[:])
}
