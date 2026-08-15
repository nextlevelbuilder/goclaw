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
import { Label } from "@/components/ui/label";
import { PromptModeCards, type PromptMode } from "./prompt-mode-cards";

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
      if (data.promptMode && data.promptMode !== "full") {
        otherConfig.prompt_mode = data.promptMode;
      }
      await onCreate({
        agent_key: data.agentKey,
        display_name: data.displayName || undefined,
        provider: data.provider,
        model: data.model,
        agent_type: data.agentType,
        // Promoted fields at top level
        emoji: data.emoji?.trim() || null,
        agent_description: data.description?.trim() || null,
        self_evolve: data.selfEvolve || false,
        ...(Object.keys(otherConfig).length > 0 && { other_config: otherConfig }),
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
    !!watch("description")?.trim();

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

          {/* Prompt Mode selector */}
          <div className="space-y-1.5">
            <Label>{t("detail.prompt.title")}</Label>
            <PromptModeCards
              value={(watch("promptMode") ?? "full") as PromptMode}
              onChange={(m) => setValue("promptMode", m === "full" ? undefined : m)}
              compact
            />
          </div>

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
