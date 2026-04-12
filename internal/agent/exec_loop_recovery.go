package agent

import (
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func recoverRepeatedExecLoop(registryName string, args map[string]any, result *tools.Result) string {
	if result == nil {
		return ""
	}
	if registryName != "exec" && registryName != "bash" {
		return ""
	}
	command, _ := args["command"].(string)
	if command == "" {
		return ""
	}
	if !looksLikeEnvironmentProbe(command) && !tools.IsReadOnlyCommand(command) {
		return ""
	}

	output := strings.TrimSpace(result.ForLLM)
	if output == "" {
		return ""
	}
	return fmt.Sprintf("I already ran `%s` and got this result:\n\n```text\n%s\n```", command, output)
}
