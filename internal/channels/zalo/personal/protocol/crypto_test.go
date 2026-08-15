package protocol

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"testing"
)

func TestEncodeDecodeAESCBC_Roundtrip(t *testing.T) {
	key := []byte("0123456789ABCDEF") // 16 bytes = AES-128
	tests := []struct {
		name string
		data string
	}{
		{"short text", "hello"},
		{"exact block size", "0123456789ABCDEF"},
		{"multi block", "this is a longer test string that spans multiple AES blocks"},
		{"json payload", `{"imei":"abc","ts":12345}`},
		{"empty string", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeAESCBC(key, tt.data, false)
			if err != nil {
				t.Fatalf("EncodeAESCBC: %v", err)
			}
			decoded, err := DecodeAESCBC(key, encoded)
			if err != nil {
				t.Fatalf("DecodeAESCBC: %v", err)
			}
			if string(decoded) != tt.data {
				t.Errorf("roundtrip mismatch: got %q, want %q", decoded, tt.data)
			}
		})
	}
}

func TestEncodeAESCBC_ZeroIV_Deterministic(t *testing.T) {
	key := []byte("0123456789ABCDEF")
	data := "deterministic test"

	enc1, _ := EncodeAESCBC(key, data, false)
	enc2, _ := EncodeAESCBC(key, data, false)

	if enc1 != enc2 {
		t.Error("zero-IV AES-CBC should be deterministic")
	}
}

func TestEncodeAESCBC_HexVsBase64(t *testing.T) {
	key := []byte("0123456789ABCDEF")
	data := "test data"

	hexEnc, _ := EncodeAESCBC(key, data, true)
	b64Enc, _ := EncodeAESCBC(key, data, false)

	if hexEnc == b64Enc {
		t.Error("hex and base64 encodings should differ")
	}

	b64Dec, err := base64.StdEncoding.DecodeString(b64Enc)
	if err != nil || len(b64Dec) == 0 {
		t.Fatal("base64 decode failed")
	}
}

func TestEncodeAESCBC_InvalidKey(t *testing.T) {
	_, err := EncodeAESCBC([]byte("short"), "data", false)
	if err == nil {
		t.Fatal("expected error for invalid key size")
	}
}

func TestDecodeAESCBC_InvalidBase64(t *testing.T) {
	key := []byte("0123456789ABCDEF")
	_, err := DecodeAESCBC(key, "not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDecodeAESCBC_WrongKey(t *testing.T) {
	key1 := []byte("0123456789ABCDEF")
	key2 := []byte("FEDCBA9876543210")

	encoded, err := EncodeAESCBC(key1, "secret data", false)
	if err != nil {
		t.Fatal(err)
	}

	_, err = DecodeAESCBC(key2, encoded)
	// Wrong key should produce padding error in most cases
	if err == nil {
		t.Log("note: no error with wrong key (padding happened to be valid)")
	}
}

func TestDecodeAESGCM(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	iv := make([]byte, 16)
	for i := range iv {
		iv[i] = byte(i + 0x10)
	}
	aad := make([]byte, 16)
	for i := range aad {
		aad[i] = byte(i + 0x20)
	}

	// Encrypt with 16-byte nonce GCM (same as Zalo uses)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, 16)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("hello from AES-GCM test")
	ct := gcm.Seal(nil, iv, plaintext, aad)

	// Decrypt with our function
	decrypted, err := DecodeAESGCM(key, iv, aad, ct)
	if err != nil {
		t.Fatalf("DecodeAESGCM: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestDecodeAESGCM_InvalidKey(t *testing.T) {
	_, err := DecodeAESGCM([]byte("short"), make([]byte, 16), make([]byte, 16), make([]byte, 32))
	if err == nil {
		t.Fatal("expected error for invalid key size")
	}
}

func TestDecodeAESGCM_TamperedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	iv := make([]byte, 16)
	aad := make([]byte, 16)

	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCMWithNonceSize(block, 16)
	ct := gcm.Seal(nil, iv, []byte("test"), aad)

	ct[0] ^= 0xFF // tamper

	_, err := DecodeAESGCM(key, iv, aad, ct)
	if err == nil {
		t.Fatal("expected error for tampered ciphertext")
	}
}

func TestPKCS7PadUnpad_Roundtrip(t *testing.T) {
	tests := []struct {
		name    string
		dataLen int
	}{
		{"empty", 0},
		{"1 byte", 1},
		{"15 bytes", 15},
		{"16 bytes (full block)", 16},
		{"17 bytes", 17},
		{"31 bytes", 31},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, tt.dataLen)
			for i := range data {
				data[i] = byte(i)
			}

			padded, err := pkcs7Pad(data, aes.BlockSize)
			if err != nil {
				t.Fatalf("pkcs7Pad: %v", err)
			}
			if len(padded)%aes.BlockSize != 0 {
				t.Errorf("padded len %d not multiple of block size", len(padded))
			}

			unpadded, err := pkcs7Unpad(padded, aes.BlockSize)
			if err != nil {
				t.Fatalf("pkcs7Unpad: %v", err)
			}
			if len(unpadded) != tt.dataLen {
				t.Errorf("unpadded len = %d, want %d", len(unpadded), tt.dataLen)
			}
		})
	}
}

func TestPKCS7Unpad_InvalidPadding(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"wrong length", []byte{1, 2, 3}},
		{"zero pad byte", make([]byte, 16)},
		{"pad too large", append(make([]byte, 15), 17)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pkcs7Unpad(tt.data, 16)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPKCS7Pad_InvalidBlockSize(t *testing.T) {
	if _, err := pkcs7Pad([]byte("test"), 0); err == nil {
		t.Fatal("expected error for blockSize 0")
	}
	if _, err := pkcs7Pad([]byte("test"), -1); err == nil {
		t.Fatal("expected error for negative blockSize")
	}
}
