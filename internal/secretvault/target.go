package secretvault

// SecretTarget marks a tool that resolves {{secret:name}} placeholders itself,
// inside Execute, via ResolveArgs. It is detected by type assertion on the tool
// instance, never by name, so per-user MCP tools and wrappers behave the same.
//
// A tool that does not implement it receives placeholders verbatim, which is
// harmless: only the vault can turn a placeholder into a value.
type SecretTarget interface {
	// SecretKinds lists the secret kinds this tool may consume.
	SecretKinds() []string
}

// IsSecretTarget reports whether t is a vault-aware tool.
func IsSecretTarget(t any) bool {
	_, ok := t.(SecretTarget)
	return ok
}
