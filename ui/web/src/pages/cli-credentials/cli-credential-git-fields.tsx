/**
 * CliCredentialGitFields — Phase 5 typed-credential form for the git adapter.
 *
 * Rendered conditionally from CLIUserCredentialsDialog when
 * `binary.adapter_name === "git"`. Three credential types: env (legacy),
 * pat (single token field), ssh_key (textarea). Host scope is required
 * for pat/ssh_key — validated client-side AND server-side; this component
 * only blocks the network call when the field is empty.
 *
 * Secret state reset: when the user switches type, all secret state is
 * cleared via `useEffect([type])` so a token entered for "PAT" cannot leak
 * into the SSH key buffer (or vice versa).
 */
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

export type GitCredentialType = "env" | "pat" | "ssh_key";

export interface CliCredentialGitFieldsProps {
  type: GitCredentialType;
  onTypeChange: (t: GitCredentialType) => void;
  hostScope: string;
  onHostScopeChange: (v: string) => void;
  token: string;
  onTokenChange: (v: string) => void;
  privateKey: string;
  onPrivateKeyChange: (v: string) => void;
  /** Backend error_key for inline display (e.g. "git.cred_ssh_passphrase_unsupported"). */
  errorKey?: string;
  /** True iff the row already exists and the user is editing — show "secret set" placeholder. */
  hasExistingSecret?: boolean;
}

export function CliCredentialGitFields({
  type,
  onTypeChange,
  hostScope,
  onHostScopeChange,
  token,
  onTokenChange,
  privateKey,
  onPrivateKeyChange,
  errorKey,
  hasExistingSecret,
}: CliCredentialGitFieldsProps) {
  const { t } = useTranslation("cli-credentials");

  // Reset all secret state on type switch — prevents a token pasted under
  // "PAT" from being submitted when the user later flips to "SSH key".
  useEffect(() => {
    onTokenChange("");
    onPrivateKeyChange("");
    // intentionally only depend on `type` — the setters are stable across renders
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [type]);

  const passphraseError = errorKey === "git.cred_ssh_passphrase_unsupported";
  const sshKeyError = errorKey === "git.cred_ssh_key_invalid";
  const hostScopeError =
    errorKey === "git.cred_host_scope_required" || errorKey === "git.cred_host_scope_invalid";

  return (
    <div data-testid="git-credential-fields" className="flex flex-col gap-3">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="git-cred-type">{t("userCredentials.credentialType")}</Label>
        <select
          id="git-cred-type"
          data-testid="git-cred-type"
          value={type}
          onChange={(e) => onTypeChange(e.target.value as GitCredentialType)}
          className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-base md:text-sm shadow-xs"
        >
          <option value="env">{t("userCredentials.credentialTypeEnv")}</option>
          <option value="pat">{t("userCredentials.credentialTypePAT")}</option>
          <option value="ssh_key">{t("userCredentials.credentialTypeSSH")}</option>
        </select>
      </div>

      {/* Env legacy: caller owns env-vars table; render nothing here. */}
      {type !== "env" && (
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="git-cred-host-scope">{t("userCredentials.hostScope")}</Label>
          <Input
            id="git-cred-host-scope"
            data-testid="git-cred-host-scope"
            value={hostScope}
            onChange={(e) => onHostScopeChange(e.target.value)}
            placeholder={t("userCredentials.hostScopePlaceholder")}
            autoComplete="off"
            spellCheck={false}
            aria-invalid={hostScopeError}
          />
          {hostScopeError && (
            <p data-testid="git-cred-host-scope-error" className="text-xs text-destructive">
              {errorKey === "git.cred_host_scope_required"
                ? t("userCredentials.hostScopeRequired")
                : t("userCredentials.hostScopeInvalid")}
            </p>
          )}
        </div>
      )}

      {type === "pat" && (
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="git-cred-token">{t("userCredentials.token")}</Label>
          <Input
            id="git-cred-token"
            data-testid="git-cred-token"
            type="password"
            // Non-standard name discourages browser password managers from
            // offering to save the PAT into the OS credential store.
            name="goclaw-cred-token"
            autoComplete="off"
            spellCheck={false}
            value={token}
            onChange={(e) => onTokenChange(e.target.value)}
            placeholder={
              hasExistingSecret
                ? t("userCredentials.secretMasked")
                : t("userCredentials.tokenPlaceholder")
            }
          />
        </div>
      )}

      {type === "ssh_key" && (
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="git-cred-ssh-key">{t("userCredentials.sshKey")}</Label>
          <Textarea
            id="git-cred-ssh-key"
            data-testid="git-cred-ssh-key"
            rows={10}
            spellCheck={false}
            autoComplete="off"
            value={privateKey}
            onChange={(e) => onPrivateKeyChange(e.target.value)}
            placeholder={
              hasExistingSecret
                ? t("userCredentials.secretMasked")
                : t("userCredentials.sshKeyPlaceholder")
            }
            aria-invalid={passphraseError || sshKeyError}
            className="font-mono text-base md:text-xs"
          />
          {passphraseError && (
            <p data-testid="git-cred-passphrase-error" className="text-xs text-destructive">
              {t("userCredentials.passphraseUnsupported")}
            </p>
          )}
          {sshKeyError && !passphraseError && (
            <p data-testid="git-cred-ssh-key-error" className="text-xs text-destructive">
              {t("userCredentials.sshKeyInvalid")}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
