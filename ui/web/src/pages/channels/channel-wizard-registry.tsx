// Channel wizard step component registry.
// To add wizard steps for a new channel type:
// 1. Create a directory: channels/{channel-name}/
// 2. Implement components matching the interfaces below
// 3. Register them in the maps at the bottom of this file
// 4. Add WizardConfig entry in channel-schemas.ts

import type { ComponentType } from "react";
import type { ChannelInstanceData } from "./hooks/use-channel-instances";

// --- Standard wizard step interfaces ---

/** Auth step rendered inside the wizard dialog after instance creation */
export interface WizardAuthStepProps {
  instanceId: string;
  /** Called when authentication completes successfully */
  onComplete: () => void;
  /** Called when user clicks "Skip" to bypass auth */
  onSkip: () => void;
}

/** Config step rendered inside the wizard dialog after auth (or skip) */
export interface WizardConfigStepProps {
  instanceId: string;
  /** Whether the preceding auth step completed successfully */
  authCompleted: boolean;
  configValues: Record<string, unknown>;
  onConfigChange: (key: string, value: unknown) => void;
}

/** Inline config widget rendered inside the form during edit mode */
export interface WizardEditConfigProps {
  instance: ChannelInstanceData;
  configValues: Record<string, unknown>;
  onConfigChange: (key: string, value: unknown) => void;
}

/** Re-auth dialog rendered from the channels table */
export interface ReauthDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  instanceId: string;
  instanceName: string;
  onSuccess: () => void;
}

// --- Channel step imports (add new channels here) ---

import { ZaloAuthStep, ZaloConfigStep, ZaloEditConfig } from "./zalo/zalo-wizard-steps";
import { ZaloPersonalQRDialog } from "./zalo/zalo-personal-qr-dialog";
import { WhatsAppAuthStep } from "./whatsapp/whatsapp-wizard-steps";
import { WhatsAppReauthDialog } from "./whatsapp/whatsapp-reauth-dialog";

// --- Component registries ---

export const wizardAuthSteps: Record<string, ComponentType<WizardAuthStepProps>> = {
  zalo_personal: ZaloAuthStep,
  whatsapp: WhatsAppAuthStep,
};

export const wizardConfigSteps: Record<string, ComponentType<WizardConfigStepProps>> = {
  zalo_personal: ZaloConfigStep,
};

export const wizardEditConfigs: Record<string, ComponentType<WizardEditConfigProps>> = {
  zalo_personal: ZaloEditConfig,
};

/** Re-auth dialogs for re-authentication from the channels table */
export const reauthDialogs: Record<string, ComponentType<ReauthDialogProps>> = {
  zalo_personal: ZaloPersonalQRDialog,
  whatsapp: WhatsAppReauthDialog,
};

/** Set of channel types that support re-authentication from the table */
export const channelsWithAuth = new Set(Object.keys(reauthDialogs));
