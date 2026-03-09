package tools

import (
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// Result is the unified return type from tool execution.
type Result struct {
	ForLLM  string `json:"for_llm"`            // content sent to the LLM
	ForUser string `json:"for_user,omitempty"` // content shown to the user
	Silent  bool   `json:"silent"`             // suppress user message
	IsError bool   `json:"is_error"`           // marks error
	Async   bool   `json:"async"`              // running asynchronously
	Err     error  `json:"-"`                  // internal error (not serialized)

	// Media holds media files to forward as output (e.g. images from delegation).
	Media []bus.MediaFile `json:"-"`

	// Deliverable holds the primary work output from this tool execution.
	// Used to capture actual content (e.g. written file text, image prompt) for team
	// task results instead of relying on the LLM's summary response.
	Deliverable string `json:"-"`

	// Usage holds token usage from tools that make internal LLM calls (e.g. read_image).
	// When set, the agent loop records these on the tool span for tracing.
	Usage    *providers.Usage `json:"-"`
	Provider string           `json:"-"` // provider name (for tool span metadata)
	Model    string           `json:"-"` // model used (for tool span metadata)
}

func NewResult(forLLM string) *Result {
	return &Result{ForLLM: forLLM}
}

func SilentResult(forLLM string) *Result {
	return &Result{ForLLM: forLLM, Silent: true}
}

func ErrorResult(message string) *Result {
	return &Result{ForLLM: message, IsError: true}
}

func UserResult(content string) *Result {
	return &Result{ForLLM: content, ForUser: content}
}

func AsyncResult(message string) *Result {
	return &Result{ForLLM: message, Async: true}
}

func (r *Result) WithError(err error) *Result {
	r.Err = err
	return r
}
