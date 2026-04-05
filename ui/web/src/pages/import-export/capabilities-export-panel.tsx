import { useTranslation } from "react-i18next";
import { Download, Package, Info } from "lucide-react";
import { Button } from "@/components/ui/button";
import { OperationProgress } from "@/components/shared/operation-progress";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import {
  useSkillsExportPreview,
  useMcpExportPreview,
  useSkillsExport,
  useMcpExport,
} from "./hooks/use-capabilities-export";

function SkillsExportTab() {
  const { t } = useTranslation("import-export");
  const { data: preview, isLoading } = useSkillsExportPreview();
  const exp = useSkillsExport();

  if (exp.status !== "idle") {
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium">
          {exp.status === "complete"
            ? t("export.done")
            : exp.status === "error"
              ? t("export.errorTitle")
              : t("export.exporting")}
        </h3>
        <OperationProgress steps={exp.steps} elapsed={exp.elapsed} />
        {exp.status === "error" && exp.error && (
          <p className="text-sm text-destructive">{exp.error.detail}</p>
        )}
        <div className="flex items-center justify-end gap-2 pt-2">
          {exp.status === "running" && (
            <Button variant="outline" onClick={exp.cancel}>
              {t("common.cancel", { ns: "common" })}
            </Button>
          )}
          {exp.status === "complete" && exp.downloadReady && (
            <>
              <Button variant="outline" onClick={exp.reset}>{t("export.startExport")}</Button>
              <Button onClick={exp.download}>
                <Download className="mr-1.5 h-4 w-4" />
                {t("export.download")}
              </Button>
            </>
          )}
          {exp.status === "error" && (
            <Button variant="outline" onClick={exp.reset}>{t("export.startExport")}</Button>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start gap-2 rounded-md border bg-muted/30 px-3 py-2">
        <Info className="h-4 w-4 mt-0.5 shrink-0 text-muted-foreground" />
        <p className="text-xs text-muted-foreground">{t("skillsMcp.skillsNote")}</p>
      </div>

      {isLoading && (
        <p className="text-sm text-muted-foreground">{t("export.previewLoading")}</p>
      )}

      {preview && (
        <div className="rounded-md border bg-muted/50 p-3 text-sm space-y-1">
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span>{t("skillsMcp.customSkills", { count: preview.custom_skills })}</span>
            <span>{t("skillsMcp.grants", { count: preview.total_grants })}</span>
          </div>
        </div>
      )}

      {preview?.custom_skills === 0 && (
        <p className="text-sm text-muted-foreground">{t("skillsMcp.noSkills")}</p>
      )}

      <div className="flex justify-end pt-2">
        <Button
          onClick={exp.startSkillsExport}
          disabled={isLoading || preview?.custom_skills === 0}
        >
          <Package className="mr-1.5 h-4 w-4" />
          {t("skillsMcp.exportSkills")}
        </Button>
      </div>
    </div>
  );
}

function McpExportTab() {
  const { t } = useTranslation("import-export");
  const { data: preview, isLoading } = useMcpExportPreview();
  const exp = useMcpExport();

  if (exp.status !== "idle") {
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium">
          {exp.status === "complete"
            ? t("export.done")
            : exp.status === "error"
              ? t("export.errorTitle")
              : t("export.exporting")}
        </h3>
        <OperationProgress steps={exp.steps} elapsed={exp.elapsed} />
        {exp.status === "error" && exp.error && (
          <p className="text-sm text-destructive">{exp.error.detail}</p>
        )}
        <div className="flex items-center justify-end gap-2 pt-2">
          {exp.status === "running" && (
            <Button variant="outline" onClick={exp.cancel}>
              {t("common.cancel", { ns: "common" })}
            </Button>
          )}
          {exp.status === "complete" && exp.downloadReady && (
            <>
              <Button variant="outline" onClick={exp.reset}>{t("export.startExport")}</Button>
              <Button onClick={exp.download}>
                <Download className="mr-1.5 h-4 w-4" />
                {t("export.download")}
              </Button>
            </>
          )}
          {exp.status === "error" && (
            <Button variant="outline" onClick={exp.reset}>{t("export.startExport")}</Button>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start gap-2 rounded-md border bg-muted/30 px-3 py-2">
        <Info className="h-4 w-4 mt-0.5 shrink-0 text-muted-foreground" />
        <p className="text-xs text-muted-foreground">{t("skillsMcp.mcpNote")}</p>
      </div>

      {isLoading && (
        <p className="text-sm text-muted-foreground">{t("export.previewLoading")}</p>
      )}

      {preview && (
        <div className="rounded-md border bg-muted/50 p-3 text-sm space-y-1">
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span>{t("skillsMcp.servers", { count: preview.servers })}</span>
            <span>{t("skillsMcp.grants", { count: preview.agent_grants })}</span>
          </div>
        </div>
      )}

      {preview?.servers === 0 && (
        <p className="text-sm text-muted-foreground">{t("skillsMcp.noServers")}</p>
      )}

      <div className="flex justify-end pt-2">
        <Button
          onClick={exp.startMcpExport}
          disabled={isLoading || preview?.servers === 0}
        >
          <Package className="mr-1.5 h-4 w-4" />
          {t("skillsMcp.exportMcp")}
        </Button>
      </div>
    </div>
  );
}

export function CapabilitiesExportPanel() {
  const { t } = useTranslation("import-export");

  return (
    <Tabs defaultValue="skills">
      <TabsList>
        <TabsTrigger value="skills">{t("skillsMcp.skillsTab")}</TabsTrigger>
        <TabsTrigger value="mcp">{t("skillsMcp.mcpTab")}</TabsTrigger>
      </TabsList>
      <TabsContent value="skills" className="mt-4">
        <SkillsExportTab />
      </TabsContent>
      <TabsContent value="mcp" className="mt-4">
        <McpExportTab />
      </TabsContent>
    </Tabs>
  );
}
