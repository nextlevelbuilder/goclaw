// Phase 5: typed credential write path for SecureCLIUserCredentials.
//
// The legacy PUT body `{env: {...}}` lives untouched in
// secure_cli_user_credentials.go; this file adds the new
// `{credential_type, host_scope, blob}` branches for typed adapters
// (pat / ssh_key). All validation runs BEFORE encryption + DB write so the
// store row never holds a malformed credential.
package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"golang.org/x/net/idna"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// hostScopeRe matches lowercase hostnames with optional port. Applied AFTER
// idna.ToASCII normalization so IDN punycode is treated as plain ASCII.
// Restrictive on purpose — prevents CRLF injection (\r\n) and weird Unicode
// from landing in the host_scope column and later being used in a `git config`
// key like http.https://<host>/.extraheader (Phase 3 wire shape).
var hostScopeRe = regexp.MustCompile(`^[a-z0-9.-]+(:[0-9]+)?$`)

// typedCredentialBody is the additive PUT shape introduced in Phase 5.
// All three new fields are nullable so the legacy `{env: {...}}` path keeps
// working unchanged.
type typedCredentialBody struct {
	CredentialType *string         `json:"credential_type,omitempty"`
	HostScope      *string         `json:"host_scope,omitempty"`
	Blob           json.RawMessage `json:"blob,omitempty"`
}

// errTypedCredential carries both an HTTP status and the i18n-resolved
// message so handleSetUserCredentials can `writeJSON` once without leaking
// validation details through fmt.Sprintf.
type errTypedCredential struct {
	status int
	msg    string
	// errorKey is exposed to the frontend so dialogs can map specific failures
	// (e.g. passphrase rejection) to inline UI feedback without parsing the
	// localized human message.
	errorKey string
}

func (e *errTypedCredential) Error() string { return e.msg }

// prepareTypedCredentialEnv validates and converts the new payload shape into
// the bytes that get encrypted-and-stored. Returns (envBytes, credentialType,
// hostScope, error). On success, envBytes is the JSON-encoded blob (e.g.
// `{"token":"..."}` or `{"key":"..."}`) ready for SetUserCredentialsTyped.
func prepareTypedCredentialEnv(locale string, body typedCredentialBody) ([]byte, *string, *string, *errTypedCredential) {
	if body.CredentialType == nil || *body.CredentialType == "" || *body.CredentialType == "env" {
		// Caller should route to legacy env path. Indicate that with all-nil
		// returns + nil error; handler treats this as "fall through".
		return nil, nil, nil, nil
	}

	credType := *body.CredentialType
	if credType != "pat" && credType != "ssh_key" {
		return nil, nil, nil, &errTypedCredential{
			status:   http.StatusBadRequest,
			msg:      i18n.T(locale, i18n.MsgGitCredUnsupportedCredType, credType),
			errorKey: "git.cred_unsupported_type",
		}
	}

	// Host scope is required + validated for both pat and ssh_key.
	scope, terr := validateHostScope(locale, body.HostScope, credType)
	if terr != nil {
		return nil, nil, nil, terr
	}

	if len(body.Blob) == 0 {
		return nil, nil, nil, &errTypedCredential{
			status:   http.StatusBadRequest,
			msg:      i18n.T(locale, i18n.MsgGitCredBlobMissingField, "blob"),
			errorKey: "git.cred_blob_missing",
		}
	}
	var blob map[string]string
	if err := json.Unmarshal(body.Blob, &blob); err != nil {
		return nil, nil, nil, &errTypedCredential{
			status:   http.StatusBadRequest,
			msg:      i18n.T(locale, i18n.MsgGrantEnvValueInvalid, err.Error()),
			errorKey: "git.cred_blob_invalid",
		}
	}

	switch credType {
	case "pat":
		token := strings.TrimSpace(blob["token"])
		if token == "" {
			return nil, nil, nil, &errTypedCredential{
				status:   http.StatusBadRequest,
				msg:      i18n.T(locale, i18n.MsgGitCredBlobMissingField, "token"),
				errorKey: "git.cred_blob_missing_token",
			}
		}
		if err := tools.ValidatePATToken(token); err != nil {
			return nil, nil, nil, &errTypedCredential{
				status:   http.StatusBadRequest,
				msg:      i18n.T(locale, i18n.MsgGitCredTokenInvalid),
				errorKey: "git.cred_token_invalid",
			}
		}
		envBytes, _ := json.Marshal(map[string]string{"token": token})
		ct := credType
		return envBytes, &ct, &scope, nil

	case "ssh_key":
		// Windows clipboard often pastes CRLF; ssh.ParsePrivateKey expects LF
		// and chokes silently otherwise. Normalize BEFORE validation so the
		// stored bytes match what ssh -i will read at exec time.
		key := strings.ReplaceAll(blob["key"], "\r\n", "\n")
		if strings.TrimSpace(key) == "" {
			return nil, nil, nil, &errTypedCredential{
				status:   http.StatusBadRequest,
				msg:      i18n.T(locale, i18n.MsgGitCredBlobMissingField, "key"),
				errorKey: "git.cred_blob_missing_key",
			}
		}
		if err := tools.ValidateSSHKey([]byte(key)); err != nil {
			if errors.Is(err, tools.ErrSSHKeyPassphraseUnsupported) {
				return nil, nil, nil, &errTypedCredential{
					status:   http.StatusBadRequest,
					msg:      i18n.T(locale, i18n.MsgGitCredSSHPassphraseUnsupported),
					errorKey: "git.cred_ssh_passphrase_unsupported",
				}
			}
			return nil, nil, nil, &errTypedCredential{
				status:   http.StatusBadRequest,
				msg:      i18n.T(locale, i18n.MsgGitCredSSHKeyInvalid, err.Error()),
				errorKey: "git.cred_ssh_key_invalid",
			}
		}
		envBytes, _ := json.Marshal(map[string]string{"key": key})
		ct := credType
		return envBytes, &ct, &scope, nil
	}

	// Unreachable — credType already gated above.
	return nil, nil, nil, &errTypedCredential{
		status: http.StatusInternalServerError,
		msg:    fmt.Sprintf("unreachable credential_type %q", credType),
	}
}

