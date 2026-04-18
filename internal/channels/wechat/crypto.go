package wechat

import (
	"crypto/aes"
	"fmt"
)

// aesEcbPaddedSize computes ciphertext size for AES-128-ECB with PKCS7 padding.
func aesEcbPaddedSize(plaintextSize int) int64 {
	return int64(((plaintextSize + 1) / aes.BlockSize + 1) * aes.BlockSize)
}

// encryptAesEcb encrypts plaintext with AES-128-ECB and PKCS7 padding.
func encryptAesEcb(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	bs := block.BlockSize()
	padding := bs - len(plaintext)%bs
	padded := make([]byte, len(plaintext)+padding)
	copy(padded, plaintext)
	for i := len(plaintext); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	ciphertext := make([]byte, len(padded))
	for i := 0; i < len(padded); i += bs {
		block.Encrypt(ciphertext[i:i+bs], padded[i:i+bs])
	}
	return ciphertext, nil
}

// decryptAesEcb decrypts AES-128-ECB ciphertext with PKCS7 padding.
func decryptAesEcb(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	bs := block.BlockSize()
	if len(ciphertext)%bs != 0 {
		return nil, fmt.Errorf("ciphertext length %d not multiple of block size %d", len(ciphertext), bs)
	}
	plaintext := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += bs {
		block.Decrypt(plaintext[i:i+bs], ciphertext[i:i+bs])
	}
	if len(plaintext) == 0 {
		return plaintext, nil
	}
	padLen := int(plaintext[len(plaintext)-1])
	if padLen > bs || padLen == 0 {
		return nil, fmt.Errorf("invalid PKCS7 padding length %d", padLen)
	}
	for i := len(plaintext) - padLen; i < len(plaintext); i++ {
		if plaintext[i] != byte(padLen) {
			return nil, fmt.Errorf("invalid PKCS7 padding byte at position %d", i)
		}
	}
	return plaintext[:len(plaintext)-padLen], nil
}
