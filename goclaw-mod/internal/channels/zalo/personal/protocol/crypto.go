// Package protocol implements the Zalo personal chat protocol.
// Ported from zcago (MIT license): https://github.com/amrakk/zcago
package protocol

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

var (
	errInvalidBlockSize    = errors.New("zalo_personal: invalid block size")
	errInvalidPKCS7Data    = errors.New("zalo_personal: invalid PKCS#7 data")
	errInvalidPKCS7Padding = errors.New("zalo_personal: invalid PKCS#7 padding")
)

// EncodeAESCBC encrypts data with AES-CBC using a zero IV (Zalo protocol quirk).
// encHex=true returns hex-encoded ciphertext, false returns base64.
func EncodeAESCBC(key []byte, data string, encHex bool) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("zalo_personal crypto: new cipher: %w", err)
	}

	plain, err := pkcs7Pad([]byte(data), aes.BlockSize)
	if err != nil {
		return "", fmt.Errorf("zalo_personal crypto: pkcs7 pad: %w", err)
	}

	iv := make([]byte, aes.BlockSize) // zero IV
	ct := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, plain)

	if encHex {
		return hex.EncodeToString(ct), nil
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

// DecodeAESCBC decrypts base64-encoded AES-CBC ciphertext with zero IV.
func DecodeAESCBC(key []byte, data string) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("zalo_personal crypto: base64 decode: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("zalo_personal crypto: new cipher: %w", err)
	}

	iv := make([]byte, aes.BlockSize) // zero IV
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)

	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return nil, fmt.Errorf("zalo_personal crypto: pkcs7 unpad: %w", err)
	}
	return plain, nil
}

// DecodeAESGCM decrypts with AES-GCM using a 16-byte nonce (non-standard).
// Zalo uses cipher.NewGCMWithNonceSize(block, 16) instead of standard 12.
func DecodeAESGCM(key, iv, aad, ct []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("zalo_personal crypto: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCMWithNonceSize(block, 16) // non-standard 16-byte nonce
	if err != nil {
		return nil, fmt.Errorf("zalo_personal crypto: new gcm: %w", err)
	}

	plain, err := gcm.Open(nil, iv, ct, aad)
	if err != nil {
		return nil, fmt.Errorf("zalo_personal crypto: gcm open: %w", err)
	}
	return plain, nil
}

// --- PKCS#7 padding ---

func pkcs7Pad(data []byte, blockSize int) ([]byte, error) {
	if blockSize <= 0 {
		return nil, errInvalidBlockSize
	}
	padLen := blockSize - (len(data) % blockSize)
	if padLen == 0 {
		padLen = blockSize
	}
	return append(data, bytes.Repeat([]byte{byte(padLen)}, padLen)...), nil
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if blockSize <= 0 {
		return nil, errInvalidBlockSize
	}
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errInvalidPKCS7Data
	}
	padLen := int(data[len(data)-1])
	if padLen == 0 || padLen > blockSize || padLen > len(data) {
		return nil, errInvalidPKCS7Padding
	}
	if !bytes.Equal(bytes.Repeat([]byte{byte(padLen)}, padLen), data[len(data)-padLen:]) {
		return nil, errInvalidPKCS7Padding
	}
	return data[:len(data)-padLen], nil
}
