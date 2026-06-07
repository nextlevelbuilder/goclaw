/**
 * Per-grant env override section.
 * Switch "Override binary defaults" (M1: checkbox-equivalent).
 * Reveal: POST .../env:reveal — values in component state only, cleared on close.
 * Denylist: keep in sync with internal/crypto/env_denylist.go
 */
import { useState, useCallback, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Eye } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { toast } from "@/stores/use-toast-store";
import { useHttp } from "@/hooks/use-ws";
import type { CLIEnvEntryKind } from "@/types/cli-credential";
import { CliCredentialGrantEnvRow } from "./cli-credential-grant-env-row";

// Keep in sync with internal/crypto/env_denylist.go.
// Backend is authoritative; this list drives inline UX warnings only.
const ENV_DENYLIST_EXACT = new Set([
  "PATH", "HOME", "USER", "SHELL", "PWD",
  "LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT",
  "NODE_OPTIONS", "NODE_PATH",
  "PYTHONPATH", "PYTHONHOME", "PYTHONSTARTUP",
  "GIT_SSH_COMMAND", "GIT_SSH", "GIT_EXEC_PATH", "GIT_CONFIG_SYSTEM",
  "SSH_AUTH_SOCK",
  // Shell startup and proxy/certificate variables can alter command behavior.
  "BASH_ENV", "ENV", "PROMPT_COMMAND",
  "PERL5LIB", "RUBYOPT",
  "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY",
  "SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE",
  "IFS",
]);
// Keep in sync with deniedPrefixes in internal/crypto/env_denylist.go.
const ENV_DENYLIST_PREFIXES = ["DYLD_", "GOCLAW_", "LD_", "NPM_CONFIG_"];

export interface GrantEnvEntry {
  key: string;
  value: string;
  kind: CLIEnvEntryKind;
  masked: boolean; // true = not yet revealed from server
}

export interface GrantEnvState {
  overrideEnabled: boolean;
  entries: GrantEnvEntry[];
}

interface Props {
  binaryId: string;
  grantId: string | null;
  initialEnvSet: boolean;
  initialEnvKeys: string[];
  state: GrantEnvState;
  onChange: (next: GrantEnvState) => void;
  rejectedKeys?: string[];
}

