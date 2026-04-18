package wechat

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// generateClientID creates a unique client ID for outbound messages.
func generateClientID() string {
	return "goclaw-wechat-" + uuid.New().String()
}

// sendTextMessage sends a plain text message downstream.
func sendTextMessage(ctx context.Context, api *APIClient, to, text, contextToken string) (string, error) {
	clientID := generateClientID()

	var itemList []MessageItem
	if text != "" {
		itemList = []MessageItem{
			{Type: MessageItemTypeText, TextItem: &TextItem{Text: text}},
		}
	}

	req := &SendMessageReq{
		Msg: &WeixinMessage{
			FromUserID:   "",
			ToUserID:     to,
			ClientID:     clientID,
			MessageType:  MessageTypeBot,
			MessageState: MessageStateFinish,
			ItemList:     itemList,
			ContextToken: contextToken,
		},
	}

	if err := api.SendMessage(ctx, req); err != nil {
		slog.Error("wechat sendTextMessage failed", "to", to, "clientId", clientID, "error", err)
		return "", err
	}
	return clientID, nil
}

// sendImageMessage sends an image using a previously uploaded file.
func sendImageMessage(ctx context.Context, api *APIClient, to, text, contextToken string, uploaded *UploadedFileInfo) (string, error) {
	aesKeyBase64 := base64.StdEncoding.EncodeToString([]byte(uploaded.AesKeyHex))

	imageItem := MessageItem{
		Type: MessageItemTypeImage,
		ImageItem: &ImageItem{
			Media: &CDNMedia{
				EncryptQueryParam: uploaded.DownloadEncryptedQueryParam,
				AesKey:            aesKeyBase64,
				EncryptType:       1,
			},
			MidSize: uploaded.FileSizeCiphertext,
		},
	}

	return sendMediaItems(ctx, api, to, text, contextToken, imageItem, "sendImageMessage")
}

// sendVideoMessage sends a video using a previously uploaded file.
func sendVideoMessage(ctx context.Context, api *APIClient, to, text, contextToken string, uploaded *UploadedFileInfo) (string, error) {
	aesKeyBase64 := base64.StdEncoding.EncodeToString([]byte(uploaded.AesKeyHex))

	videoItem := MessageItem{
		Type: MessageItemTypeVideo,
		VideoItem: &VideoItem{
			Media: &CDNMedia{
				EncryptQueryParam: uploaded.DownloadEncryptedQueryParam,
				AesKey:            aesKeyBase64,
				EncryptType:       1,
			},
			VideoSize: uploaded.FileSizeCiphertext,
		},
	}

	return sendMediaItems(ctx, api, to, text, contextToken, videoItem, "sendVideoMessage")
}

// sendFileMessage sends a file attachment using a previously uploaded file.
func sendFileMessage(ctx context.Context, api *APIClient, to, text, contextToken, fileName string, uploaded *UploadedFileInfo) (string, error) {
	aesKeyBase64 := base64.StdEncoding.EncodeToString([]byte(uploaded.AesKeyHex))

	fileItem := MessageItem{
		Type: MessageItemTypeFile,
		FileItem: &FileItem{
			Media: &CDNMedia{
				EncryptQueryParam: uploaded.DownloadEncryptedQueryParam,
				AesKey:            aesKeyBase64,
				EncryptType:       1,
			},
			FileName: fileName,
			Len:      fmt.Sprintf("%d", uploaded.FileSize),
		},
	}

	return sendMediaItems(ctx, api, to, text, contextToken, fileItem, "sendFileMessage")
}

// sendMediaItems sends a media item optionally preceded by a text caption.
func sendMediaItems(ctx context.Context, api *APIClient, to, text, contextToken string, mediaItem MessageItem, label string) (string, error) {
	var items []MessageItem
	if text != "" {
		items = append(items, MessageItem{
			Type:     MessageItemTypeText,
			TextItem: &TextItem{Text: text},
		})
	}
	items = append(items, mediaItem)

	var lastClientID string
	for _, item := range items {
		lastClientID = generateClientID()
		req := &SendMessageReq{
			Msg: &WeixinMessage{
				FromUserID:   "",
				ToUserID:     to,
				ClientID:     lastClientID,
				MessageType:  MessageTypeBot,
				MessageState: MessageStateFinish,
				ItemList:     []MessageItem{item},
				ContextToken: contextToken,
			},
		}
		if err := api.SendMessage(ctx, req); err != nil {
			slog.Error("wechat "+label+" failed", "to", to, "clientId", lastClientID, "error", err)
			return "", err
		}
	}

	slog.Info("wechat "+label+" success", "to", to, "clientId", lastClientID)
	return lastClientID, nil
}

// sendMediaFile uploads a local file and sends it, routing by MIME type.
func sendMediaFile(ctx context.Context, api *APIClient, filePath, to, text, contextToken, cdnBaseURL string) (string, error) {
	mime := guessMimeFromFilename(filePath)

	if strings.HasPrefix(mime, "video/") {
		slog.Info("wechat sendMediaFile: uploading video", "path", filePath, "to", to)
		uploaded, err := uploadMediaToCdn(ctx, api, filePath, to, cdnBaseURL, UploadMediaTypeVideo, "uploadVideo")
		if err != nil {
			return "", err
		}
		return sendVideoMessage(ctx, api, to, text, contextToken, uploaded)
	}

	if strings.HasPrefix(mime, "image/") {
		slog.Info("wechat sendMediaFile: uploading image", "path", filePath, "to", to)
		uploaded, err := uploadMediaToCdn(ctx, api, filePath, to, cdnBaseURL, UploadMediaTypeImage, "uploadImage")
		if err != nil {
			return "", err
		}
		return sendImageMessage(ctx, api, to, text, contextToken, uploaded)
	}

	// File attachment
	fileName := filepath.Base(filePath)
	slog.Info("wechat sendMediaFile: uploading file", "path", filePath, "name", fileName, "to", to)
	uploaded, err := uploadMediaToCdn(ctx, api, filePath, to, cdnBaseURL, UploadMediaTypeFile, "uploadFile")
	if err != nil {
		return "", err
	}
	return sendFileMessage(ctx, api, to, text, contextToken, fileName, uploaded)
}

// guessMimeFromFilename returns a MIME type based on file extension.
func guessMimeFromFilename(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".mkv":
		return "video/x-matroska"
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg"
	case ".pdf":
		return "application/pdf"
	case ".doc", ".docx":
		return "application/msword"
	case ".xls", ".xlsx":
		return "application/vnd.ms-excel"
	case ".ppt", ".pptx":
		return "application/vnd.ms-powerpoint"
	case ".zip":
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}
