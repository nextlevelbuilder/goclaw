import { useState, lazy, Suspense } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useProviders } from "../hooks/use-providers";
import { ProviderHeader } from "./provider-header";
import { ProviderOverview } from "./provider-overview";
import { ProviderUsageTab } from "./provider-usage-tab";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { DetailPageSkeleton } from "@/components/shared/loading-skeleton";

const DETAIL_TABS = ["connection", "usage"] as const;
type DetailTab = (typeof DETAIL_TABS)[number];

function parseDetailTab(v: string | null): DetailTab {
  return v === "usage" ? "usage" : "connection";
}

const ProviderAdvancedDialog = lazy(() =>
  import("./provider-advanced-dialog").then((m) => ({ default: m.ProviderAdvancedDialog }))
);

interface ProviderDetailPageProps {
  providerId: string;
  onBack: () => void;
}

export function ProviderDetailPage({ providerId, onBack }: ProviderDetailPageProps) {
  const { t } = useTranslation("providers");
  const { providers, loading, updateProvider, deleteProvider } = useProviders();
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [params, setParams] = useSearchParams();
  // Active tab is derived from the URL (?tab=usage) so reload and back navigation keep it.
  const activeTab = parseDetailTab(params.get("tab"));
  const setActiveTab = (value: string) => {
    const next = new URLSearchParams(params);
    if (value === "usage") {
      next.set("tab", value);
    } else {
      next.delete("tab");
      next.delete("range");
    }
    setParams(next, { replace: true });
  };

  const provider = providers.find((p) => p.id === providerId);

  if (loading || !provider) {
    return <DetailPageSkeleton tabs={DETAIL_TABS.length} />;
  }

  const displayTitle = provider.display_name || provider.name;

  return (
    <div>
      <ProviderHeader
        provider={provider}
        onBack={onBack}
        onAdvanced={() => setAdvancedOpen(true)}
        onDelete={() => setDeleteOpen(true)}
      />

      <div className="p-3 sm:p-4">
        <div className="max-w-4xl">
          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList className="w-full justify-start overflow-x-auto overflow-y-hidden">
              <TabsTrigger value="connection">{t("detail.tabs.connection")}</TabsTrigger>
              <TabsTrigger value="usage">{t("detail.tabs.usage")}</TabsTrigger>
            </TabsList>

            <TabsContent value="connection" className="mt-4">
              <ProviderOverview
                key={provider.id + "-" + provider.updated_at}
                provider={provider}
                onUpdate={updateProvider}
              />
            </TabsContent>
            <TabsContent value="usage" className="mt-4">
              <ProviderUsageTab provider={provider} />
            </TabsContent>
          </Tabs>
        </div>
      </div>

      <Suspense fallback={null}>
        <ProviderAdvancedDialog
          key={provider.id}
          open={advancedOpen}
          onOpenChange={setAdvancedOpen}
          provider={provider}
          onUpdate={updateProvider}
        />
      </Suspense>

      <ConfirmDeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t("delete.title")}
        description={t("delete.description", { name: displayTitle })}
        confirmValue={displayTitle}
        confirmLabel={t("delete.confirmLabel")}
        onConfirm={async () => {
          await deleteProvider(providerId);
          setDeleteOpen(false);
          onBack();
        }}
      />
    </div>
  );
}
