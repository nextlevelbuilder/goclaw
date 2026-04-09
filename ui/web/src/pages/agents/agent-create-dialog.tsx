import { useMemo, useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import type { AgentData } from "@/types/agent";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useProviderModels } from "@/pages/providers/hooks/use-provider-models";
import { useProviderVerify } from "@/pages/providers/hooks/use-provider-verify";
import { getChatGPTOAuthPoolOwnership } from "@/pages/providers/provider-utils";
import { useAgentPresets } from "./agent-presets";
import { agentCreateSchema, type AgentCreateFormData } from "@/schemas/agent.schema";
import { AgentIdentityAndModelFields } from "./agent-identity-and-model-fields";
import { AgentDescriptionSection } from "./agent-description-section";

interface AgentCreateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (data: Partial<AgentData>) => Promise<unknown>;
}

export function AgentCreateDialog({ open, onOpenChange, onCreate }: AgentCreateDialogProps) {
  const { t } = useTranslation("agents");
  const agentPresets = useAgentPresets();
  const { providers, refresh: refreshProviders } = useProviders();
  const [loading, setLoading] = useState(false);
  const [submitError, setSubmitError] = useState("");

  const form = useForm<AgentCreateFormData>({
    resolver: zodResolver(agentCreateSchema),
    mode: "onChange",
    defaultValues: {
      emoji: "",
      displayName: "",
      agentKey: "",
      provider: "",
      model: "",
      agentType: "predefined",
      description: "",
      selfEvolve: false,
    },
  });

  const { handleSubmit, watch, setValue, reset, formState: { errors } } = form;

  const provider = watch("provider");
  const model = watch("model");
  const agentType = watch("agentType");
  const agentKey = watch("agentKey");
  const displayName = watch("displayName");

  const poolOwnership = useMemo(() => getChatGPTOAuthPoolOwnership(providers), [providers]);
  const enabledProviders = useMemo(
    () => providers.filter((p) => p.enabled && !poolOwnership.ownerByMember.has(p.name)),
    [providers, poolOwnership],
  );
  const poolOwnerNames = useMemo(
    () => new Set(poolOwnership.membersByOwner.keys()),
    [poolOwnership],
  );
  const selectedProvider = useMemo(
    () => enabledProviders.find((p) => p.name === provider),
    [enabledProviders, provider],
  );
  const selectedProviderId = selectedProvider?.id;
  const { models, loading: modelsLoading } = useProviderModels(selectedProviderId);
  const { verify, verifying, result: verifyResult, reset: resetVerify } = useProviderVerify();

  useEffect(() => { resetVerify(); }, [provider, model, resetVerify]);

  useEffect(() => {
    if (open) {
      refreshProviders();
    } else {
      reset();
      setSubmitError("");
      resetVerify();
    }
  }, [open, reset, resetVerify, refreshProviders]);

  const handleVerify = async () => {
    if (!selectedProviderId || !model.trim()) return;
    await verify(selectedProviderId, model.trim());
  };

  const handleSubmitForm = async (data: AgentCreateFormData) => {
    setLoading(true);
    setSubmitError("");
    try {
      const otherConfig: Record<string, unknown> = {};
      if (data.emoji?.trim()) otherConfig.emoji = data.emoji.trim();
      if (data.description?.trim()) otherConfig.description = data.description.trim();
      if (data.selfEvolve) otherConfig.self_evolve = true;
      await onCreate({
        agent_key: data.agentKey,
        display_name: data.displayName || undefined,
        provider: data.provider,
        model: data.model,
        agent_type: data.agentType,
        other_config: Object.keys(otherConfig).length > 0 ? otherConfig : undefined,
      });
      onOpenChange(false);
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : t("create.failedToCreate"));
    } finally {
      setLoading(false);
    }
  };

  const handleProviderChange = (value: string) => {
    setValue("provider", value, { shouldValidate: true });
    setValue("model", "", { shouldValidate: false });
  };

  const canCreate = !!agentKey && !!displayName && !!provider && !!model &&
    !errors.agentKey && !errors.displayName &&
    (agentType !== "predefined" || !!watch("description")?.trim());

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-4xl max-h-[90vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{t("create.title")}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4 py-4 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
          <AgentIdentityAndModelFields
            form={form}
            enabledProviders={enabledProviders}
            poolOwnerNames={poolOwnerNames}
            models={models}
            modelsLoading={modelsLoading}
            verifying={verifying}
            verifyResult={verifyResult}
            onProviderChange={handleProviderChange}
            onVerify={handleVerify}
          />
          <AgentDescriptionSection form={form} agentPresets={agentPresets} />
          {submitError && <p className="text-sm text-destructive">{submitError}</p>}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            {t("create.cancel")}
          </Button>
          {loading ? (
            <Button disabled>{t("create.creating")}</Button>
          ) : (
            <Button onClick={handleSubmit(handleSubmitForm)} disabled={!canCreate || loading}>
              {t("create.create")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
