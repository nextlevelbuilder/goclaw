import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { PageHeader } from "@/components/shared/page-header";
import { cn } from "@/lib/utils";
import { BuiltinToolsPage } from "@/pages/builtin-tools/builtin-tools-page";
import { CustomToolsPage } from "@/pages/custom-tools/custom-tools-page";
import { ManagedToolsTab } from "./managed-tools-tab";

type Tab = "core" | "custom" | "managed";
const TABS: Tab[] = ["core", "custom", "managed"];

function isValidTab(v: string | null): v is Tab {
  return v === "core" || v === "custom" || v === "managed";
}

export function ToolsPage() {
  const { t } = useTranslation("tools");
  const [searchParams, setSearchParams] = useSearchParams();
  const rawTab = searchParams.get("tab");
  const tab: Tab = isValidTab(rawTab) ? rawTab : "core";

  const handleTabChange = (newTab: Tab) => {
    setSearchParams({ tab: newTab }, { replace: true });
  };

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title={t("title")}
        description={t("description")}
      />

      {/* Tabs */}
      <div className="flex gap-1 border-b mt-4">
        {TABS.map((t_) => (
          <button
            key={t_}
            type="button"
            className={cn(
              "px-3 py-1.5 text-sm font-medium border-b-2 -mb-px",
              tab === t_
                ? "border-primary text-primary"
                : "border-transparent text-muted-foreground hover:text-foreground",
            )}
            onClick={() => handleTabChange(t_)}
          >
            {t(`tabs.${t_}`)}
          </button>
        ))}
      </div>

      {/* Tab content */}
      {tab === "core" && (
        <div className="-m-4 sm:-m-6">
          <BuiltinToolsPage />
        </div>
      )}
      {tab === "custom" && (
        <div className="-m-4 sm:-m-6">
          <CustomToolsPage />
        </div>
      )}
      {tab === "managed" && <ManagedToolsTab />}
    </div>
  );
}
