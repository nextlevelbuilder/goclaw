// Manual E2E test for Zalo Personal file & image sending via Go protocol package.
// Requires real Zalo credentials — not run by CI.
//
// Usage:
//
//	docker run --rm -v $(pwd):/src -w /src \
//	  -e ZALO_CREDS_FILE=/src/tests/zalo_e2e/creds.json \
//	  -e ZALO_GROUP_ID=<your-group-id> \
//	  golang:1.25-alpine go run ./tests/zalo_e2e/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

func main() {
	credsFile := os.Getenv("ZALO_CREDS_FILE")
	if credsFile == "" {
		log.Fatal("ZALO_CREDS_FILE env var is required")
	}
	groupID := os.Getenv("ZALO_GROUP_ID")
	if groupID == "" {
		log.Fatal("ZALO_GROUP_ID env var is required")
	}

	log.Println("=== Zalo E2E Test ===")
	log.Println("Creds:", credsFile)
	log.Println("Group:", groupID)

	// Load credentials
	raw, err := os.ReadFile(credsFile)
	if err != nil {
		log.Fatalf("read creds: %v", err)
	}
	var creds protocol.Credentials
	if err := json.Unmarshal(raw, &creds); err != nil {
		log.Fatalf("parse creds: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Login
	log.Println("\n--- Step 1: Login ---")
	sess := protocol.NewSession()
	if err := protocol.LoginWithCredentials(ctx, sess, creds); err != nil {
		log.Fatalf("login: %v", err)
	}
	log.Println("UID:", sess.UID)

	// Start listener (needed for file upload WS callback)
	log.Println("\n--- Step 2: Start Listener ---")
	ln, err := protocol.NewListener(sess)
	if err != nil {
		log.Fatalf("new listener: %v", err)
	}
	if err := ln.Start(ctx); err != nil {
		log.Fatalf("start listener: %v", err)
	}
	defer ln.Stop()

	// Wait for cipher key
	log.Println("Waiting for cipher key (max 15s)...")
	time.Sleep(3 * time.Second)
	log.Println("Listener ready")

	// Test 1: Send text message
	log.Println("\n--- Test 1: Send Text Message ---")
	msgID, err := protocol.SendMessage(ctx, sess, groupID, protocol.ThreadTypeGroup,
		fmt.Sprintf("GoClaw Go E2E test - %s", time.Now().Format(time.RFC3339)))
	if err != nil {
		log.Fatalf("send text: %v", err)
	}
	log.Println("OK, msgId:", msgID)

	// Test 2: Upload & send image
	log.Println("\n--- Test 2: Upload & Send Image ---")
	imgPath := createTestImage()
	defer os.Remove(imgPath)

	upload, err := protocol.UploadImage(ctx, sess, groupID, protocol.ThreadTypeGroup, imgPath)
	if err != nil {
		log.Fatalf("upload image: %v", err)
	}
	log.Printf("Upload OK: photoId=%s normalUrl=%s", upload.PhotoID.String(), truncate(upload.NormalURL, 60))

	imgMsgID, err := protocol.SendImage(ctx, sess, groupID, protocol.ThreadTypeGroup, upload, "Go E2E test image")
	if err != nil {
		log.Fatalf("send image: %v", err)
	}
	log.Println("OK, msgId:", imgMsgID)

	// Test 3: Upload & send file (requires WS callback)
	log.Println("\n--- Test 3: Upload & Send File ---")
	filePath := createTestFile()
	defer os.Remove(filePath)

	fileUpload, err := protocol.UploadFile(ctx, sess, ln, groupID, protocol.ThreadTypeGroup, filePath)
	if err != nil {
		log.Fatalf("upload file: %v", err)
	}
	log.Printf("Upload OK: fileId=%s fileUrl=%s", fileUpload.FileID, truncate(fileUpload.FileURL, 60))

	fileMsgID, err := protocol.SendFile(ctx, sess, groupID, protocol.ThreadTypeGroup, fileUpload)
	if err != nil {
		log.Fatalf("send file: %v", err)
	}
	log.Println("OK, msgId:", fileMsgID)

	log.Println("\n=== ALL TESTS PASSED ===")
}

func createTestImage() string {
	// Minimal valid PNG: 1x1 red pixel
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, // 8-bit RGB
		0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41, 0x54, // IDAT chunk
		0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00, 0x00,
		0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc, 0x33,
		0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, // IEND chunk
		0xae, 0x42, 0x60, 0x82,
	}
	f, err := os.CreateTemp("", "goclaw_e2e_*.png")
	if err != nil {
		log.Fatal(err)
	}
	f.Write(png)
	f.Close()
	return f.Name()
}

func createTestFile() string {
	content := fmt.Sprintf("GoClaw E2E Test File\nTimestamp: %s\nThis file tests the file upload pipeline.\n", time.Now().Format(time.RFC3339))
	f, err := os.CreateTemp("", "goclaw_e2e_*.txt")
	if err != nil {
		log.Fatal(err)
	}
	f.WriteString(content)
	f.Close()
	return f.Name()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