export function CliCredentialGrantEnvSection({
  binaryId, grantId, initialEnvSet, initialEnvKeys,
  state, onChange, rejectedKeys = [],
}: Props) {
  const { t } = useTranslation("cli-credentials");
  const http = useHttp();
  const [revealing, setRevealing] = useState(false);
  const [revealed, setRevealed] = useState(false);
  const { overrideEnabled, entries } = state;
  // Track reveal timeout so plaintext can be cleared on reveal refresh/unmount.
  const blurTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Clear revealed plaintext from entries on component unmount.
  // This is defense-in-depth — plaintext should not persist in React state beyond use.
  useEffect(() => {
    return () => {
      if (blurTimeoutRef.current) clearTimeout(blurTimeoutRef.current);
      // Overwrite revealed values with empty strings on unmount.
      onChange({
        overrideEnabled: state.overrideEnabled,
        entries: state.entries.map((e) => ({ ...e, value: "", masked: e.masked })),
      });
    };
  }, []);

  const setEntries = useCallback(
    (updater: (prev: GrantEnvEntry[]) => GrantEnvEntry[]) =>
      onChange({ overrideEnabled, entries: updater(entries) }),
    [onChange, overrideEnabled, entries],
  );

  const handleToggle = useCallback((checked: boolean) => {
    if (checked) {
      if (initialEnvSet && !revealed && entries.every((e) => e.masked)) {
        const masked: GrantEnvEntry[] = initialEnvKeys.map((k) => ({ key: k, value: "", kind: "sensitive", masked: true }));
        onChange({ overrideEnabled: true, entries: masked.length > 0 ? masked : [{ key: "", value: "", kind: "sensitive", masked: false }] });
      } else if (entries.length === 0) {
        onChange({ overrideEnabled: true, entries: [{ key: "", value: "", kind: "sensitive", masked: false }] });
      } else {
        onChange({ overrideEnabled: true, entries });
      }
    } else {
      onChange({ overrideEnabled: false, entries });
    }
  }, [initialEnvSet, initialEnvKeys, revealed, entries, onChange]);

  const handleReveal = useCallback(async () => {
    if (!grantId) return;
    setRevealing(true);
    try {
      // POST keeps reveal out of URL/history and avoids query caching.
      const res = await http.post<{ env_vars: Record<string, string> }>(
        `/v1/cli-credentials/${binaryId}/agent-grants/${grantId}/env:reveal`,
      );
      const filled: GrantEnvEntry[] = Object.entries(res.env_vars).map(([k, v]) => ({
        key: k,
        value: v,
        kind: entries.find((entry) => entry.key === k)?.kind ?? "sensitive",
        masked: false,
      }));
      onChange({ overrideEnabled: true, entries: filled.length > 0 ? filled : entries });
      setRevealed(true);
      // Wipe plaintext after 30s of inactivity.
      if (blurTimeoutRef.current) clearTimeout(blurTimeoutRef.current);
      blurTimeoutRef.current = setTimeout(() => {
        onChange({
          overrideEnabled: true,
          entries: (filled.length > 0 ? filled : entries).map((e) => ({ ...e, value: "", masked: true })),
        });
        setRevealed(false);
      }, 30_000);
    } catch (err) {
      const code = (err as { code?: string }).code ?? "";
      const msg = err instanceof Error ? err.message : "";
      const isRateLimit = code === "RESOURCE_EXHAUSTED" || msg.toLowerCase().includes("rate");
      toast.error(t("grants.envVars.revealError"), isRateLimit ? undefined : msg || undefined);
    } finally {
      setRevealing(false);
    }
  }, [grantId, binaryId, http, onChange, entries, t]);

  const addEntry = useCallback(() => setEntries((p) => [...p, { key: "", value: "", kind: "sensitive", masked: false }]), [setEntries]);
  const removeEntry = useCallback((i: number) => setEntries((p) => p.filter((_, j) => j !== i)), [setEntries]);
  const updateEntry = useCallback((i: number, f: "key" | "value" | "kind", v: string) =>
    setEntries((p) => p.map((e, j) => j === i ? { ...e, [f]: v, masked: false } : e)), [setEntries]);

  const isDenied = (k: string) => {
    if (k.length === 0) return false;
    const upper = k.toUpperCase();
    if (ENV_DENYLIST_EXACT.has(upper)) return true;
    return ENV_DENYLIST_PREFIXES.some((p) => upper.startsWith(p));
  };
  const isRejected = (k: string) => k.length > 0 && rejectedKeys.includes(k);
  const hasMasked = entries.some((e) => e.masked);

  return (
    <div className="grid gap-2 rounded-md border p-3">
      <Label className="text-sm font-medium">
        {t("grants.envVars.title")}
      </Label>
      <div className="flex items-start gap-3">
        <Switch id="grant-env-override" checked={overrideEnabled} onCheckedChange={handleToggle} className="mt-0.5" />
        <div className="grid gap-0.5">
          <Label htmlFor="grant-env-override" className="text-sm font-medium cursor-pointer">
            {t("grants.envVars.overrideToggle")}
          </Label>
          <p className="text-xs text-muted-foreground">{t("grants.envVars.overrideHelp")}</p>
        </div>
      </div>

      {overrideEnabled && (
        <div className="grid gap-2 mt-1">
          {hasMasked && !revealed && grantId && (
            <Button type="button" variant="outline" size="sm" onClick={handleReveal}
              disabled={revealing} className="w-fit gap-1.5">
              <Eye className="h-3.5 w-3.5" />
              {revealing ? "..." : t("grants.envVars.reveal")}
            </Button>
          )}
          {entries.map((entry, idx) => {
            const hasError = isDenied(entry.key) || isRejected(entry.key);
            return (
              <CliCredentialGrantEnvRow
                key={idx}
                entry={entry}
                hasError={hasError}
                onRemove={() => removeEntry(idx)}
                onUpdate={(field, value) => updateEntry(idx, field, value)}
              />
            );
          })}
          {entries.length === 0 && (
            <p className="text-xs text-muted-foreground">{t("grants.envVars.emptyState")}</p>
          )}
          <Button type="button" variant="outline" size="sm" onClick={addEntry} className="w-fit gap-1">
            <Plus className="h-3.5 w-3.5" /> {t("grants.envVars.addKey")}
          </Button>
        </div>
      )}
    </div>
  );
}
