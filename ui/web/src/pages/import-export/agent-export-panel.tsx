import { useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Download, Package, Info } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Combobox } from "@/components/ui/combobox";
import { OperationProgress } from "@/components/shared/operation-progress";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { SectionPicker, PRESETS, type SectionDef } from "./section-picker";
import { useExportPreview, useExport } from "./hooks/use-agent-export";

const PRESET_KEYS = ["minimal", "standard", "complete"] as const;

export function AgentExportPanel() {
  const { t } = useTranslation("import-export");
  const { agents } = useAgents();
  const [agentId, setAgentId] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set(PRESETS.standard));

  const agent = agents.find((a) => a.id === agentId);
  const { data: preview } = useExportPreview(agentId || null);
  const exp = useExport();

  const agentOptions = useMemo(
    () => agents.map((a) => ({ value: a.id, label: a.display_name || a.agent_key })),
    [agents],
  );

  const sections: SectionDef[] = useMemo(() => {
    const p = preview;
    return [
      { id: "config", labelKey: "sections.config", required: true },
      { id: "context_files", labelKey: "sections.context_files", count: p?.context_files },
      {
        id: "user_data", labelKey: "sections.user_data", count: p?.user_context_files_users,
        countLabel: p ? `${p.user_context_files_users} users` : undefined,
        children: [
          { id: "user_context_files", labelKey: "sections.user_context_files", count: p?.user_context_files_users },
          { id: "user_profiles", labelKey: "sections.user_profiles", count: p?.user_profiles },
          { id: "user_overrides", labelKey: "sections.user_overrides", count: p?.user_overrides },
        ],
      },
      {
        id: "memory", labelKey: "sections.memory",
        count: p ? p.memory_global + p.memory_per_user : undefined,
        countLabel: p ? `${p.memory_global + p.memory_per_user} docs` : undefined,
        children: [
          { id: "memory_global", labelKey: "sections.memory_global", count: p?.memory_global },
          { id: "memory_per_user", labelKey: "sections.memory_per_user", count: p?.memory_per_user },
        ],
      },
      {
        id: "knowledge_graph", labelKey: "sections.knowledge_graph",
        countLabel: p ? `${p.kg_entities.toLocaleString()} ent / ${p.kg_relations.toLocaleString()} rel` : undefined,
      },
      { id: "cron", labelKey: "sections.cron", count: p?.cron_jobs },
      { id: "workspace", labelKey: "sections.workspace", count: p?.workspace_files },
    ];
  }, [preview]);

  const handlePreset = (preset: string) => setSelected(new Set(PRESETS[preset] ?? []));

  const handleExport = () => {
    if (!agent) return;
    const secs = Array.from(selected).filter((s) => !s.startsWith("memory_") && !s.startsWith("user_"));
    exp.startExport(agent.id, secs);
  };

  // Idle state
  if (exp.status === "idle") {
    return (
      <div className="space-y-4">
        <div className="flex items-start gap-2 rounded-md border bg-muted/30 px-3 py-2">
          <Info className="h-4 w-4 mt-0.5 shrink-0 text-muted-foreground" />
          <p className="text-xs text-muted-foreground">{t("export.infoNote")}</p>
        </div>

        <div>
          <Label className="mb-1.5">{t("export.agent")}</Label>
          <Combobox value={agentId} onChange={setAgentId} options={agentOptions} placeholder={t("export.agentPlaceholder")} />
        </div>

        {agentId && (
          <>
            <div className="flex items-center gap-2">
              <span className="text-sm text-muted-foreground">{t("export.presetsLabel")}:</span>
              {PRESET_KEYS.map((p) => (
                <Button
                  key={p}
                  variant={PRESETS[p] && setsEqual(selected, PRESETS[p]) ? "default" : "outline"}
                  size="xs"
                  onClick={() => handlePreset(p)}
                >
                  {t(`presets.${p}`)}
                </Button>
              ))}
            </div>

            <SectionPicker sections={sections} selected={selected} onChange={setSelected} />

            <div className="flex items-center justify-between pt-2">
              <span className="text-sm text-muted-foreground">
                {agent && (agent.display_name || agent.agent_key)}
              </span>
              <Button onClick={handleExport} disabled={!agentId || selected.size === 0}>
                <Package className="mr-1.5 h-4 w-4" />
                {t("export.startExport")}
              </Button>
            </div>
          </>
        )}
      </div>
    );
  }

  // Running / complete / error
  return (
    <div className="space-y-4">
      <h3 className="text-sm font-medium">
        {exp.status === "complete" ? t("export.done") : exp.status === "error" ? t("export.errorTitle") : t("export.exporting")}
      </h3>
      <OperationProgress steps={exp.steps} elapsed={exp.elapsed} />
      {exp.status === "error" && exp.error && (
        <p className="text-sm text-destructive">{exp.error.detail}</p>
      )}
      <div className="flex items-center justify-end gap-2 pt-2">
        {exp.status === "running" && <Button variant="outline" onClick={exp.cancel}>{t("common.cancel", { ns: "common" })}</Button>}
        {exp.status === "complete" && exp.downloadReady && (
          <>
            <Button variant="outline" onClick={exp.reset}>{t("export.startExport")}</Button>
            <Button onClick={exp.download}><Download className="mr-1.5 h-4 w-4" />{t("export.download")}</Button>
          </>
        )}
        {exp.status === "error" && <Button variant="outline" onClick={exp.reset}>{t("export.startExport")}</Button>}
      </div>
    </div>
  );
}

function setsEqual(a: ReadonlySet<string>, b: ReadonlySet<string>): boolean {
  if (a.size !== b.size) return false;
  for (const v of a) if (!b.has(v)) return false;
  return true;
}
