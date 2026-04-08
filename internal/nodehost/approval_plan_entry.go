package nodehost

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MutableFileOperandSnapshotResult is the result of resolving a mutable file operand.
type MutableFileOperandSnapshotResult struct {
	Ok       bool
	Snapshot *SystemRunApprovalFileOperand
	Message  string // error message when !Ok
}

// ResolveMutableFileOperandSnapshot resolves the mutable file operand for an argv.
func ResolveMutableFileOperandSnapshot(argv []string, cwd string, shellCommand *string) MutableFileOperandSnapshotResult {
	argvIndex := resolveMutableFileOperandIndex(argv, cwd)
	if argvIndex == nil {
		if requiresStableInterpreterBinding(argv, shellCommand, cwd) {
			return MutableFileOperandSnapshotResult{
				Ok:      false,
				Message: "SYSTEM_RUN_DENIED: approval cannot safely bind this interpreter/runtime command",
			}
		}
		return MutableFileOperandSnapshotResult{Ok: true}
	}
	rawOperand := strings.TrimSpace(safeIndex(argv, *argvIndex))
	if rawOperand == "" {
		return MutableFileOperandSnapshotResult{
			Ok:      false,
			Message: "SYSTEM_RUN_DENIED: approval requires a stable script operand",
		}
	}
	resolved := rawOperand
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(cwd, resolved)
	}
	realPath, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return MutableFileOperandSnapshotResult{
			Ok:      false,
			Message: "SYSTEM_RUN_DENIED: approval requires an existing script operand",
		}
	}
	info, err := os.Stat(realPath)
	if err != nil || !info.Mode().IsRegular() {
		return MutableFileOperandSnapshotResult{
			Ok:      false,
			Message: "SYSTEM_RUN_DENIED: approval requires a file script operand",
		}
	}
	hash, err := hashFileContents(realPath)
	if err != nil {
		return MutableFileOperandSnapshotResult{
			Ok:      false,
			Message: "SYSTEM_RUN_DENIED: approval requires a readable script operand",
		}
	}
	return MutableFileOperandSnapshotResult{
		Ok: true,
		Snapshot: &SystemRunApprovalFileOperand{
			ArgvIndex: *argvIndex,
			Path:      realPath,
			SHA256:    hash,
		},
	}
}

func requiresStableInterpreterBinding(argv []string, shellCommand *string, cwd string) bool {
	if shellCommand != nil {
		return shellPayloadNeedsStableBinding(*shellCommand, cwd)
	}
	unwrapped, _ := unwrapArgvForMutableOperand(argv)
	exe := NormalizeExecutableToken(safeIndex(unwrapped, 0))
	if exe == "" {
		return false
	}
	if PosixShellWrappers[exe] {
		return false
	}
	return isMutableScriptRunner(exe)
}

func shellPayloadNeedsStableBinding(shellCmd, cwd string) bool {
	argv := SplitShellArgs(shellCmd)
	if len(argv) == 0 {
		return false
	}
	snap := ResolveMutableFileOperandSnapshot(argv, cwd, nil)
	if !snap.Ok {
		return true
	}
	if snap.Snapshot != nil {
		return true
	}
	firstToken := strings.TrimSpace(safeIndex(argv, 0))
	return resolvesToExistingFile(firstToken, cwd)
}

// --- CWD snapshot ---

