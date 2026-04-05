import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { Plus, X } from "lucide-react";
import { useHttp } from "@/hooks/use-ws";
import type { SecureCLIBinary, CLICredentialInput, CLIPreset } from "./hooks/use-cli-credentials";

interface ManualEnvEntry {
  key: string;
  value: string;
}

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  credential?: SecureCLIBinary | null;
  presets: Record<string, CLIPreset>;
  onSubmit: (data: CLICredentialInput) => Promise<unknown>;
}

const NONE_PRESET = "__none__";

export function CliCredentialFormDialog({ open, onOpenChange, credential, presets, onSubmit }: Props) {
  const { t } = useTranslation("cli-credentials");
  const { t: tc } = useTranslation("common");
  const http = useHttp();

  const [selectedPreset, setSelectedPreset] = useState(NONE_PRESET);
  const [binaryName, setBinaryName] = useState("");
  const [binaryPath, setBinaryPath] = useState("");
  const [description, setDescription] = useState("");
  const [denyArgs, setDenyArgs] = useState("");
  const [denyVerbose, setDenyVerbose] = useState("");
  const [timeout, setTimeout] = useState(30);
  const [tips, setTips] = useState("");
  const [agentId, setAgentId] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [envValues, setEnvValues] = useState<Record<string, string>>({});
  const [manualEnvEntries, setManualEnvEntries] = useState<ManualEnvEntry[]>([]);
  /** Snapshot when dialog opens (edit) — detect clearing all env rows */
  const [initialEnvKeys, setInitialEnvKeys] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const isEdit = !!credential;
  const presetEntries: Array<[string, CLIPreset]> = Object.entries(presets).filter(
    (e): e is [string, CLIPreset] => e[1] !== undefined,
  );

  const activePreset: CLIPreset | null =
    selectedPreset !== NONE_PRESET ? (presets[selectedPreset] ?? null) : null;

  const isManualMode = selectedPreset === NONE_PRESET;

  useEffect(() => {
    if (!open) return;
    setSelectedPreset(NONE_PRESET);
    setBinaryName(credential?.binary_name ?? "");
    setBinaryPath(credential?.binary_path ?? "");
    setDescription(credential?.description ?? "");
    setDenyArgs((credential?.deny_args ?? []).join(", "));
    setDenyVerbose((credential?.deny_verbose ?? []).join(", "));
    setTimeout(credential?.timeout_seconds ?? 30);
    setTips(credential?.tips ?? "");
    setAgentId(credential?.agent_id ?? "");
    setEnabled(credential?.enabled ?? true);
    setEnvValues({});
    setError("");

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
    return () => {
      cancelled = true;
    };
  }, [open, credential, http]);

  const applyPreset = (key: string) => {
    setSelectedPreset(key);
    if (key === NONE_PRESET) return;
    const p = presets[key];
    if (!p) return;
    setBinaryName(p.binary_name);
    setDescription(p.description);
    setDenyArgs(p.deny_args.join(", "));
    setDenyVerbose(p.deny_verbose.join(", "));
    setTimeout(p.timeout);
    setTips(p.tips);
    setEnvValues({});
    setManualEnvEntries([]);
  };

  const addManualEnvEntry = useCallback(() => {
    setManualEnvEntries((prev) => [...prev, { key: "", value: "" }]);
  }, []);

  const removeManualEnvEntry = useCallback((index: number) => {
    setManualEnvEntries((prev) => prev.filter((_, i) => i !== index));
  }, []);

  const updateManualEnvEntry = useCallback((index: number, field: "key" | "value", val: string) => {
    setManualEnvEntries((prev) =>
      prev.map((entry, i) => (i === index ? { ...entry, [field]: val } : entry)),
    );
  }, []);

  const splitCommaList = (v: string): string[] =>
    v.split(",").map((s) => s.trim()).filter(Boolean);

  const ENV_KEY_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;

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

  const handleSubmit = async () => {
    if (!binaryName.trim()) {
      setError(t("form.binaryNameRequired"));
      return;
    }
    setLoading(true);
    setError("");
    try {
      const payload: CLICredentialInput = {
        binary_name: binaryName.trim(),
        binary_path: binaryPath.trim() || undefined,
        description: description.trim(),
        deny_args: splitCommaList(denyArgs),
        deny_verbose: splitCommaList(denyVerbose),
        timeout_seconds: timeout,
        tips: tips.trim(),
        agent_id: agentId.trim() || undefined,
        enabled,
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
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !loading && onOpenChange(v)}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{isEdit ? t("form.editTitle") : t("form.createTitle")}</DialogTitle>
        </DialogHeader>

        <div className="grid gap-4 py-2 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
          {/* Preset selector — only on create */}
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
              <p className="text-xs text-muted-foreground">
                {t("form.presetHint")}
              </p>
            </div>
          )}

          {/* Existing credentials indicator (edit mode) */}
          {isEdit && (
            <p className="text-xs text-muted-foreground rounded-md border border-dashed p-2">
              {t("form.encryptedHint")}
            </p>
          )}

          {/* Env var inputs from preset */}
          {activePreset && activePreset.env_vars.length > 0 && (
            <div className="grid gap-3 rounded-md border p-3">
              <p className="text-sm font-medium">{t("form.envVars")}</p>
              {activePreset.env_vars.map((ev) => (
                <div key={ev.name} className="grid gap-1.5">
                  <Label htmlFor={`env-${ev.name}`}>
                    {ev.name}
                    {ev.optional && <span className="ml-1 text-xs text-muted-foreground">({tc("optional")})</span>}
                  </Label>
                  <Input
                    id={`env-${ev.name}`}
                    type="password"
                    autoComplete="off"
                    placeholder={ev.desc}
                    value={envValues[ev.name] ?? ""}
                    onChange={(e) => setEnvValues((prev) => ({ ...prev, [ev.name]: e.target.value }))}
                    className="text-base md:text-sm"
                  />
                  {ev.desc && (
                    <p className="text-xs text-muted-foreground">{ev.desc}</p>
                  )}
                </div>
              ))}
            </div>
          )}

          {/* Manual env var entries (no preset selected) */}
          {isManualMode && (
            <div className="grid gap-3 rounded-md border p-3">
              <div className="flex items-center justify-between">
                <p className="text-sm font-medium">{t("form.envVars")}</p>
                <Button type="button" variant="outline" size="sm" onClick={addManualEnvEntry}>
                  <Plus className="mr-1 h-3.5 w-3.5" />
                  {t("form.addEnvVar")}
                </Button>
              </div>
              {manualEnvEntries.length === 0 && (
                <p className="text-xs text-muted-foreground">{t("form.noEnvVarsHint")}</p>
              )}
              {manualEnvEntries.map((entry, idx) => (
                <div key={idx} className="flex items-start gap-2">
                  <div className="grid flex-1 gap-1.5">
                    <Input
                      placeholder={t("form.envKeyPlaceholder")}
                      value={entry.key}
                      onChange={(e) => updateManualEnvEntry(idx, "key", e.target.value)}
                      className="text-base md:text-sm font-mono"
                    />
                  </div>
                  <div className="grid flex-1 gap-1.5">
                    <Input
                      type="password"
                      autoComplete="off"
                      placeholder={t("form.envValuePlaceholder")}
                      value={entry.value}
                      onChange={(e) => updateManualEnvEntry(idx, "value", e.target.value)}
                      className="text-base md:text-sm"
                    />
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="mt-0.5 h-8 w-8 shrink-0"
                    onClick={() => removeManualEnvEntry(idx)}
                  >
                    <X className="h-4 w-4" />
                  </Button>
                </div>
              ))}
            </div>
          )}

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="grid gap-1.5">
              <Label htmlFor="cc-name">{t("form.binaryName")}</Label>
              <Input
                id="cc-name"
                value={binaryName}
                onChange={(e) => setBinaryName(e.target.value)}
                placeholder={t("placeholders.binaryName")}
                className="text-base md:text-sm"
              />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="cc-path">{t("form.binaryPath")} <span className="text-xs text-muted-foreground">({tc("optional")})</span></Label>
              <Input
                id="cc-path"
                value={binaryPath}
                onChange={(e) => setBinaryPath(e.target.value)}
                placeholder={t("placeholders.binaryPath")}
                className="text-base md:text-sm"
              />
            </div>
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="cc-desc">{tc("description")}</Label>
            <Textarea
              id="cc-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t("placeholders.description")}
              rows={2}
              className="text-base md:text-sm"
            />
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="grid gap-1.5">
              <Label htmlFor="cc-deny-args">{t("form.denyArgs")} <span className="text-xs text-muted-foreground">({t("form.commaSeparated")})</span></Label>
              <Input
                id="cc-deny-args"
                value={denyArgs}
                onChange={(e) => setDenyArgs(e.target.value)}
                placeholder={t("placeholders.denyArgs")}
                className="text-base md:text-sm"
              />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="cc-timeout">{t("form.timeout")}</Label>
              <Input
                id="cc-timeout"
                type="number"
                min={1}
                value={timeout}
                onChange={(e) => setTimeout(Number(e.target.value))}
                className="text-base md:text-sm"
              />
            </div>
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="cc-deny-verbose">{t("form.denyVerbose")} <span className="text-xs text-muted-foreground">({t("form.commaSeparated")})</span></Label>
            <Input
              id="cc-deny-verbose"
              value={denyVerbose}
              onChange={(e) => setDenyVerbose(e.target.value)}
              placeholder={t("placeholders.denyVerbose")}
              className="text-base md:text-sm"
            />
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="cc-tips">{t("form.tips")}</Label>
            <Textarea
              id="cc-tips"
              value={tips}
              onChange={(e) => setTips(e.target.value)}
              placeholder={t("placeholders.tips")}
              rows={2}
              className="text-base md:text-sm"
            />
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="cc-agent">{t("form.agentId")} <span className="text-xs text-muted-foreground">({t("form.agentIdHint")})</span></Label>
            <Input
              id="cc-agent"
              value={agentId}
              onChange={(e) => setAgentId(e.target.value)}
              placeholder={t("placeholders.agentId")}
              className="text-base md:text-sm"
            />
          </div>

          <div className="flex items-center gap-2">
            <Switch id="cc-enabled" checked={enabled} onCheckedChange={setEnabled} />
            <Label htmlFor="cc-enabled">{tc("enabled")}</Label>
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>{tc("cancel")}</Button>
          <Button onClick={handleSubmit} disabled={loading}>
            {loading ? tc("saving") : isEdit ? tc("update") : tc("create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
