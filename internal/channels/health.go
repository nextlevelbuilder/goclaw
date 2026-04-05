package channels

import (
	"strings"
	"time"
)

// ChannelHealthState captures the current runtime state of a channel instance.
type ChannelHealthState string

const (
	ChannelHealthStateRegistered ChannelHealthState = "registered"
	ChannelHealthStateStarting   ChannelHealthState = "starting"
	ChannelHealthStateHealthy    ChannelHealthState = "healthy"
	ChannelHealthStateDegraded   ChannelHealthState = "degraded"
	ChannelHealthStateFailed     ChannelHealthState = "failed"
	ChannelHealthStateStopped    ChannelHealthState = "stopped"
)

// ChannelFailureKind classifies the dominant cause of the current failure state.
type ChannelFailureKind string

const (
	ChannelFailureKindAuth    ChannelFailureKind = "auth"
	ChannelFailureKindConfig  ChannelFailureKind = "config"
	ChannelFailureKindNetwork ChannelFailureKind = "network"
	ChannelFailureKindUnknown ChannelFailureKind = "unknown"
)

// ChannelRemediationCode identifies the next operator step suggested for a channel incident.
type ChannelRemediationCode string

const (
	ChannelRemediationCodeReauth          ChannelRemediationCode = "reauth"
	ChannelRemediationCodeOpenCredentials ChannelRemediationCode = "open_credentials"
	ChannelRemediationCodeOpenAdvanced    ChannelRemediationCode = "open_advanced"
	ChannelRemediationCodeCheckNetwork    ChannelRemediationCode = "check_network"
)

// ChannelRemediationTarget tells the UI which existing surface can help resolve the issue.
type ChannelRemediationTarget string

const (
	ChannelRemediationTargetCredentials ChannelRemediationTarget = "credentials"
	ChannelRemediationTargetAdvanced    ChannelRemediationTarget = "advanced"
	ChannelRemediationTargetReauth      ChannelRemediationTarget = "reauth"
	ChannelRemediationTargetDetails     ChannelRemediationTarget = "details"
)

// ChannelRemediation contains a coarse, additive operator hint for the current incident.
type ChannelRemediation struct {
	Code     ChannelRemediationCode   `json:"code"`
	Headline string                   `json:"headline"`
	Hint     string                   `json:"hint,omitempty"`
	Target   ChannelRemediationTarget `json:"target,omitempty"`
}

// ChannelHealth is the shared runtime health snapshot exposed via channels.status.
type ChannelHealth struct {
	ChannelType         string              `json:"-"`
	Enabled             bool                `json:"enabled"`
	Running             bool                `json:"running"`
	State               ChannelHealthState  `json:"state"`
	Summary             string              `json:"summary,omitempty"`
	Detail              string              `json:"detail,omitempty"`
	FailureKind         ChannelFailureKind  `json:"failure_kind,omitempty"`
	Retryable           bool                `json:"retryable"`
	CheckedAt           time.Time           `json:"checked_at,omitempty"`
	FailureCount        int                 `json:"failure_count,omitempty"`
	ConsecutiveFailures int                 `json:"consecutive_failures,omitempty"`
	FirstFailedAt       time.Time           `json:"first_failed_at,omitempty"`
	LastFailedAt        time.Time           `json:"last_failed_at,omitempty"`
	LastHealthyAt       time.Time           `json:"last_healthy_at,omitempty"`
	Remediation         *ChannelRemediation `json:"remediation,omitempty"`
}

// ChannelErrorInfo contains shared error classification output for operators.
type ChannelErrorInfo struct {
	Summary   string
	Detail    string
	Kind      ChannelFailureKind
	Retryable bool
}

