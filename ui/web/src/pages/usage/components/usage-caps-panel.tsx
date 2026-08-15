import { useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Plus, RefreshCw, Save, ShieldAlert, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { formatApiCost, formatDate, formatTokens } from "@/lib/format";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useUsageCaps } from "../hooks/use-usage-caps";
import type { UsageCapPolicy } from "@/types/usage-caps";
import { UsageCapRow } from "./usage-cap-row";

const ALL = "__all__";

export function UsageCapsPanel() {
  const { t } = useTranslation("usage");
  const { agents } = useAgents();
  const { providers } = useProviders();
  const { utilization, events, refreshing, refresh, createPolicy, updatePolicy, deletePolicy } = useUsageCaps();
  const [windowValue, setWindowValue] = useState<UsageCapPolicy["window"]>("day");
  const [agentId, setAgentId] = useState(ALL);
  const [providerId, setProviderId] = useState(ALL);
  const [modelId, setModelId] = useState("");
  const [maxTokens, setMaxTokens] = useState("");
  const [maxCost, setMaxCost] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [editingPolicy, setEditingPolicy] = useState<UsageCapPolicy | null>(null);
  const [saving, setSaving] = useState(false);

  const provider = useMemo(() => providers.find((p) => p.id === providerId), [providerId, providers]);
  const providerType = provider?.provider_type ?? (editingPolicy && providerId === (editingPolicy.provider_id ?? ALL) ? editingPolicy.provider_type : undefined);
  const blockedEvents = events.filter((event) => event.decision === "block");
  const isEditing = editingPolicy != null;

  const resetForm = () => {
    setEditingPolicy(null);
    setWindowValue("day");
    setAgentId(ALL);
    setProviderId(ALL);
    setModelId("");
    setMaxTokens("");
    setMaxCost("");
    setEnabled(true);
  };

  const startEdit = (policy: UsageCapPolicy) => {
    setEditingPolicy(policy);
    setWindowValue(policy.window);
    setAgentId(policy.agent_id ?? ALL);
    setProviderId(policy.provider_id ?? ALL);
    setModelId(policy.model_id ?? "");
    setMaxTokens(policy.max_tokens ? String(policy.max_tokens) : "");
    setMaxCost(policy.max_cost_micros ? String(policy.max_cost_micros / 1_000_000) : "");
    setEnabled(policy.enabled);
  };

  const onSubmit = async () => {
    const tokenValue = maxTokens.trim();
    const costValue = maxCost.trim();
    const tokens = Number(tokenValue);
    const cost = Number(costValue);
    if ((!tokenValue || !Number.isFinite(tokens) || tokens <= 0) && (!costValue || !Number.isFinite(cost) || cost <= 0)) return;
    setSaving(true);
    try {
      const input = {
        window: windowValue,
        agent_id: agentId === ALL ? (isEditing ? "" : undefined) : agentId,
        provider_id: providerId === ALL ? (isEditing ? "" : undefined) : providerId,
        provider_type: providerType ?? (isEditing ? "" : undefined),
        model_id: modelId.trim() || (isEditing ? "" : undefined),
        max_tokens: tokenValue && Number.isFinite(tokens) && tokens > 0 ? Math.floor(tokens) : (isEditing ? null : undefined),
        max_cost_usd: costValue && Number.isFinite(cost) && cost > 0 ? cost : (isEditing ? null : undefined),
        enabled,
      };
      if (editingPolicy) {
        await updatePolicy(editingPolicy.id, input);
      } else {
        await createPolicy(input);
      }
      resetForm();
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="space-y-4 rounded-md border p-3 sm:p-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h3 className="text-sm font-semibold">{t("caps.title")}</h3>
          <p className="text-xs text-muted-foreground">{t("caps.description")}</p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={() => void refresh()} disabled={refreshing} className="gap-1 self-start sm:self-auto">
          <RefreshCw className={`h-3.5 w-3.5${refreshing ? " animate-spin" : ""}`} />
          {t("common:refresh", "Refresh")}
        </Button>
      </div>

      <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-7">
        <Field label={t("caps.window")}>
          <Select value={windowValue} onValueChange={(value) => setWindowValue(value as UsageCapPolicy["window"])}>
            <SelectTrigger className="w-full text-base md:text-sm"><SelectValue /></SelectTrigger>
            <SelectContent>
              {["hour", "day", "week", "month"].map((value) => <SelectItem key={value} value={value}>{t(`caps.windows.${value}`)}</SelectItem>)}
            </SelectContent>
          </Select>
        </Field>
        <Field label={t("caps.agent")}>
          <Select value={agentId} onValueChange={setAgentId}>
            <SelectTrigger className="w-full text-base md:text-sm"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t("caps.allAgents")}</SelectItem>
              {agents.map((a) => <SelectItem key={a.id} value={a.id}>{a.display_name || a.agent_key || a.id}</SelectItem>)}
            </SelectContent>
          </Select>
        </Field>
        <Field label={t("caps.provider")}>
          <Select value={providerId} onValueChange={setProviderId}>
            <SelectTrigger className="w-full text-base md:text-sm"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t("caps.allProviders")}</SelectItem>
              {providers.map((p) => <SelectItem key={p.id} value={p.id}>{p.display_name || p.name}</SelectItem>)}
            </SelectContent>
          </Select>
        </Field>
        <Field label={t("caps.model")}>
          <Input value={modelId} onChange={(e) => setModelId(e.target.value)} placeholder="openai/gpt-4o-mini" className="text-base md:text-sm" />
        </Field>
        <Field label={t("caps.maxTokens")}>
          <Input value={maxTokens} onChange={(e) => setMaxTokens(e.target.value)} inputMode="numeric" placeholder="500000" className="text-base md:text-sm" />
        </Field>
        <Field label={t("caps.maxCost")}>
          <div className="flex gap-2">
            <Input value={maxCost} onChange={(e) => setMaxCost(e.target.value)} inputMode="decimal" placeholder="25" className="text-base md:text-sm" />
            <Button type="button" size="icon" onClick={() => void onSubmit()} disabled={saving} aria-label={isEditing ? t("caps.save") : t("caps.create")}>
              {isEditing ? <Save className="h-4 w-4" /> : <Plus className="h-4 w-4" />}
            </Button>
            {isEditing ? (
              <Button type="button" variant="outline" size="icon" onClick={resetForm} aria-label={t("caps.cancel")}>
                <X className="h-4 w-4" />
              </Button>
            ) : null}
          </div>
        </Field>
        <Field label={t("columns.status")}>
          <div className="flex h-10 items-center gap-2">
            <Switch checked={enabled} onCheckedChange={setEnabled} />
            <span className="text-xs text-muted-foreground">{enabled ? t("caps.enabled") : t("caps.disabled")}</span>
          </div>
        </Field>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full min-w-[760px] text-sm">
          <thead>
            <tr className="border-b bg-muted/40">
              <th className="px-3 py-2 text-left font-medium">{t("caps.scope")}</th>
              <th className="px-3 py-2 text-left font-medium">{t("caps.window")}</th>
              <th className="px-3 py-2 text-right font-medium">{t("caps.tokens")}</th>
              <th className="px-3 py-2 text-right font-medium">{t("caps.cost")}</th>
              <th className="px-3 py-2 text-right font-medium">{t("columns.status")}</th>
              <th className="px-3 py-2 text-right font-medium">{t("columns.actions", "Actions")}</th>
            </tr>
          </thead>
          <tbody>
            {utilization.length === 0 ? (
              <tr><td colSpan={6} className="px-3 py-6 text-center text-muted-foreground">{t("caps.empty")}</td></tr>
            ) : utilization.map((row) => (
              <UsageCapRow key={row.policy.id} row={row} onEdit={startEdit} onDelete={() => void deletePolicy(row.policy.id)} />
            ))}
          </tbody>
        </table>
      </div>

      {blockedEvents.length > 0 ? (
        <div className="space-y-2">
          <div className="flex items-center gap-2 text-sm font-medium"><ShieldAlert className="h-4 w-4 text-destructive" />{t("caps.recentBlocks")}</div>
          <div className="grid gap-2 sm:grid-cols-2">
            {blockedEvents.slice(0, 4).map((event) => (
              <div key={event.id} className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs">
                <div className="font-medium">{event.reason || t("caps.blocked")}</div>
                <div className="text-muted-foreground">{formatDate(event.created_at)} · {formatTokens(event.estimated_tokens)} · {formatApiCost(event.estimated_cost_micros / 1_000_000)}</div>
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </section>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <div className="space-y-1.5"><Label className="text-xs">{label}</Label>{children}</div>;
}
