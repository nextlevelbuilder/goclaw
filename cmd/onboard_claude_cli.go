package cmd

import (
	"fmt"
	"os"
	"os/exec"
)

// runClaudeAuthLogin runs `claude auth login` interactively,
// connecting stdin/stdout/stderr to the terminal for the browser OAuth flow.
func runClaudeAuthLogin(cliPath string) error {
	fmt.Println("  Opening browser for Claude authentication...")
	cmd := exec.Command(cliPath, "auth", "login")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
