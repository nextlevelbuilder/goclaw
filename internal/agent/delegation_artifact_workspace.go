package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

func isExistingRealDirectory(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func validateDelegationArtifactWorkspace(delegationID, inputsPath, outputsPath string) bool {
	parsedID, err := uuid.Parse(delegationID)
	if err != nil || parsedID == uuid.Nil ||
		!isExistingRealDirectory(inputsPath) ||
		!isExistingRealDirectory(outputsPath) {
		return false
	}
	inputsReal, err := filepath.EvalSymlinks(filepath.Clean(inputsPath))
	if err != nil {
		return false
	}
	outputsReal, err := filepath.EvalSymlinks(filepath.Clean(outputsPath))
	if err != nil {
		return false
	}
	exchangeRoot := filepath.Dir(inputsReal)
	return filepath.Base(inputsReal) == "inputs" &&
		filepath.Base(outputsReal) == "outputs" &&
		filepath.Dir(outputsReal) == exchangeRoot &&
		filepath.Base(exchangeRoot) == parsedID.String() &&
		filepath.Base(filepath.Dir(exchangeRoot)) == "delegations" &&
		filepath.Base(filepath.Dir(filepath.Dir(exchangeRoot))) == "collaboration"
}

func delegationArtifactTextRedactor(req *RunRequest) tracing.TextRedactor {
	if req == nil || req.RunKind != "delegate" ||
		req.DelegateInputsPath == "" || req.DelegateOutputsPath == "" {
		return nil
	}
	type replacement struct {
		value string
		alias string
	}
	var replacements []replacement
	seen := make(map[string]struct{})
	add := func(value, alias string) {
		for _, variant := range []string{
			value,
			filepath.ToSlash(value),
			strings.Trim(strconv.Quote(value), `"`),
		} {
			if variant == "" {
				continue
			}
			if _, exists := seen[variant]; exists {
				continue
			}
			seen[variant] = struct{}{}
			replacements = append(replacements, replacement{value: variant, alias: alias})
		}
	}
	add(req.DelegateInputsPath, "inputs")
	add(req.DelegateOutputsPath, "outputs")
	if root := filepath.Dir(req.DelegateInputsPath); root == filepath.Dir(req.DelegateOutputsPath) {
		add(root, "delegation exchange")
	}
	sort.Slice(replacements, func(i, j int) bool {
		return len(replacements[i].value) > len(replacements[j].value)
	})
	pairs := make([]string, 0, len(replacements)*2)
	for _, replacement := range replacements {
		pairs = append(pairs, replacement.value, replacement.alias)
	}
	replacer := strings.NewReplacer(pairs...)
	return replacer.Replace
}

func withDelegationArtifactTextRedactor(ctx context.Context, req *RunRequest) context.Context {
	return tracing.WithTextRedactor(ctx, delegationArtifactTextRedactor(req))
}

func redactDelegationAgentEvent(req *RunRequest, event AgentEvent) AgentEvent {
	redactor := delegationArtifactTextRedactor(req)
	event.Payload = tracing.RedactValueWith(redactor, event.Payload)
	return event
}

func redactDelegationMessage(req *RunRequest, message providers.Message) providers.Message {
	redactor := delegationArtifactTextRedactor(req)
	if redactor == nil {
		return message
	}
	message.Content = redactor(message.Content)
	message.Thinking = redactor(message.Thinking)
	if len(message.RawAssistantContent) > 0 {
		message.RawAssistantContent = json.RawMessage(redactor(string(message.RawAssistantContent)))
	}
	for i := range message.ToolCalls {
		if arguments, ok := tracing.RedactValueWith(redactor, message.ToolCalls[i].Arguments).(map[string]any); ok {
			message.ToolCalls[i].Arguments = arguments
		}
		message.ToolCalls[i].ParseError = redactor(message.ToolCalls[i].ParseError)
	}
	// Pre-publication paths are ephemeral and cannot be durable session media.
	message.MediaRefs = nil
	return message
}

func redactDelegationRunResult(req *RunRequest, result *RunResult) *RunResult {
	if result == nil {
		return nil
	}
	redactor := delegationArtifactTextRedactor(req)
	if redactor == nil {
		return result
	}
	result.Content = redactor(result.Content)
	result.Thinking = redactor(result.Thinking)
	result.LastBlockReply = redactor(result.LastBlockReply)
	for i := range result.Deliverables {
		result.Deliverables[i] = redactor(result.Deliverables[i])
	}
	// The outer artifact publisher is the only egress path.
	result.Media = nil
	return result
}
