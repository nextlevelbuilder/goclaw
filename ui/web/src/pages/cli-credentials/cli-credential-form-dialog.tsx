import type { ManualEnvEntry } from "./cli-credential-env-vars-section";
import { useState, useEffect } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import {
  Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { useHttp } from "@/hooks/use-ws";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import type { SecureCLIBinary, CLICredentialInput, CLIPreset } from "./hooks/use-cli-credentials";
import { CliCredentialEnvVarsSection } from "./cli-credential-env-vars-section";
import { CliCredentialBinaryFields } from "./cli-credential-binary-fields";
import { CliCredentialScopeFields } from "./cli-credential-scope-fields";
import { cliCredentialSchema, type CliCredentialFormData } from "@/schemas/credential.schema";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  credential?: SecureCLIBinary | null;
  presets: Record<string, CLIPreset>;
  onSubmit: (data: CLICredentialInput) => Promise<unknown>;
}

const NONE_PRESET = "__none__";
const ENV_KEY_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;

export function CliCredentialFormDialog({ open, onOpenChange, credential, presets, onSubmit }: Props) {
  const { t } = useTranslation("cli-credentials");
  const { t: tc } = useTranslation("common");
  const http = useHttp();
  const { agents } = useAgents();

  const [selectedPreset, setSelectedPreset] = useState(NONE_PRESET);
  const [envValues, setEnvValues] = useState<Record<string, string>>({});
  const [manualEnvEntries, setManualEnvEntries] = useState<ManualEnvEntry[]>([]);
  const [initialEnvKeys, setInitialEnvKeys] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [checking, setChecking] = useState(false);
  const [checkResult, setCheckResult] = useState<{ found: boolean; path?: string; error?: string } | null>(null);

  const isEdit = !!credential;
  const presetEntries: Array<[string, CLIPreset]> = Object.entries(presets).filter(
    (e): e is [string, CLIPreset] => e[1] !== undefined,
  );
  const activePreset: CLIPreset | null = selectedPreset !== NONE_PRESET ? (presets[selectedPreset] ?? null) : null;
  const isManualMode = selectedPreset === NONE_PRESET;

  const form = useForm<CliCredentialFormData>({
    resolver: zodResolver(cliCredentialSchema),
    mode: "onChange",
    defaultValues: {
      binaryName: "",
      binaryPath: "",
      description: "",
      denyArgs: "",
      denyVerbose: "",
      timeout: 30,
      tips: "",
      agentId: "",
      enabled: true,
    },
  });


  useEffect(() => {
    if (!open) return;
    setSelectedPreset(NONE_PRESET);
    form.reset({
      binaryName: credential?.binary_name ?? "",
      binaryPath: credential?.binary_path ?? "",
      description: credential?.description ?? "",
      denyArgs: (credential?.deny_args ?? []).join(", "),
      denyVerbose: (credential?.deny_verbose ?? []).join(", "),
      timeout: credential?.timeout_seconds ?? 30,
      tips: credential?.tips ?? "",
      agentId: credential?.agent_id ?? "",
      enabled: credential?.enabled ?? true,
    });
    setEnvValues({});
    setError("");
    setCheckResult(null);

    if (!credential) {
      setInitialEnvKeys([]);
      setManualEnvEntries([]);
      return;
    }

    const applyEnvKeys = (keys: string[]) => {
      setInitialEnvKeys(keys);
      setManualEnvEntries(keys.length > 0 ? keys.map((k) => ({ key: k, value: "" })) : []);
    };

    if (credential.env_keys !== undefined) {
      applyEnvKeys(credential.env_keys ?? []);
      return;
    }

    let cancelled = false;
    void (async () => {
      try {
        const full = await http.get<SecureCLIBinary>(`/v1/cli-credentials/${credential.id}`);
        if (cancelled) return;
        applyEnvKeys(full.env_keys ?? []);
      } catch {
        if (!cancelled) applyEnvKeys([]);
      }
    })();
    return () => { cancelled = true; };
  }, [open, credential, http, form]);

  const applyPreset = (key: string) => {
    setSelectedPreset(key);
    if (key === NONE_PRESET) return;
    const p = presets[key];
    if (!p) return;
    form.reset({
      binaryName: p.binary_name,
      binaryPath: "",
      description: p.description,
      denyArgs: p.deny_args.join(", "),
      denyVerbose: p.deny_verbose.join(", "),
      timeout: p.timeout,
      tips: p.tips,
      agentId: "",
      enabled: true,
    });
    setEnvValues({});
    setManualEnvEntries([]);
  };

  const handleCheckBinary = async () => {
    const name = form.getValues("binaryName").trim();
    if (!name) return;
    setChecking(true);
    setCheckResult(null);
    try {
      const res = await http.post<{ found: boolean; path?: string; error?: string }>(
        "/v1/cli-credentials/check-binary",
        { binary_name: name },
      );
      setCheckResult(res);
      if (res.found && res.path) form.setValue("binaryPath", res.path);
    } catch {
      setCheckResult({ found: false, error: t("form.binaryNotFound") });
    } finally {
      setChecking(false);
    }
  };

  const splitCommaList = (v: string): string[] =>
    v.split(",").map((s) => s.trim()).filter(Boolean);

  const buildEnvPayload = (): Record<string, string> | null => {
    if (!isManualMode) return envValues;
    const env: Record<string, string> = {};
    for (const entry of manualEnvEntries) {
      const k = entry.key.trim();
      if (k && !ENV_KEY_PATTERN.test(k)) {
        setError(t("form.invalidEnvKey", { key: k }));
        return null;
      }
      if (k) env[k] = entry.value;
    }
    return env;
  };

  const handleSubmit = form.handleSubmit(async (values) => {
    setLoading(true);
    setError("");
    try {
      const payload: CLICredentialInput = {
        binary_name: values.binaryName.trim(),
        binary_path: values.binaryPath?.trim() || undefined,
        description: values.description?.trim() ?? "",
        deny_args: splitCommaList(values.denyArgs ?? ""),
        deny_verbose: splitCommaList(values.denyVerbose ?? ""),
        timeout_seconds: values.timeout,
        tips: values.tips?.trim() ?? "",
        agent_id: values.agentId?.trim() || undefined,
        enabled: values.enabled,
      };
      if (selectedPreset !== NONE_PRESET) payload.preset = selectedPreset;
      const env = buildEnvPayload();
      if (!env) return;
      if (Object.keys(env).length > 0) {
        payload.env = env;
      } else if (isEdit && isManualMode && initialEnvKeys.length > 0) {
        payload.env = {};
      }
      await onSubmit(payload);
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("form.failedToSave"));
    } finally {
      setLoading(false);
    }
  });

  return (
    <Dialog open={open} onOpenChange={(v) => !loading && onOpenChange(v)}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{isEdit ? t("form.editTitle") : t("form.createTitle")}</DialogTitle>
        </DialogHeader>

        <div className="grid gap-4 py-2 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
          {!isEdit && presetEntries.length > 0 && (
            <div className="grid gap-1.5">
              <Label>{t("form.preset")}</Label>
              <Select value={selectedPreset} onValueChange={applyPreset}>
                <SelectTrigger className="text-base md:text-sm">
                  <SelectValue placeholder={t("form.presetPlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE_PRESET}>{t("form.noPreset")}</SelectItem>
                  {presetEntries.map(([k, p]) => (
                    <SelectItem key={k} value={k}>
                      {p.binary_name} — {p.description}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">{t("form.presetHint")}</p>
            </div>
          )}

          {isEdit && (
            <p className="text-xs text-muted-foreground rounded-md border border-dashed p-2">
              {t("form.encryptedHint")}
            </p>
          )}

          <CliCredentialEnvVarsSection
            isManualMode={isManualMode}
            activePreset={activePreset}
            envValues={envValues}
            setEnvValues={setEnvValues}
            manualEnvEntries={manualEnvEntries}
            setManualEnvEntries={setManualEnvEntries}
          />

          <CliCredentialBinaryFields
            form={form}
            checking={checking}
            checkResult={checkResult}
            onCheckBinary={handleCheckBinary}
          />

          <CliCredentialScopeFields form={form} agents={agents} />

          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            {tc("cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={loading}>
            {loading
              ? tc("saving")
              : isEdit
                ? tc("update")
                : tc("create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
