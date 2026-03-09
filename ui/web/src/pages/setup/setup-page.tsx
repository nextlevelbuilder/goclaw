import { useState, useEffect } from "react";
import { useNavigate } from "react-router";
import { useBootstrapStatus, type SetupStep } from "./hooks/use-bootstrap-status";
import { SetupLayout } from "./setup-layout";
import { SetupStepper } from "./setup-stepper";
import { StepProvider } from "./step-provider";
import { StepModel } from "./step-model";
import { StepAgent } from "./step-agent";
import { StepChannel } from "./step-channel";
import { SetupCompleteModal } from "./setup-complete-modal";
import { ROUTES } from "@/lib/constants";
import type { ProviderData } from "@/types/provider";
import type { AgentData } from "@/types/agent";

function PageLoader() {
  return (
    <div className="flex h-32 items-center justify-center">
      <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
    </div>
  );
}

export function SetupPage() {
  const navigate = useNavigate();
  const { currentStep, loading, providers, agents } = useBootstrapStatus();
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
  const activeProvider = createdProvider ?? providers.find((p) => p.enabled &&
    (p.api_key === "***" || p.provider_type === "claude_cli" || p.provider_type === "chatgpt_oauth")) ?? null;
  const activeAgent = createdAgent ?? agents[0] ?? null;

  const handleFinish = () => setShowComplete(true);

  return (
    <SetupLayout>
      <SetupStepper currentStep={step} completedSteps={completedSteps} />

      {step === 1 && (
        <StepProvider
          onComplete={(provider) => {
            setCreatedProvider(provider);
            setStep(2);
          }}
        />
      )}

      {step === 2 && activeProvider && (
        <StepModel
          provider={activeProvider}
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
          onComplete={(agent) => {
            setCreatedAgent(agent);
            setStep(4);
          }}
        />
      )}

      {step === 4 && (
        <StepChannel
          agent={activeAgent}
          onComplete={handleFinish}
          onSkip={handleFinish}
        />
      )}

      <SetupCompleteModal
        open={showComplete}
        onGoToDashboard={() => navigate(ROUTES.OVERVIEW, { replace: true })}
      />
    </SetupLayout>
  );
}
