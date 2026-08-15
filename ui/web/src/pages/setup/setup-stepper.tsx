import { Check } from "lucide-react";
import { useTranslation } from "react-i18next";

interface SetupStepperProps {
  currentStep: 1 | 2 | 3 | 4;
  completedSteps: number[];
}

export function SetupStepper({ currentStep, completedSteps }: SetupStepperProps) {
  const { t } = useTranslation("setup");

  const STEPS: { num: number; label: string; sublabel?: string }[] = [
    { num: 1, label: t("steps.provider") },
    { num: 2, label: t("steps.model") },
    { num: 3, label: t("steps.agent") },
    { num: 4, label: t("steps.channel"), sublabel: t("steps.channelOptional") },
  ];

  return (
    <div className="flex items-center justify-center gap-0">
      {STEPS.map((step, i) => {
        const isCompleted = completedSteps.includes(step.num);
        const isCurrent = step.num === currentStep;

        return (
          <div key={step.num} className="flex items-center">
            {/* Step circle + label */}
            <div className="flex flex-col items-center gap-1.5">
              <div
                className={`flex h-9 w-9 items-center justify-center rounded-full text-sm font-medium transition-colors ${
                  isCompleted
                    ? "bg-primary text-primary-foreground"
                    : isCurrent
                      ? "border-2 border-primary bg-background text-primary"
                      : "border border-muted-foreground/30 bg-muted text-muted-foreground"
                }`}
              >
                {isCompleted ? <Check className="h-4 w-4" /> : step.num}
              </div>
              <div className="h-8 text-center">
                <span className={`text-xs font-medium ${isCurrent || isCompleted ? "text-foreground" : "text-muted-foreground"}`}>
                  {step.label}
                </span>
                {step.sublabel && (
                  <span className="block text-2xs text-muted-foreground">{step.sublabel}</span>
                )}
              </div>
            </div>

            {/* Connector line */}
            {i < STEPS.length - 1 && (
              <div
                className={`mx-3 mb-9 h-0.5 w-10 sm:w-16 ${
                  completedSteps.includes(step.num) ? "bg-primary" : "bg-muted-foreground/20"
                }`}
              />
            )}
          </div>
        );
      })}
    </div>
  );
}