// ClassifyChannelError maps common channel startup/runtime failures into operator-facing buckets.
// This is a best-effort classification based on error message patterns from upstream libraries
// (telego, discordgo, etc.). If upstream changes error format, the default case returns
// "unknown + retryable", so misclassification degrades gracefully to generic guidance.
// TODO: Use errors.As() where upstream libraries expose typed errors.
func ClassifyChannelError(err error) ChannelErrorInfo {
	if err == nil {
		return ChannelErrorInfo{
			Summary:   "Channel failed",
			Detail:    "GoClaw could not determine the latest channel error.",
			Kind:      ChannelFailureKindUnknown,
			Retryable: true,
		}
	}

	detail := err.Error()
	msg := strings.ToLower(detail)

	switch {
	case strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden"):
		return ChannelErrorInfo{
			Summary:   "Authentication failed",
			Detail:    "The upstream service rejected the configured credentials or session.",
			Kind:      ChannelFailureKindAuth,
			Retryable: false,
		}
	case strings.Contains(msg, "invalid proxy"):
		return ChannelErrorInfo{
			Summary:   "Configuration is invalid",
			Detail:    "Configured proxy URL is invalid.",
			Kind:      ChannelFailureKindConfig,
			Retryable: false,
		}
	case strings.Contains(msg, "agent ") && strings.Contains(msg, " not found for channel"):
		return ChannelErrorInfo{
			Summary:   "Configuration is invalid",
			Detail:    "The linked agent for this channel could not be found.",
			Kind:      ChannelFailureKindConfig,
			Retryable: false,
		}
	case strings.Contains(msg, "token is required"),
		strings.Contains(msg, "missing credentials"),
		strings.Contains(msg, "decode "),
		strings.Contains(msg, "not found for channel"),
		strings.Contains(msg, "required"):
		safeDetail := "A required channel setting is missing or invalid."
		switch {
		case strings.Contains(msg, "token is required"), strings.Contains(msg, "missing credentials"):
			safeDetail = "Required channel credentials are missing or incomplete."
		case strings.Contains(msg, "decode "):
			safeDetail = "Saved channel configuration could not be parsed."
		}
		return ChannelErrorInfo{
			Summary:   "Configuration is invalid",
			Detail:    safeDetail,
			Kind:      ChannelFailureKindConfig,
			Retryable: false,
		}
	case strings.Contains(msg, "timeout"),
		strings.Contains(msg, "i/o timeout"),
		strings.Contains(msg, "deadline exceeded"),
		strings.Contains(msg, "context deadline exceeded"):
		return ChannelErrorInfo{
			Summary:   "Network error",
			Detail:    "Timed out while reaching the upstream service.",
			Kind:      ChannelFailureKindNetwork,
			Retryable: true,
		}
	case strings.Contains(msg, "connection refused"):
		return ChannelErrorInfo{
			Summary:   "Network error",
			Detail:    "The upstream service refused the connection attempt.",
			Kind:      ChannelFailureKindNetwork,
			Retryable: true,
		}
	case strings.Contains(msg, "no such host"):
		return ChannelErrorInfo{
			Summary:   "Network error",
			Detail:    "GoClaw could not resolve the upstream host.",
			Kind:      ChannelFailureKindNetwork,
			Retryable: true,
		}
	case strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "eof"):
		return ChannelErrorInfo{
			Summary:   "Network error",
			Detail:    "The upstream service closed the connection unexpectedly.",
			Kind:      ChannelFailureKindNetwork,
			Retryable: true,
		}
	case strings.Contains(msg, "dial tcp"),
		strings.Contains(msg, "tcp "):
		return ChannelErrorInfo{
			Summary:   "Network error",
			Detail:    "GoClaw could not open a network connection to the upstream service.",
			Kind:      ChannelFailureKindNetwork,
			Retryable: true,
		}
	default:
		return ChannelErrorInfo{
			Summary:   "Channel failed",
			Detail:    "An unexpected channel error occurred. Review server logs for the full error.",
			Kind:      ChannelFailureKindUnknown,
			Retryable: true,
		}
	}
}

// NewChannelHealth builds a shared runtime snapshot with a current timestamp.
func NewChannelHealth(state ChannelHealthState, summary, detail string, kind ChannelFailureKind, retryable bool) ChannelHealth {
	return NewChannelHealthForType("", state, summary, detail, kind, retryable)
}

// NewChannelHealthForType builds a shared runtime snapshot for a specific channel type.
func NewChannelHealthForType(channelType string, state ChannelHealthState, summary, detail string, kind ChannelFailureKind, retryable bool) ChannelHealth {
	return ChannelHealth{
		ChannelType: channelType,
		Enabled:     true,
		Running:     state == ChannelHealthStateHealthy || state == ChannelHealthStateDegraded,
		State:       state,
		Summary:     summary,
		Detail:      detail,
		FailureKind: kind,
		Retryable:   retryable,
		CheckedAt:   time.Now().UTC(),
	}
}

// NewFailedChannelHealth builds a failed snapshot from a classified error.
func NewFailedChannelHealth(summary string, err error) ChannelHealth {
	return NewFailedChannelHealthForType("", summary, err)
}

// NewFailedChannelHealthForType builds a failed snapshot from a classified error for one channel type.
func NewFailedChannelHealthForType(channelType, summary string, err error) ChannelHealth {
	info := ClassifyChannelError(err)
	if summary == "" {
		summary = info.Summary
	}
	return NewChannelHealthForType(channelType, ChannelHealthStateFailed, summary, info.Detail, info.Kind, info.Retryable)
}

func isFailureState(state ChannelHealthState) bool {
	return state == ChannelHealthStateFailed || state == ChannelHealthStateDegraded
}

