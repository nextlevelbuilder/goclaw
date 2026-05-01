package telegram

import "time"

const (
	// telegramMaxMessageLen is the safe limit for Telegram messages.
	// Telegram's hard limit is 4096, but we use 4000 for safety (matching TS textChunkLimit).
	telegramMaxMessageLen = 4000

	// telegramCaptionMaxLen is the max length for media captions.
	telegramCaptionMaxLen = 1024

	// pairingReplyDebounce is the minimum interval between pairing replies to the same user.
	pairingReplyDebounce = 60 * time.Second

	// sendOverallTimeout is the maximum duration for a multi-retry text send
	// sequence. Text rarely needs more than a couple of seconds per attempt.
	sendOverallTimeout = 60 * time.Second

	// sendMediaOverallTimeout covers photo/video/audio/document uploads, where
	// a single attempt can take 30–90s on slow/mobile networks for larger
	// files. 60s was triggering "context deadline exceeded" mid-upload (#628)
	// for multi-MB sketchnote/PDF attachments. 3 min gives three realistic
	// retry attempts even on poor links.
	sendMediaOverallTimeout = 3 * time.Minute

	// probeOverallTimeout is the maximum duration for initial bot status check and command sync.
	probeOverallTimeout = 60 * time.Second
)
