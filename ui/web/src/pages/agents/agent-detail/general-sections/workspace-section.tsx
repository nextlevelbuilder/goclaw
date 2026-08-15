import { useTranslation } from "react-i18next";
import { Label } from "@/components/ui/label";

interface WorkspaceSectionProps {
  workspace: string;
}

export function WorkspaceSection({ workspace }: WorkspaceSectionProps) {
  const { t } = useTranslation("agents");
  return (
    <section className="space-y-4">
      <h3 className="text-sm font-medium text-muted-foreground">{t("workspace.title")}</h3>
      <div className="space-y-4 rounded-lg border p-4">
        <div className="space-y-2">
          <Label>{t("workspace.workspacePath")}</Label>
          <p className="rounded-md border bg-muted/50 px-3 py-2 font-mono text-sm text-muted-foreground">
            {workspace || t("workspace.noWorkspace")}
          </p>
          <p className="text-xs text-muted-foreground">
            {t("workspace.workspacePathHint")}
          </p>
        </div>
      </div>
    </section>
  );
}