// ResolveCanonicalApprovalCwd creates a canonical CWD snapshot.
// Validates no mutable symlink path components and cross-checks file identity.
func ResolveCanonicalApprovalCwd(cwd string) (*ApprovedCwdSnapshot, string) {
	requested := filepath.Clean(cwd)
	if !filepath.IsAbs(requested) {
		abs, err := filepath.Abs(requested)
		if err != nil {
			return nil, "SYSTEM_RUN_DENIED: approval requires an existing canonical cwd"
		}
		requested = abs
	}

	cwdLstat, err := os.Lstat(requested)
	if err != nil {
		return nil, "SYSTEM_RUN_DENIED: approval requires an existing canonical cwd"
	}
	cwdStat, err := os.Stat(requested)
	if err != nil || !cwdStat.IsDir() {
		return nil, "SYSTEM_RUN_DENIED: approval requires cwd to be a directory"
	}
	realPath, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return nil, "SYSTEM_RUN_DENIED: approval requires an existing canonical cwd"
	}
	cwdRealStat, err := GetFileIdentity(realPath)
	if err != nil {
		return nil, "SYSTEM_RUN_DENIED: approval requires an existing canonical cwd"
	}

	// Check for mutable symlink path components (TOCTOU defense).
	if hasMutableSymlinkPathComponent(requested) {
		return nil, "SYSTEM_RUN_DENIED: approval requires canonical cwd (no symlink path components)"
	}

	// Direct symlink check on the CWD itself.
	if cwdLstat.Mode()&os.ModeSymlink != 0 {
		return nil, "SYSTEM_RUN_DENIED: approval requires canonical cwd (no symlink cwd)"
	}

	// Three-way identity cross-check (defense against bind mount/hardlink manipulation).
	cwdStatIdentity, err := GetFileIdentity(requested)
	if err != nil {
		return nil, "SYSTEM_RUN_DENIED: approval requires an existing canonical cwd"
	}
	cwdLstatIdentity := cwdStatIdentity // lstat on non-symlink = stat
	if !SameFileIdentity(cwdStatIdentity, cwdLstatIdentity) ||
		!SameFileIdentity(cwdStatIdentity, cwdRealStat) ||
		!SameFileIdentity(cwdLstatIdentity, cwdRealStat) {
		return nil, "SYSTEM_RUN_DENIED: approval cwd identity mismatch"
	}

	return &ApprovedCwdSnapshot{Cwd: realPath, Stat: cwdRealStat}, ""
}

