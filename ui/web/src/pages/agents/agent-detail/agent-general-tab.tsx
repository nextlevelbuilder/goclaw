import { useState, useCallback } from "react";
import { Save, Check, AlertCircle, Sparkles, Info, DollarSign } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import type { AgentData } from "@/types/agent";
import { IdentitySection, LlmConfigSection, WorkspaceSection } from "./general-sections";

interface AgentGeneralTabProps {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

export function AgentGeneralTab({ agent, onUpdate }: AgentGeneralTabProps) {
  const { t } = useTranslation("agents");

  // Identity
  const [displayName, setDisplayName] = useState(agent.display_name ?? "");
  const [frontmatter, setFrontmatter] = useState(agent.frontmatter ?? "");
  const [status, setStatus] = useState(agent.status);
  const [isDefault, setIsDefault] = useState(agent.is_default);

  // LLM
  const [provider, setProvider] = useState(agent.provider);
  const [model, setModel] = useState(agent.model);
  const [contextWindow, setContextWindow] = useState(agent.context_window || 200000);
  const [maxToolIterations, setMaxToolIterations] = useState(agent.max_tool_iterations || 20);
  const [llmSaveBlocked, setLlmSaveBlocked] = useState(false);

  // Workspace
  const [restrictToWorkspace, setRestrictToWorkspace] = useState(agent.restrict_to_workspace);

  // Budget (stored in cents, displayed in dollars)
  const [budgetDollars, setBudgetDollars] = useState(
    agent.budget_monthly_cents ? String(agent.budget_monthly_cents / 100) : "",
  );

  // Self-evolve (predefined agents only)
  const otherCfg = (agent.other_config ?? {}) as Record<string, unknown>;
  const [selfEvolve, setSelfEvolve] = useState(Boolean(otherCfg.self_evolve));

  // Save state
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const handleSaveBlockedChange = useCallback((blocked: boolean) => {
    setLlmSaveBlocked(blocked);
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setSaveError(null);
    setSaved(false);
    try {
      const updatedOtherConfig = { ...otherCfg, self_evolve: selfEvolve };
      const budgetCents = budgetDollars ? Math.round(parseFloat(budgetDollars) * 100) : null;
      await onUpdate({
        display_name: displayName,
        frontmatter: frontmatter || null,
        provider,
        model,
        context_window: contextWindow,
        max_tool_iterations: maxToolIterations,
        restrict_to_workspace: restrictToWorkspace,
        status,
        is_default: isDefault,
        other_config: updatedOtherConfig,
        budget_monthly_cents: budgetCents,
      });
      setSaved(true);
      setTimeout(() => setSaved(false), 3000);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : t("general.failedToSave"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="max-w-4xl space-y-6">
      <IdentitySection
        agentKey={agent.agent_key}
        displayName={displayName}
        onDisplayNameChange={setDisplayName}
        frontmatter={frontmatter}
        onFrontmatterChange={setFrontmatter}
        status={status}
        onStatusChange={setStatus}
        isDefault={isDefault}
        onIsDefaultChange={setIsDefault}
      />

      <Separator />

      <LlmConfigSection
        provider={provider}
        onProviderChange={setProvider}
        model={model}
        onModelChange={setModel}
        contextWindow={contextWindow}
        onContextWindowChange={setContextWindow}
        maxToolIterations={maxToolIterations}
        onMaxToolIterationsChange={setMaxToolIterations}
        savedProvider={agent.provider}
        savedModel={agent.model}
        onSaveBlockedChange={handleSaveBlockedChange}
      />

      <Separator />

      <WorkspaceSection
        workspace={agent.workspace}
        restrictToWorkspace={restrictToWorkspace}
        onRestrictChange={setRestrictToWorkspace}
      />

      {/* Budget */}
      <Separator />
      <div className="space-y-3">
        <div className="flex items-center gap-3">
          <DollarSign className="h-4 w-4 text-emerald-500" />
          <h3 className="text-sm font-medium">{t("general.budget")}</h3>
        </div>
        <div className="space-y-1">
          <Label htmlFor="budget" className="text-sm font-normal">
            {t("general.budgetLabel")}
          </Label>
          <p className="text-xs text-muted-foreground">
            {t("general.budgetHint")}
          </p>
        </div>
        <Input
          id="budget"
          type="number"
          min="0"
          step="0.01"
          placeholder="0.00"
          value={budgetDollars}
          onChange={(e) => setBudgetDollars(e.target.value)}
          className="max-w-[200px]"
        />
      </div>

      {/* Self-Evolve (predefined agents only) */}
      {agent.agent_type === "predefined" && (
        <>
          <Separator />
          <div className="space-y-3">
            <div className="flex items-center gap-3">
              <Sparkles className="h-4 w-4 text-violet-500" />
              <h3 className="text-sm font-medium">{t("general.selfEvolution")}</h3>
            </div>
            <div className="flex items-center justify-between gap-4">
              <div className="space-y-1">
                <Label htmlFor="self-evolve" className="text-sm font-normal">
                  {t("general.selfEvolutionLabel")}
                </Label>
                <p className="text-xs text-muted-foreground">
                  {t("general.selfEvolutionHint")}
                </p>
              </div>
              <Switch
                id="self-evolve"
                checked={selfEvolve}
                onCheckedChange={setSelfEvolve}
              />
            </div>
            {selfEvolve && (
              <div className="flex items-start gap-2 rounded-md border border-violet-200 bg-violet-50 px-3 py-2 text-xs text-violet-700 dark:border-violet-800 dark:bg-violet-950/30 dark:text-violet-300">
                <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                <span>{t("general.selfEvolutionInfo")}</span>
              </div>
            )}
          </div>
        </>
      )}

      {/* Save */}
      {saveError && (
        <div className="flex items-center gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <AlertCircle className="h-4 w-4 shrink-0" />
          {saveError}
        </div>
      )}
      <div className="flex items-center justify-end gap-2">
        {saved && (
          <span className="flex items-center gap-1 text-sm text-success">
            <Check className="h-3.5 w-3.5" /> {t("general.saved")}
          </span>
        )}
        <Button onClick={handleSave} disabled={saving || llmSaveBlocked}>
          {!saving && <Save className="h-4 w-4" />}
          {saving ? t("general.saving") : t("general.saveChanges")}
        </Button>
      </div>
    </div>
  );
}
