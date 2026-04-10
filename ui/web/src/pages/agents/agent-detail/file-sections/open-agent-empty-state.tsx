import { useTranslation } from "react-i18next";
import { FileText, Info } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { BootstrapFile } from "@/types/agent";
import { FILE_DESCRIPTIONS } from "./file-utils";

interface OpenAgentEmptyStateProps {
  files: BootstrapFile[];
}

export function OpenAgentEmptyState({ files }: OpenAgentEmptyStateProps) {
  const { t } = useTranslation("agents");
  return (
    <div className="max-w-2xl space-y-4">
      <div className="flex items-start gap-3 rounded-lg border border-info/30 bg-sky-500/5 p-4">
        <Info className="mt-0.5 h-5 w-5 shrink-0 text-sky-600 dark:text-sky-400" />
        <div className="space-y-2 text-sm">
          <p className="font-medium">{t("files.openAgentTitle")}</p>
          <p className="text-muted-foreground">
            {t("files.openAgentDesc1")}
          </p>
          <p className="text-muted-foreground">
            {t("files.openAgentDesc2pre")}
            <code className="rounded bg-muted px-1 py-0.5 text-xs">
              user_context_files
            </code>
            {t("files.openAgentDesc2post")}
          </p>
        </div>
      </div>

      <div className="rounded-lg border p-4">
        <h4 className="mb-3 text-sm font-medium">{t("files.contextFiles")}</h4>
        <div className="space-y-2">
          {files.map((file) => (
            <div
              key={file.name}
              className="flex items-center gap-3 rounded-md bg-muted/50 px-3 py-2"
            >
              <FileText className="h-4 w-4 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">{file.name}</div>
                <div className="text-xs text-muted-foreground">
                  {FILE_DESCRIPTIONS[file.name] || t("files.contextFile")}
                </div>
              </div>
              <Badge variant="outline" className="shrink-0 text-2xs">
                {t("files.perUser")}
              </Badge>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
