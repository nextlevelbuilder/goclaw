import { useState } from "react";
import { Download } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";

interface TeamsAppPackageButtonProps {
  instanceId: string;
  displayName: string;
}

export function TeamsAppPackageButton({
  instanceId,
  displayName,
}: TeamsAppPackageButtonProps) {
  const { t } = useTranslation("channels");
  const http = useHttp();
  const [loading, setLoading] = useState(false);

  const handleDownload = async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams({
        instance_id: instanceId,
        name: displayName || "GoClaw Bot",
      });
      const blob = await http.downloadBlob(
        `/v1/teams/app-package?${params}`,
      );
      const a = document.createElement("a");
      a.href = URL.createObjectURL(blob);
      a.download = `teams-app-${displayName || "bot"}.zip`;
      a.click();
      URL.revokeObjectURL(a.href);
    } catch {
      toast.error(
        t("teams.appPackage.error", {
          defaultValue: "Failed to generate app package",
        }),
      );
    } finally {
      setLoading(false);
    }
  };

  return (
    <Button
      variant="outline"
      size="sm"
      onClick={handleDownload}
      disabled={loading}
    >
      <Download className="mr-1.5 h-4 w-4" />
      {loading
        ? t("teams.appPackage.generating", { defaultValue: "Generating..." })
        : t("teams.appPackage.download", {
            defaultValue: "Download App Package",
          })}
    </Button>
  );
}
