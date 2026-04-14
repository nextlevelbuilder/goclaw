package feishu

import (
	"bytes"
	"context"
	"testing"
)

// sendImage, sendFile, sendMarkdownCard, downloadMessageResource, uploadImage, uploadFile
// all delegate to LarkClient methods. These tests exercise the Channel-level wrappers.

func TestChannelSendImage(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"message_id":"om_img_1"}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	if err := ch.sendImage(context.Background(), "oc_chat_1", "chat_id", "img_key_abc", ""); err != nil {
		t.Fatalf("sendImage: %v", err)
	}
}

func TestChannelSendFile(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"message_id":"om_file_1"}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	if err := ch.sendFile(context.Background(), "oc_chat_1", "chat_id", "file_key_abc", "file", ""); err != nil {
		t.Fatalf("sendFile: %v", err)
	}
}

func TestChannelSendFile_DefaultMsgType(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"message_id":"om_file_2"}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	// Empty msgType → defaults to "file"
	if err := ch.sendFile(context.Background(), "oc_chat_1", "chat_id", "file_key_abc", "", ""); err != nil {
		t.Fatalf("sendFile (empty msgType): %v", err)
	}
}

func TestChannelSendMarkdownCard(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"message_id":"om_card_1"}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	if err := ch.sendMarkdownCard(context.Background(), "oc_chat_1", "chat_id", "**hello world**", "", nil); err != nil {
		t.Fatalf("sendMarkdownCard: %v", err)
	}
}

func TestChannelSendMarkdownCard_WithMetadata(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"message_id":"om_card_2"}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	meta := map[string]string{"key": "value"}
	if err := ch.sendMarkdownCard(context.Background(), "oc_chat_1", "chat_id", "text", "", meta); err != nil {
		t.Fatalf("sendMarkdownCard (metadata): %v", err)
	}
}

func TestChannelDownloadMessageResource(t *testing.T) {
	data := []byte("resource data")
	srv := newBinaryMockServer(t, data, "application/octet-stream")
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	got, _, err := ch.downloadMessageResource(context.Background(), "om_msg1", "fk_123", "file")
	if err != nil {
		t.Fatalf("downloadMessageResource: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("data mismatch: got %q, want %q", got, data)
	}
}

func TestChannelUploadImage(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"image_key":"img_uploaded_ch"}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	key, err := ch.uploadImage(context.Background(), bytes.NewReader([]byte{0xFF, 0xD8, 0xFF}))
	if err != nil {
		t.Fatalf("uploadImage: %v", err)
	}
	if key != "img_uploaded_ch" {
		t.Errorf("image_key: got %q, want img_uploaded_ch", key)
	}
}

func TestChannelUploadFile(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"file_key":"file_uploaded_ch"}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	key, err := ch.uploadFile(context.Background(), bytes.NewReader([]byte("pdf data")), "test.pdf", "pdf", 0)
	if err != nil {
		t.Fatalf("uploadFile: %v", err)
	}
	if key != "file_uploaded_ch" {
		t.Errorf("file_key: got %q, want file_uploaded_ch", key)
	}
}