func mergeChannelHealth(previous, snapshot ChannelHealth) ChannelHealth {
	if snapshot.CheckedAt.IsZero() {
		snapshot.CheckedAt = time.Now().UTC()
	}
	if !snapshot.Enabled {
		snapshot.Enabled = true
	}
	if snapshot.ChannelType == "" {
		snapshot.ChannelType = previous.ChannelType
	}

	if isFailureState(snapshot.State) {
		if snapshot.FailureCount == 0 {
			snapshot.FailureCount = previous.FailureCount + 1
		}
		if snapshot.ConsecutiveFailures == 0 {
			snapshot.ConsecutiveFailures = previous.ConsecutiveFailures + 1
		}
		if snapshot.FirstFailedAt.IsZero() {
			if previous.FirstFailedAt.IsZero() || !isFailureState(previous.State) {
				snapshot.FirstFailedAt = snapshot.CheckedAt
			} else {
				snapshot.FirstFailedAt = previous.FirstFailedAt
			}
		}
		if snapshot.LastFailedAt.IsZero() {
			snapshot.LastFailedAt = snapshot.CheckedAt
		}
		if snapshot.LastHealthyAt.IsZero() {
			snapshot.LastHealthyAt = previous.LastHealthyAt
		}
	} else {
		if snapshot.FailureCount == 0 {
			snapshot.FailureCount = previous.FailureCount
		}
		snapshot.ConsecutiveFailures = 0
		snapshot.FirstFailedAt = time.Time{}
		if snapshot.LastFailedAt.IsZero() {
			snapshot.LastFailedAt = previous.LastFailedAt
		}
		if snapshot.State == ChannelHealthStateHealthy {
			snapshot.LastHealthyAt = snapshot.CheckedAt
		} else if snapshot.LastHealthyAt.IsZero() {
			snapshot.LastHealthyAt = previous.LastHealthyAt
		}
	}

	snapshot.Remediation = buildChannelRemediation(snapshot)
	return snapshot
}

func buildChannelRemediation(snapshot ChannelHealth) *ChannelRemediation {
	if !isFailureState(snapshot.State) {
		return nil
	}

	text := strings.ToLower(snapshot.Summary + " " + snapshot.Detail)

	switch snapshot.FailureKind {
	case ChannelFailureKindAuth:
		if snapshot.ChannelType == TypeZaloPersonal {
			return &ChannelRemediation{
				Code:     ChannelRemediationCodeReauth,
				Headline: "Reconnect the channel session",
				Hint:     "Open the sign-in flow again to restore the current session.",
				Target:   ChannelRemediationTargetReauth,
			}
		}
		return &ChannelRemediation{
			Code:     ChannelRemediationCodeOpenCredentials,
			Headline: "Review channel credentials",
			Hint:     "Open credentials and confirm the current token or secret is still valid.",
			Target:   ChannelRemediationTargetCredentials,
		}
	case ChannelFailureKindConfig:
		if strings.Contains(text, "credential") ||
			strings.Contains(text, "token") ||
			strings.Contains(text, "secret") ||
			strings.Contains(text, "app_id") ||
			strings.Contains(text, "app id") ||
			strings.Contains(text, "required") {
			return &ChannelRemediation{
				Code:     ChannelRemediationCodeOpenCredentials,
				Headline: "Complete required credentials",
				Hint:     "Open credentials and fill the missing or invalid values for this channel.",
				Target:   ChannelRemediationTargetCredentials,
			}
		}
		return &ChannelRemediation{
			Code:     ChannelRemediationCodeOpenAdvanced,
			Headline: "Review channel settings",
			Hint:     "Open advanced settings and correct the invalid channel configuration.",
			Target:   ChannelRemediationTargetAdvanced,
		}
	case ChannelFailureKindNetwork:
		return &ChannelRemediation{
			Code:     ChannelRemediationCodeCheckNetwork,
			Headline: "Check upstream reachability",
			Hint:     "Verify the upstream service is reachable from GoClaw, then inspect proxy or API server settings if you use them.",
			Target:   ChannelRemediationTargetDetails,
		}
	default:
		if snapshot.Retryable {
			return &ChannelRemediation{
				Code:     ChannelRemediationCodeCheckNetwork,
				Headline: "Inspect the latest failure",
				Hint:     "Open the channel details and review the latest runtime error before retrying.",
				Target:   ChannelRemediationTargetDetails,
			}
		}
		return &ChannelRemediation{
			Code:     ChannelRemediationCodeOpenAdvanced,
			Headline: "Review channel settings",
			Hint:     "Open channel settings and inspect the latest error detail.",
			Target:   ChannelRemediationTargetAdvanced,
		}
	}
}
