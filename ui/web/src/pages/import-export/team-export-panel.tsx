import { useState, useMemo, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Download, Package, Info, CheckCircle2, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Combobox } from "@/components/ui/combobox";
import { useTeamExportPreview, useTeamExport } from "./hooks/use-team-export";
import type { ProgressStep } from "@/components/shared/operation-progress";
import type { TeamData } from "@/types/team";

/** Renders team export steps as a tree: Team node + per-agent nodes with section breakdown. */
function ExportTree({ steps, elapsed, teamLabel }: { steps: ProgressStep[]; elapsed: number; teamLabel: string }) {
  const teamStep = steps.find((s) => s.id === "team");
  const agentSteps = steps.filter((s) => s.id !== "team" && s.id !== "workspace");

  return (
    <div className="rounded-lg border bg-card text-sm">
      {/* Team node */}
      {teamStep && (
        <div className="flex items-center gap-2 px-4 py-2.5 border-b">
          <StepIcon status={teamStep.status} />
          <span className="font-medium">{teamLabel}</span>
          {teamStep.detail && (
            <span className="text-xs text-muted-foreground ml-auto">{teamStep.detail}</span>
          )}
        </div>
      )}

      {/* Agent nodes */}
      <div className="divide-y">
        {agentSteps.map((step) => {
          const sections = step.detail?.split(" · ").filter(Boolean) ?? [];
          return (
            <div key={step.id} className="px-4 py-2.5">
              <div className="flex items-center gap-2">
                <StepIcon status={step.status} />
                <span className="font-medium">{step.label}</span>
              </div>
              {sections.length > 0 && (
                <ul className="mt-1.5 ml-6 space-y-0.5">
                  {sections.map((s, i) => (
                    <li key={i} className="text-xs text-muted-foreground flex items-center gap-1.5">
                      <span className="h-1 w-1 rounded-full bg-muted-foreground/30 shrink-0" />
                      {s}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          );
        })}
      </div>

      {/* Elapsed */}
      <div className="text-xs text-muted-foreground px-4 py-2 border-t">
        {elapsed < 60 ? `${elapsed}s` : `${Math.floor(elapsed / 60)}m ${elapsed % 60}s`}
      </div>
    </div>
  );
}

function StepIcon({ status }: { status: string }) {
  if (status === "done") return <CheckCircle2 className="h-4 w-4 shrink-0 text-green-500" />;
  return <Loader2 className="h-4 w-4 shrink-0 text-blue-500 animate-spin" />;
}

interface TeamExportPanelProps {
  teams: TeamData[];
  loading: boolean;
  loadTeams: () => void;
}

export function TeamExportPanel({ teams, loading, loadTeams }: TeamExportPanelProps) {
  const { t } = useTranslation("import-export");
  const [teamId, setTeamId] = useState("");

  useEffect(() => { loadTeams(); }, [loadTeams]);

  const { data: preview, isLoading: previewLoading, error: previewError } = useTeamExportPreview(teamId || null);
  const exp = useTeamExport();

  const teamOptions = useMemo(
    () => teams.map((t) => ({ value: t.id, label: t.name })),
    [teams],
  );

  const team = teams.find((t) => t.id === teamId);

  if (exp.status !== "idle") {
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium">
          {exp.status === "complete" ? t("export.done") : exp.status === "error" ? t("export.errorTitle") : t("export.exporting")}
        </h3>
        <ExportTree steps={exp.steps} elapsed={exp.elapsed} teamLabel={t("tabs.teams")} />
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

  return (
    <div className="space-y-4">
      <div className="flex items-start gap-2 rounded-md border bg-muted/30 px-3 py-2">
        <Info className="h-4 w-4 mt-0.5 shrink-0 text-muted-foreground" />
        <p className="text-xs text-muted-foreground">{t("teamExportNote")}</p>
      </div>

      <div>
        <Label className="mb-1.5">{t("tabs.teams")}</Label>
        <Combobox
          value={teamId}
          onChange={setTeamId}
          options={teamOptions}
          placeholder={loading ? t("export.previewLoading") : t("teamSelectPlaceholder")}
        />
      </div>

      {teamId && previewLoading && (
        <p className="text-sm text-muted-foreground">{t("teamPreview.loading")}</p>
      )}

      {teamId && previewError && (
        <p className="text-sm text-destructive">{t("teamPreview.error")}</p>
      )}

      {teamId && preview && (
        <>
          <div className="rounded-md border bg-muted/50 p-3 text-sm space-y-1">
            <p className="font-medium">{preview.team_name}</p>
            <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
              <span>{t("teamPreview.members", { count: preview.members })}</span>
              <span>{t("teamPreview.agents", { count: preview.agent_count })}</span>
              <span>{t("teamPreview.tasks", { count: preview.tasks })}</span>
              {preview.agent_links > 0 && <span>{t("teamPreview.links", { count: preview.agent_links })}</span>}
            </div>
          </div>

          <div className="flex items-center justify-between pt-2">
            <span className="text-sm text-muted-foreground">{team?.name}</span>
            <Button onClick={() => exp.startExport(teamId)} disabled={!teamId}>
              <Package className="mr-1.5 h-4 w-4" />
              {t("export.startExport")}
            </Button>
          </div>
        </>
      )}

      {teamId && !previewLoading && !previewError && !preview && (
        <div className="flex items-center justify-end pt-2">
          <Button onClick={() => exp.startExport(teamId)} disabled={!teamId}>
            <Package className="mr-1.5 h-4 w-4" />
            {t("export.startExport")}
          </Button>
        </div>
      )}
    </div>
  );
}
