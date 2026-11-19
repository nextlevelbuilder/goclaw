export type SetupStep = 1 | 2 | 3 | 4 | "complete";

interface BootstrapFacts {
  canManageProviders: boolean;
  providersLoadFailed: boolean;
  skipped: boolean;
  hasProvider: boolean;
  hasAgent: boolean;
}

/**
 * Decides whether the setup wizard applies. Provider state only counts when it
 * is actually known: a role that cannot read providers, or a failed read, says
 * nothing about whether the gateway has any.
 */
export function resolveBootstrapState(facts: BootstrapFacts): { needsSetup: boolean; currentStep: SetupStep } {
  const { canManageProviders, providersLoadFailed, skipped, hasProvider, hasAgent } = facts;
  if (!canManageProviders || providersLoadFailed || skipped) {
    return { needsSetup: false, currentStep: "complete" };
  }
  if (!hasProvider) return { needsSetup: true, currentStep: 1 };
  if (!hasAgent) return { needsSetup: true, currentStep: 2 };
  return { needsSetup: false, currentStep: "complete" };
}
