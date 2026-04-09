package googlechat

import "time"

const (
	typeGoogleChat               = "google_chat"
	chatAPIBase                  = "https://chat.googleapis.com/v1"
	pubsubAPIBase                = "https://pubsub.googleapis.com/v1"
	driveUploadBase              = "https://www.googleapis.com/upload/drive/v3"
	driveAPIBase                 = "https://www.googleapis.com/drive/v3"
	tokenEndpoint                = "https://oauth2.googleapis.com/token"
	googleChatMaxMessageBytes    = 3900
	longFormThresholdDefault     = 6000
	dedupTTL                     = 5 * time.Minute
	defaultPullInterval          = 1 * time.Second
	defaultPullMaxMessages       = 10
	defaultMediaMaxMB            = 20
	defaultFileRetentionDays     = 7
	shutdownDrainTimeout         = 5 * time.Second
	scopeChat                    = "https://www.googleapis.com/auth/chat.bot"
	scopePubSub                  = "https://www.googleapis.com/auth/pubsub"
	scopeDrive                   = "https://www.googleapis.com/auth/drive.file"
	retrySendMaxAttempts         = 5
	retrySendBaseDelay           = 1 * time.Second
	retrySendMaxDelay            = 30 * time.Second
	defaultStreamThrottle        = 1500 * time.Millisecond
)
