import { useState, useMemo, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { toast } from "@/stores/use-toast-store";
import {
  buildProviderSettingsWithChatGPTOAuthRouting,
} from "@/types/provider";
import type { EffectiveChatGPTOAuthRoutingStrategy } from "@/types/agent";
import type { ProviderData, ProviderInput } from "@/types/provider";

interface PoolSetupWizardDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  providers: ProviderData[];
  unpooledProviders: ProviderData[];
  onSave: (ownerId: string, data: ProviderInput) => Promise<void>;
}

const STRATEGIES: { value: EffectiveChatGPTOAuthRoutingStrategy; labelKey: string }[] = [
  { value: "primary_first", labelKey: "list.strategy.primaryFirst" },
  { value: "round_robin", labelKey: "list.strategy.roundRobin" },
  { value: "priority_order", labelKey: "list.strategy.priorityOrder" },
];

export function PoolSetupWizardDialog({
  open,
  onOpenChange,
  unpooledProviders,
  onSave,
}: PoolSetupWizardDialogProps) {
  const { t } = useTranslation("providers");

  const [ownerId, setOwnerId] = useState<string>(() => unpooledProviders[0]?.id ?? "");
  const [selectedMemberIds, setSelectedMemberIds] = useState<Set<string>>(
    () => new Set(unpooledProviders.slice(1).map((p) => p.id)),
  );
  const [strategy, setStrategy] = useState<EffectiveChatGPTOAuthRoutingStrategy>("primary_first");
  const [saving, setSaving] = useState(false);

  // When owner changes, auto-exclude owner from members
  const handleOwnerChange = useCallback((id: string) => {
    setOwnerId(id);
    setSelectedMemberIds((prev) => {
      const next = new Set(prev);
      next.delete(id);
      // add the previously-selected owner back as a potential member
      if (ownerId && ownerId !== id) {
        next.add(ownerId);
      }
      return next;
    });
  }, [ownerId]);

  const ownerProvider = useMemo(
    () => unpooledProviders.find((p) => p.id === ownerId),
    [unpooledProviders, ownerId],
  );

  // Members list = all unpooled except the current owner
  const memberCandidates = useMemo(
    () => unpooledProviders.filter((p) => p.id !== ownerId),
    [unpooledProviders, ownerId],
  );

  const selectedCount = useMemo(
    () => memberCandidates.filter((p) => selectedMemberIds.has(p.id)).length,
    [memberCandidates, selectedMemberIds],
  );

  const handleToggleMember = (id: string) => {
    setSelectedMemberIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const handleSelectAll = () => {
    setSelectedMemberIds(new Set(memberCandidates.filter((p) => p.enabled).map((p) => p.id)));
  };

  const handleDeselectAll = () => {
    setSelectedMemberIds(new Set());
  };

  const canCreate = ownerProvider && selectedCount > 0;

  const handleCreate = async () => {
    if (!ownerProvider) return;
    const selectedMembers = memberCandidates.filter((p) => selectedMemberIds.has(p.id));
    if (selectedMembers.length === 0) return;

    setSaving(true);
    try {
      const settings = buildProviderSettingsWithChatGPTOAuthRouting(
        ownerProvider.settings,
        {
          strategy,
          extra_provider_names: selectedMembers.map((p) => p.name),
        },
      );
      await onSave(ownerProvider.id, {
        name: ownerProvider.name,
        provider_type: "chatgpt_oauth",
        settings,
      });
      toast.success(t("poolWizard.success"));
      onOpenChange(false);
    } catch {
      toast.error(t("poolWizard.error"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex flex-col gap-0 p-0 sm:max-w-lg max-sm:inset-0 max-sm:overflow-y-auto">
        <DialogHeader className="px-4 pt-4 sm:px-6 sm:pt-6 pb-3">
          <DialogTitle>{t("poolWizard.title")}</DialogTitle>
          <DialogDescription>{t("poolWizard.description")}</DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4 overflow-y-auto px-4 sm:px-6 pb-4 max-h-[60vh] sm:max-h-[440px]">
          {/* Pool Owner */}
          <section>
            <p className="text-sm font-medium mb-1">{t("poolWizard.selectOwner")}</p>
            <p className="text-xs text-muted-foreground mb-2">{t("poolWizard.selectOwnerHint")}</p>
            <div className="rounded-md border divide-y overflow-hidden">
              {unpooledProviders.map((provider) => (
                <label
                  key={provider.id}
                  className="flex items-center gap-3 px-3 py-2 cursor-pointer hover:bg-muted/40 transition-colors"
                >
                  <input
                    type="radio"
                    name="pool-owner"
                    value={provider.id}
                    checked={ownerId === provider.id}
                    onChange={() => handleOwnerChange(provider.id)}
                    className="accent-primary"
                  />
                  <div className="flex-1 min-w-0">
                    <span className="block text-base md:text-sm font-medium truncate">{provider.display_name || provider.name}</span>
                    {provider.display_name && provider.display_name !== provider.name && (
                      <span className="block text-xs text-muted-foreground truncate">{provider.name}</span>
                    )}
                  </div>
                  <ProviderPlanBadge provider={provider} />
                </label>
              ))}
            </div>
          </section>

          {/* Pool Members */}
          <section>
            <p className="text-sm font-medium mb-1">{t("poolWizard.selectMembers")}</p>
            <p className="text-xs text-muted-foreground mb-2">{t("poolWizard.selectMembersHint")}</p>
            <div className="rounded-md border divide-y overflow-hidden">
              {memberCandidates.length === 0 && (
                <div className="px-3 py-2 text-sm text-muted-foreground">{t("poolWizard.noMembers")}</div>
              )}
              {memberCandidates.map((provider) => {
                const isDisabled = !provider.enabled;
                return (
                  <label
                    key={provider.id}
                    className={[
                      "flex items-center gap-3 px-3 py-2 transition-colors",
                      isDisabled
                        ? "cursor-not-allowed opacity-50"
                        : "cursor-pointer hover:bg-muted/40",
                    ].join(" ")}
                  >
                    <input
                      type="checkbox"
                      checked={selectedMemberIds.has(provider.id)}
                      onChange={() => !isDisabled && handleToggleMember(provider.id)}
                      disabled={isDisabled}
                      className="accent-primary"
                    />
                    <div className="flex-1 min-w-0">
                      <span className="block text-base md:text-sm font-medium truncate">{provider.display_name || provider.name}</span>
                      {provider.display_name && provider.display_name !== provider.name && (
                        <span className="block text-xs text-muted-foreground truncate">{provider.name}</span>
                      )}
                    </div>
                    <ProviderPlanBadge provider={provider} />
                  </label>
                );
              })}
            </div>

            {memberCandidates.length > 0 && (
              <div className="flex items-center gap-2 mt-2">
                <button
                  type="button"
                  onClick={handleSelectAll}
                  className="text-xs text-primary hover:underline"
                >
                  {t("poolWizard.selectAll")}
                </button>
                <span className="text-xs text-muted-foreground">·</span>
                <button
                  type="button"
                  onClick={handleDeselectAll}
                  className="text-xs text-primary hover:underline"
                >
                  {t("poolWizard.deselectAll")}
                </button>
                <span className="ml-auto text-xs text-muted-foreground">
                  {t("poolWizard.selectedCount", { count: selectedCount })}
                </span>
              </div>
            )}
          </section>

          {/* Strategy */}
          <section>
            <p className="text-sm font-medium mb-2">{t("poolWizard.strategy")}</p>
            <div className="flex flex-wrap gap-2">
              {STRATEGIES.map((s) => (
                <button
                  key={s.value}
                  type="button"
                  onClick={() => setStrategy(s.value)}
                  className={[
                    "rounded-md border px-3 py-1.5 text-xs font-medium transition-colors",
                    strategy === s.value
                      ? "border-primary bg-primary text-primary-foreground"
                      : "border-border bg-background hover:bg-muted/40",
                  ].join(" ")}
                >
                  {t(s.labelKey)}
                </button>
              ))}
            </div>
          </section>
        </div>

        <DialogFooter className="px-4 pb-4 sm:px-6 sm:pb-6 pt-3 border-t">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("form.cancel")}
          </Button>
          <Button onClick={handleCreate} disabled={!canCreate || saving}>
            {saving && <Loader2 className="h-4 w-4 animate-spin mr-1" />}
            {t("poolWizard.createPool")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ProviderPlanBadge({ provider }: { provider: ProviderData }) {
  const { t } = useTranslation("common");
  if (!provider.enabled) {
    return (
      <span className="text-2xs text-muted-foreground shrink-0">{t("disabled")}</span>
    );
  }
  // plan_type is not available at this level without quota data, show nothing
  return null;
}
