import { useTranslation } from "react-i18next";
import { Pencil, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatCost, formatTokens } from "@/lib/format";
import type { UsageCapPolicy, UsageCapUtilization } from "@/types/usage-caps";

interface UsageCapRowProps {
  row: UsageCapUtilization;
  onEdit: (policy: UsageCapPolicy) => void;
  onDelete: () => void;
}

export function UsageCapRow({ row, onEdit, onDelete }: UsageCapRowProps) {
  const { t } = useTranslation("usage");
  const p = row.policy;
  const tokenUsed = row.used_tokens + row.reserved_tokens;
  const costUsed = row.used_cost_micros + row.reserved_cost_micros;
  const tokenPct = p.max_tokens ? Math.min(100, Math.round((tokenUsed / p.max_tokens) * 100)) : 0;
  const costPct = p.max_cost_micros ? Math.min(100, Math.round((costUsed / p.max_cost_micros) * 100)) : 0;
  const isAgentBudget = p.source === "agent_budget_monthly_cents";

  return (
    <tr className="border-b last:border-0">
      <td className="px-3 py-2">
        <div className="font-medium">{p.model_id || p.provider_type || t("caps.tenantScope")}</div>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span>{p.agent_id ? t("caps.agentScoped") : t("caps.tenantScoped")}</span>
          {isAgentBudget ? <Badge variant="secondary">{t("caps.agentBudgetSource")}</Badge> : null}
        </div>
      </td>
      <td className="px-3 py-2"><Badge variant="outline">{t(`caps.windows.${p.window}`)}</Badge></td>
      <td className="px-3 py-2 text-right">{p.max_tokens ? `${formatTokens(tokenUsed)} / ${formatTokens(p.max_tokens)} (${tokenPct}%)` : "-"}</td>
      <td className="px-3 py-2 text-right">{p.max_cost_micros ? `${formatCost(costUsed / 1_000_000)} / ${formatCost(p.max_cost_micros / 1_000_000)} (${costPct}%)` : "-"}</td>
      <td className="px-3 py-2 text-right"><Badge variant={p.enabled ? "default" : "secondary"}>{p.enabled ? t("caps.enabled") : t("caps.disabled")}</Badge></td>
      <td className="px-3 py-2 text-right">
        <div className="flex justify-end gap-1">
          <Button type="button" variant="ghost" size="icon" onClick={() => onEdit(p)} disabled={isAgentBudget} aria-label={t("caps.edit")} title={isAgentBudget ? t("caps.agentBudgetManaged") : undefined}><Pencil className="h-4 w-4" /></Button>
          <Button type="button" variant="ghost" size="icon" onClick={onDelete} disabled={isAgentBudget} aria-label={t("caps.delete")} title={isAgentBudget ? t("caps.agentBudgetManaged") : undefined}><Trash2 className="h-4 w-4" /></Button>
        </div>
      </td>
    </tr>
  );
}
