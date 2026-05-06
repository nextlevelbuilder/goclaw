package max

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// VerifyContactHash validates the hash field returned with a request_contact
// button response, confirming that the contact's vCard data was issued by Max
// for this bot's session and has not been tampered with.
//
// Per Max API docs: hash = HMAC-SHA256(access_token, vcf_info)
// Reference: https://dev.max.ru/docs-api (request_contact button section)
//
// Inputs:
//   - botToken: the bot's access token (from creds.BotToken)
//   - vcfInfo:  the vCard string from Attachment.Payload.VcfInfo
//   - gotHash:  the hex-encoded HMAC from Attachment.Payload.Hash
//
// Returns nil if the hash is valid, ErrInvalidContactHash otherwise.
// Returns ErrInvalidHashFormat if gotHash is not valid hex.
//
// Comparison uses hmac.Equal for constant-time evaluation, defending against
// timing oracles that could otherwise leak the expected hash byte-by-byte.
func VerifyContactHash(botToken, vcfInfo, gotHash string) error {
	if botToken == "" {
		return errors.New("max auth: bot token is required")
	}
	if vcfInfo == "" {
		return errors.New("max auth: vcf_info is empty")
	}
	if gotHash == "" {
		return ErrInvalidContactHash
	}

	gotBytes, err := hex.DecodeString(gotHash)
	if err != nil {
		return ErrInvalidHashFormat
	}

	mac := hmac.New(sha256.New, []byte(botToken))
	mac.Write([]byte(vcfInfo))
	expected := mac.Sum(nil)

	if !hmac.Equal(expected, gotBytes) {
		return ErrInvalidContactHash
	}
	return nil
}

// ErrInvalidContactHash is returned when a request_contact hash does not match
// the expected HMAC of the vCard data. Treat as security-relevant: the contact
// payload may have been forged.
var ErrInvalidContactHash = errors.New("max auth: contact hash mismatch")

// ErrInvalidHashFormat is returned when the hash field is not valid hex.
// Indicates a malformed payload — distinct from a security failure.
var ErrInvalidHashFormat = errors.New("max auth: hash is not valid hex")