// validateHostScope normalizes via idna.ToASCII (matches Phase 3's runtime
// comparison) then enforces hostScopeRe. Returns the canonical host_scope
// the row should store.
func validateHostScope(locale string, raw *string, credType string) (string, *errTypedCredential) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return "", &errTypedCredential{
			status:   http.StatusBadRequest,
			msg:      i18n.T(locale, i18n.MsgGitCredHostScopeRequired, credType),
			errorKey: "git.cred_host_scope_required",
		}
	}
	scope := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(*raw), "."))

	// Optional host:port split — only the host part goes through idna.
	hostPart, port, hasPort := splitHostScopePort(scope)
	ascii, err := idna.Lookup.ToASCII(hostPart)
	if err != nil {
		return "", &errTypedCredential{
			status:   http.StatusBadRequest,
			msg:      i18n.T(locale, i18n.MsgGitCredHostScopeInvalid, *raw),
			errorKey: "git.cred_host_scope_invalid",
		}
	}
	normalized := ascii
	if hasPort {
		normalized = ascii + ":" + port
	}
	if !hostScopeRe.MatchString(normalized) {
		return "", &errTypedCredential{
			status:   http.StatusBadRequest,
			msg:      i18n.T(locale, i18n.MsgGitCredHostScopeInvalid, *raw),
			errorKey: "git.cred_host_scope_invalid",
		}
	}
	return normalized, nil
}

func splitHostScopePort(h string) (host, port string, ok bool) {
	idx := strings.LastIndex(h, ":")
	if idx < 0 {
		return h, "", false
	}
	port = h[idx+1:]
	if port == "" {
		return h, "", false
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			return h, "", false
		}
	}
	return h[:idx], port, true
}

// writeTypedCredentialError serializes errTypedCredential into the standard
// gateway error envelope `{error:{code,message}}` so the shared HttpClient
// parser picks up `code` as `error_key`. We ALSO keep a flat `error_key` field
// at the top level for older clients and tests that parse the body directly.
func writeTypedCredentialError(w http.ResponseWriter, e *errTypedCredential) {
	inner := map[string]string{"message": e.msg}
	if e.errorKey != "" {
		inner["code"] = e.errorKey
	}
	body := map[string]any{"error": inner}
	if e.errorKey != "" {
		body["error_key"] = e.errorKey
	}
	writeJSON(w, e.status, body)
}
