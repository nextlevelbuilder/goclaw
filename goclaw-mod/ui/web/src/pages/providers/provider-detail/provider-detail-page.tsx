import { useState, lazy, Suspense } from "react";
import { useTranslation } from "react-i18next";
import { useProviders } from "../hooks/use-providers";
import { ProviderHeader } from "./provider-header";
import { ProviderOverview } from "./provider-overview";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { DetailPageSkeleton } from "@/components/shared/loading-skeleton";

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

  const provider = providers.find((p) => p.id === providerId);

  if (loading || !provider) {
    return <DetailPageSkeleton tabs={0} />;
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
          <ProviderOverview
            key={provider.id + "-" + provider.updated_at}
            provider={provider}
            onUpdate={updateProvider}
          />
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
