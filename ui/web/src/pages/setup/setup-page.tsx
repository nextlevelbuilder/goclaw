import { useState, useEffect } from "react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { useBootstrapStatus, type SetupStep } from "./hooks/use-bootstrap-status";
import { SetupLayout } from "./setup-layout";
import { SetupStepper } from "./setup-stepper";
import { StepProvider } from "./step-provider";
import { StepModel } from "./step-model";
import { StepAgent } from "./step-agent";
import { StepChannel } from "./step-channel";
import { SetupCompleteModal } from "./setup-complete-modal";
import { Building2 } from "lucide-react";
import { ROUTES, SUPPORTED_LANGUAGES, LANGUAGE_LABELS, LOCAL_STORAGE_KEYS } from "@/lib/constants";
import { markSetupSkipped } from "@/lib/setup-skip";
import { useChatGPTOAuthProviderStatuses } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import { useAuthStore } from "@/stores/use-auth-store";
import { useUiStore } from "@/stores/use-ui-store";
import { useTenants } from "@/hooks/use-tenants";
import type { ProviderData } from "@/types/provider";
import type { AgentData } from "@/types/agent";

function PageLoader() {
  return (
    <div className="flex h-32 items-center justify-center">
      <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
    </div>
  );
}

function LanguageSelector() {
  const { language, setLanguage } = useUiStore();
  return (
    <div className="flex items-center gap-1.5">
      {SUPPORTED_LANGUAGES.map((lang) => (
        <button
          key={lang}
          type="button"
          onClick={() => setLanguage(lang)}
          className={`text-xs px-2 py-1 rounded transition-colors ${
            language === lang
              ? "text-foreground font-medium bg-muted"
              : "text-muted-foreground hover:text-foreground"
          }`}
        >
          {LANGUAGE_LABELS[lang]}
        </button>
      ))}
    </div>
  );
}

function TenantSwitcher() {
  const { t } = useTranslation("setup");
  const { currentTenantName, isMultiTenant } = useTenants();
  const navigate = useNavigate();

  if (!isMultiTenant) return null;

  const label = currentTenantName;

  const handleSwitch = () => {
    // Clear tenant selection and go to selector
    localStorage.removeItem(LOCAL_STORAGE_KEYS.TENANT_ID);
    navigate(ROUTES.SELECT_TENANT, { replace: true });
  };

  return (
    <button
      type="button"
      onClick={handleSwitch}
      className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
      title={t("switchTenant", { defaultValue: "Switch tenant" })}
    >
      <Building2 className="h-3.5 w-3.5" />
      <span>{label}</span>
      <span className="text-2xs underline underline-offset-2">{t("switchTenant", { defaultValue: "Switch" })}</span>
    </button>
  );
}

export function SetupPage() {
  const { t } = useTranslation("setup");
  const navigate = useNavigate();
  const userId = useAuthStore((s) => s.userId);
  const { currentTenantId, currentTenantSlug } = useTenants();
  const { currentStep, loading, providers, agents } = useBootstrapStatus();
  const { statuses: oauthStatuses } = useChatGPTOAuthProviderStatuses(providers);
  const [step, setStep] = useState<1 | 2 | 3 | 4>(1);
  const [createdProvider, setCreatedProvider] = useState<ProviderData | null>(null);
  const [selectedModel, setSelectedModel] = useState<string | null>(null);
  const [createdAgent, setCreatedAgent] = useState<AgentData | null>(null);
  const [initialized, setInitialized] = useState(false);
  const [showComplete, setShowComplete] = useState(false);

  // Initialize step from server state (only on first load, not on refetches)
  useEffect(() => {
    if (loading || initialized) return;
    if (currentStep === ("complete" as SetupStep)) {
      navigate(ROUTES.OVERVIEW, { replace: true });
      return;
    }
    setStep(currentStep as 1 | 2 | 3 | 4);
    setInitialized(true);
  }, [currentStep, loading, initialized, navigate]);

  if (loading || !initialized) {
    return <SetupLayout><PageLoader /></SetupLayout>;
  }

  const completedSteps: number[] = [];
  if (step > 1) completedSteps.push(1);
  if (step > 2) completedSteps.push(2);
  if (step > 3) completedSteps.push(3);
  if (showComplete) { completedSteps.push(1, 2, 3, 4); }

  // For resuming: find existing provider/agent from server data
  const readyOAuthProviders = new Set(
    oauthStatuses
      .filter((status) => status.availability === "ready")
      .map((status) => status.provider.name),
  );
  const activeProvider = createdProvider ?? providers.find((provider) => provider.enabled && (
    provider.api_key === "***"
    || provider.provider_type === "claude_cli"
    || provider.provider_type === "ollama"
    || (provider.provider_type === "chatgpt_oauth" && readyOAuthProviders.has(provider.name))
  )) ?? null;
  const activeAgent = createdAgent ?? agents[0] ?? null;

  const handleFinish = () => setShowComplete(true);

  return (
    <SetupLayout>
      <SetupStepper currentStep={step} completedSteps={completedSteps} />

      {step === 1 && (
        <StepProvider
          existingProvider={createdProvider}
          onComplete={(provider) => {
            setCreatedProvider(provider);
            setStep(2);
          }}
        />
      )}

      {step === 2 && activeProvider && (
        <StepModel
          provider={activeProvider}
          initialModel={selectedModel}
          onBack={() => setStep(1)}
          onComplete={(model) => {
            setSelectedModel(model);
            setStep(3);
          }}
        />
      )}

      {step === 3 && activeProvider && (
        <StepAgent
          provider={activeProvider}
          model={selectedModel}
          existingAgent={createdAgent}
          onBack={() => setStep(2)}
          onComplete={(agent) => {
            setCreatedAgent(agent);
            setStep(4);
          }}
        />
      )}

      {step === 4 && (
        <StepChannel
          agent={activeAgent}
          onBack={() => setStep(3)}
          onComplete={handleFinish}
          onSkip={handleFinish}
        />
      )}

      {/* Footer: skip link + language selector */}
      {!showComplete && (
        <div className="mt-6 flex flex-col items-center gap-3">
          <button
            type="button"
            className="text-sm text-muted-foreground underline underline-offset-4 hover:text-foreground transition-colors"
            onClick={() => {
              if (window.confirm(t("skipSetupConfirm"))) {
                markSetupSkipped({ userId, tenantId: currentTenantId, tenantSlug: currentTenantSlug });
                navigate(ROUTES.OVERVIEW, { replace: true });
              }
            }}
          >
            {t("skipSetup")}
          </button>
          <div className="flex items-center gap-4">
            <TenantSwitcher />
            <LanguageSelector />
          </div>
        </div>
      )}

      <SetupCompleteModal
        open={showComplete}
        onGoToDashboard={() => navigate(ROUTES.OVERVIEW, { replace: true })}
      />
    </SetupLayout>
  );
}
