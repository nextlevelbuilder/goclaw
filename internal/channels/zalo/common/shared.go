package common

// WebhookPathPrefix is the single mount point for both Zalo channel flavors.
// Per-instance dispatch reads the slug suffix (e.g. "/channels/zalo/webhook/my-oa").
// The trailing slash makes ServeMux treat this as a prefix match.
const WebhookPathPrefix = "/channels/zalo/webhook/"

// WebhookPathBare is the no-slash form. Mount an explicit 404 handler here so
// http.ServeMux doesn't auto-301 to WebhookPathPrefix.
const WebhookPathBare = "/channels/zalo/webhook"

var sharedRouter = NewRouter()

// SharedRouter returns the process-global router.
func SharedRouter() *Router { return sharedRouter }