// hasMutableSymlinkPathComponent walks from root to target, checking each
// path component for writable symlinks (TOCTOU defense).
func hasMutableSymlinkPathComponent(targetPath string) bool {
	abs, err := filepath.Abs(targetPath)
	if err != nil {
		return true
	}
	parts := pathComponentsFromRoot(abs)
	for _, component := range parts {
		info, err := os.Lstat(component)
		if err != nil {
			return true // fail closed
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		parentDir := filepath.Dir(component)
		if isWritableByCurrentProcess(parentDir) {
			return true
		}
	}
	return false
}

// pathComponentsFromRoot returns all path components from root to target.
func pathComponentsFromRoot(targetPath string) []string {
	var parts []string
	cursor := targetPath
	for {
		parts = append([]string{cursor}, parts...)
		parent := filepath.Dir(cursor)
		if parent == cursor {
			return parts
		}
		cursor = parent
	}
}

// isWritableByCurrentProcess checks if the current process can write to a path.
func isWritableByCurrentProcess(path string) bool {
	// Try to open for writing. This is the most reliable cross-platform check.
	f, err := os.OpenFile(filepath.Join(path, ".goclaw-write-test"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// RevalidateApprovedCwdSnapshot checks that a CWD snapshot is still valid.
func RevalidateApprovedCwdSnapshot(snapshot *ApprovedCwdSnapshot) bool {
	current, msg := ResolveCanonicalApprovalCwd(snapshot.Cwd)
	if msg != "" || current == nil {
		return false
	}
	return SameFileIdentity(snapshot.Stat, current.Stat)
}

// RevalidateApprovedMutableFileOperand checks that a file operand hasn't changed.
func RevalidateApprovedMutableFileOperand(snapshot *SystemRunApprovalFileOperand, argv []string, cwd string) bool {
	operand := strings.TrimSpace(safeIndex(argv, snapshot.ArgvIndex))
	if operand == "" {
		return false
	}
	resolved := operand
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(cwd, resolved)
	}
	realPath, err := filepath.EvalSymlinks(resolved)
	if err != nil || realPath != snapshot.Path {
		return false
	}
	hash, err := hashFileContents(realPath)
	if err != nil {
		return false
	}
	return hash == snapshot.SHA256
}

// --- Executable path hardening ---

// HardenResult is the result of hardening approved execution paths.
type HardenResult struct {
	Ok                  bool
	Argv                []string
	ArgvChanged         bool
	Cwd                 string
	ApprovedCwdSnapshot *ApprovedCwdSnapshot
	Message             string // error message when !Ok
}

// HardenApprovedExecutionPaths pins executable paths and CWD for approval.
func HardenApprovedExecutionPaths(approvedByAsk bool, argv []string, shellCommand *string, cwd string) HardenResult {
	if !approvedByAsk {
		return HardenResult{Ok: true, Argv: argv, Cwd: cwd}
	}

	hardenedCwd := cwd
	var cwdSnapshot *ApprovedCwdSnapshot
	if hardenedCwd != "" {
		snap, msg := ResolveCanonicalApprovalCwd(hardenedCwd)
		if msg != "" {
			return HardenResult{Ok: false, Message: msg}
		}
		hardenedCwd = snap.Cwd
		cwdSnapshot = snap
	}

	if len(argv) == 0 {
		return HardenResult{Ok: true, Argv: argv, Cwd: hardenedCwd, ApprovedCwdSnapshot: cwdSnapshot}
	}

	// Check if we should pin the executable.
	wrapperChain := resolveWrapperChain(argv)
	if shellCommand != nil || len(wrapperChain) > 0 {
		return HardenResult{Ok: true, Argv: argv, Cwd: hardenedCwd, ApprovedCwdSnapshot: cwdSnapshot}
	}

	// Resolve the executable to an absolute path.
	pinnedExe := resolveExecutablePath(argv[0], hardenedCwd)
	if pinnedExe == "" {
		return HardenResult{Ok: false, Message: "SYSTEM_RUN_DENIED: approval requires a stable executable path"}
	}
	if pinnedExe == argv[0] {
		return HardenResult{Ok: true, Argv: argv, Cwd: hardenedCwd, ApprovedCwdSnapshot: cwdSnapshot}
	}

	newArgv := make([]string, len(argv))
	copy(newArgv, argv)
	newArgv[0] = pinnedExe
	return HardenResult{Ok: true, Argv: newArgv, ArgvChanged: true, Cwd: hardenedCwd, ApprovedCwdSnapshot: cwdSnapshot}
}

func resolveWrapperChain(argv []string) []string {
	var wrappers []string
	current := argv
	for range MaxDispatchWrapperDepth {
		r := UnwrapKnownDispatchWrapperInvocation(current)
		if r.Kind != "unwrapped" || len(r.Argv) == 0 {
			break
		}
		wrappers = append(wrappers, r.Wrapper)
		current = r.Argv
	}
	return wrappers
}

func resolveExecutablePath(token, cwd string) string {
	// If already absolute, resolve symlinks.
	if filepath.IsAbs(token) {
		real, err := filepath.EvalSymlinks(token)
		if err != nil {
			return ""
		}
		return real
	}
	// Try exec.LookPath for PATH-based resolution.
	resolved, err := exec.LookPath(token)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(cwd, resolved)
	}
	real, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return ""
	}
	return real
}

// --- Build approval plan (public entry point) ---

// ApprovalPlanResult is the result of building an approval plan.
type ApprovalPlanResult struct {
	Ok      bool
	Plan    *SystemRunApprovalPlan
	Message string
}

// BuildSystemRunApprovalPlan builds a complete approval plan for a system run command.
func BuildSystemRunApprovalPlan(command []string, rawCommand *string, cwd *string, agentID *string, sessionKey *string) ApprovalPlanResult {
	resolved := ResolveSystemRunCommandRequest(command, rawCommand)
	if !resolved.Ok {
		return ApprovalPlanResult{Ok: false, Message: resolved.Message}
	}
	if len(resolved.Argv) == 0 {
		return ApprovalPlanResult{Ok: false, Message: "command required"}
	}

	effectiveCwd := ""
	if cwd != nil {
		effectiveCwd = strings.TrimSpace(*cwd)
	}

	hardening := HardenApprovedExecutionPaths(true, resolved.Argv, resolved.ShellPayload, effectiveCwd)
	if !hardening.Ok {
		return ApprovalPlanResult{Ok: false, Message: hardening.Message}
	}

	commandText := FormatExecCommand(hardening.Argv)
	var commandPreview *string
	if resolved.PreviewText != nil {
		trimmed := strings.TrimSpace(*resolved.PreviewText)
		if trimmed != "" && trimmed != commandText {
			commandPreview = &trimmed
		}
	}

	mutableOp := ResolveMutableFileOperandSnapshot(hardening.Argv, hardening.Cwd, resolved.ShellPayload)
	if !mutableOp.Ok {
		return ApprovalPlanResult{Ok: false, Message: mutableOp.Message}
	}

	var cwdPtr *string
	if hardening.Cwd != "" {
		cwdPtr = &hardening.Cwd
	}

	return ApprovalPlanResult{
		Ok: true,
		Plan: &SystemRunApprovalPlan{
			Argv:               hardening.Argv,
			Cwd:                cwdPtr,
			CommandText:        commandText,
			CommandPreview:     commandPreview,
			AgentID:            agentID,
			SessionKey:         sessionKey,
			MutableFileOperand: mutableOp.Snapshot,
		},
	}
}
