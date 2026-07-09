package dingtalk

import (
	"context"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	dtclient "github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
)

// streamTransport is the seam between the channel and DingTalk's Stream-mode
// socket. The channel depends on this interface, not on the SDK, so the inbound
// pipeline can be driven by a fake in tests without opening a connection.
type streamTransport interface {
	// Start dials and begins receiving. It is non-blocking: the SDK dials,
	// spawns its read and keepalive goroutines, and returns. An error means
	// the initial dial failed.
	Start(ctx context.Context) error

	// Close tears down the connection. Safe to call on an unstarted transport.
	Close()
}

// sdkTransport adapts the official DingTalk Go SDK to streamTransport.
//
// Everything the upstream TypeScript connector hand-rolls around its Node SDK —
// a 10s/20s heartbeat, exponential reconnect backoff, and a monkey-patch to
// silence the SDK's own console noise — exists to work around defects in that
// SDK. The Go SDK has none of them: NewStreamClient subscribes to the system
// "disconnect" and "ping" topics, runs a ping/pong watchdog on a 120s idle
// timer, and enables auto-reconnect, all by default. Layering a second
// heartbeat on the same socket would be worse than useless.
type sdkTransport struct {
	cli *dtclient.StreamClient
}

var _ streamTransport = (*sdkTransport)(nil)

// newSDKTransport builds a Stream-mode client for one app and routes inbound
// robot messages to h.
//
// endpoint overrides the OpenAPI host used to negotiate the socket; empty means
// the SDK default. WithAutoReconnect(true) and WithKeepAlive(120s) are already
// the SDK's defaults, so they are not passed here — doing so would imply they
// are not.
func newSDKTransport(clientID, clientSecret, endpoint string, h chatbot.IChatBotMessageHandler) *sdkTransport {
	opts := []dtclient.ClientOption{
		dtclient.WithAppCredential(dtclient.NewAppCredentialConfig(clientID, clientSecret)),
	}
	if endpoint != "" {
		opts = append(opts, dtclient.WithOpenApiHost(endpoint))
	}

	cli := dtclient.NewStreamClient(opts...)
	cli.RegisterChatBotCallbackRouter(h)
	return &sdkTransport{cli: cli}
}

func (t *sdkTransport) Start(ctx context.Context) error { return t.cli.Start(ctx) }

func (t *sdkTransport) Close() { t.cli.Close() }
