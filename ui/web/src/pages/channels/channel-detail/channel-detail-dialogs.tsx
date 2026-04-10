import { useTranslation } from "react-i18next";
import { ChannelAdvancedDialog } from "./channel-advanced-dialog";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { reauthDialogs } from "../channel-wizard-registry";
import type { ChannelInstanceData } from "@/types/channel";
import type { ComponentType } from "react";
import type { ReauthDialogProps } from "../channel-wizard-registry";

interface ChannelDetailDialogsProps {
  instance: ChannelInstanceData;
  advancedOpen: boolean;
  setAdvancedOpen: (v: boolean) => void;
  reauthOpen: boolean;
  setReauthOpen: (v: boolean) => void;
  deleteOpen: boolean;
  setDeleteOpen: (v: boolean) => void;
  supportsReauth: boolean;
  onDelete?: (instance: { id: string; name: string }) => void;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

export function ChannelDetailDialogs({
  instance,
  advancedOpen,
  setAdvancedOpen,
  reauthOpen,
  setReauthOpen,
  deleteOpen,
  setDeleteOpen,
  supportsReauth,
  onDelete,
  onUpdate,
}: ChannelDetailDialogsProps) {
  const { t } = useTranslation("channels");

  const ReauthDialog: ComponentType<ReauthDialogProps> | null = supportsReauth
    ? (reauthDialogs[instance.channel_type] ?? null)
    : null;

  return (
    <>
      <ChannelAdvancedDialog
        open={advancedOpen}
        onOpenChange={setAdvancedOpen}
        instance={instance}
        onUpdate={onUpdate}
      />

      {ReauthDialog && (
        <ReauthDialog
          open={reauthOpen}
          onOpenChange={setReauthOpen}
          instanceId={instance.id}
          instanceName={instance.display_name || instance.name}
          onSuccess={() => setReauthOpen(false)}
        />
      )}

      {onDelete && (
        <ConfirmDeleteDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          title={t("delete.title")}
          description={t("delete.description", {
            name: instance.display_name || instance.name,
          })}
          confirmValue={instance.display_name || instance.name}
          confirmLabel={t("delete.confirmLabel")}
          onConfirm={async () => {
            onDelete({
              id: instance.id,
              name: instance.display_name || instance.name,
            });
            setDeleteOpen(false);
          }}
        />
      )}
    </>
  );
}
