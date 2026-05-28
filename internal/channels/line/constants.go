package line

const (
	maxTextLength    = 5000
	maxReplyMessages = 5
	replyTokenTTL    = 25 // seconds, buffer before 30s expiry
	loadingSeconds   = 60
	loadingAPIURL    = "https://api.line.me/v2/bot/chat/loading/start"
)
