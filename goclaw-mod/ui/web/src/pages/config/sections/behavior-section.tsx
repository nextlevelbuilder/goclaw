import { useState, useEffect } from "react";
import { Save } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { BehaviorUxCard } from "./behavior-ux-card";
import { BehaviorRateCard } from "./behavior-rate-card";
import { BehaviorSecurityCard } from "./behavior-security-card";
import { BehaviorPendingCompactionCard, type PendingCompactionValues } from "./behavior-pending-compaction-card";
import { BehaviorChatCard, type ChatBehaviorValues } from "./behavior-chat-card";
import { BehaviorPassiveMemoryCard } from "./behavior-passive-memory-card";

interface Props {
  config: Record<string, any>;
  onPatch: (updates: Record<string, unknown>) => Promise<void>;
  saving: boolean;
}

/** State container for Behavior tab cards and their patch payload. */
export function BehaviorSection({ config, onPatch, saving }: Props) {
  const { t } = useTranslation("config");
  const gw = config.gateway ?? {};
  const ag = config.agents?.defaults ?? {};
  const tl = config.tools ?? {};
  const ch = config.channels ?? {};

  // UX toggles (from gateway + agents.defaults)
  const [ux, setUx] = useState({
    intent_classify: ag.intent_classify !== false,
    team_work_classify: gw.team_work_classify === true,
  });

  // Rate limiting (from gateway)
  const [rate, setRate] = useState<{ max_message_chars?: number; rate_limit_rpm?: number; inbound_debounce_ms?: number }>({
    max_message_chars: gw.max_message_chars,
    rate_limit_rpm: gw.rate_limit_rpm,
    inbound_debounce_ms: gw.inbound_debounce_ms,
  });

  // Security (from gateway + tools)
  const [security, setSecurity] = useState<{ injection_action?: string; scrub_credentials?: boolean }>({
    injection_action: gw.injection_action,
    scrub_credentials: tl.scrub_credentials,
  });

  // Pending compaction (from channels.pending_compaction)
  const [pendingCompaction, setPendingCompaction] = useState<PendingCompactionValues>(
    ch.pending_compaction ?? {},
  );
  const [chatBehavior, setChatBehavior] = useState<ChatBehaviorValues>(normalizeChatBehavior(gw.chat_behavior));

  const [dirty, setDirty] = useState(false);

  // Reset drafts when external config changes
  useEffect(() => {
    setUx({
      intent_classify: ag.intent_classify !== false,
      team_work_classify: gw.team_work_classify === true,
    });
    setRate({
      max_message_chars: gw.max_message_chars,
      rate_limit_rpm: gw.rate_limit_rpm,
      inbound_debounce_ms: gw.inbound_debounce_ms,
    });
    setSecurity({
      injection_action: gw.injection_action,
      scrub_credentials: tl.scrub_credentials,
    });
    setPendingCompaction(ch.pending_compaction ?? {});
    setChatBehavior(normalizeChatBehavior(gw.chat_behavior));
    setDirty(false);
  }, [config]);  

  const markDirty = <T,>(setter: React.Dispatch<React.SetStateAction<T>>) =>
    (v: T) => { setter(v); setDirty(true); };

  const handleSave = () => {
    onPatch(buildBehaviorPatch({
      rate,
      security,
      chatBehavior,
      ux,
      pendingCompaction,
    })).catch(() => {});
  };

  return (
    <div className="space-y-4">
      <BehaviorUxCard value={ux} onChange={markDirty(setUx)} />
      <BehaviorChatCard value={chatBehavior} onChange={markDirty(setChatBehavior)} />
      <BehaviorRateCard value={rate} onChange={markDirty(setRate)} />
      <BehaviorSecurityCard value={security} onChange={markDirty(setSecurity)} />
      <BehaviorPendingCompactionCard value={pendingCompaction} onChange={markDirty(setPendingCompaction)} />
      <BehaviorPassiveMemoryCard />

      {dirty && (
        <div className="flex justify-end pt-2">
          <Button size="sm" onClick={handleSave} disabled={saving} className="gap-1.5">
            <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("save")}
          </Button>
        </div>
      )}
    </div>
  );
}

export interface BehaviorPatchInput {
  rate: { max_message_chars?: number; rate_limit_rpm?: number; inbound_debounce_ms?: number };
  security: { injection_action?: string; scrub_credentials?: boolean };
  chatBehavior: ChatBehaviorValues;
  ux: { intent_classify: boolean; team_work_classify: boolean };
  pendingCompaction: PendingCompactionValues;
}

export function buildBehaviorPatch({
  rate,
  security,
  chatBehavior,
  ux,
  pendingCompaction,
}: BehaviorPatchInput): Record<string, unknown> {
  return {
    gateway: {
      max_message_chars: rate.max_message_chars,
      rate_limit_rpm: rate.rate_limit_rpm,
      inbound_debounce_ms: rate.inbound_debounce_ms,
      injection_action: security.injection_action,
      chat_behavior: chatBehavior,
      team_work_classify: ux.team_work_classify,
    },
    agents: {
      defaults: { intent_classify: ux.intent_classify },
    },
    tools: { scrub_credentials: security.scrub_credentials },
    channels: { pending_compaction: pendingCompaction },
  };
}

function normalizeChatBehavior(value: any): ChatBehaviorValues {
  return {
    enabled: value?.enabled ?? false,
    quick_ack: {
      enabled: value?.quick_ack?.enabled ?? true,
      mode: value?.quick_ack?.mode ?? "sidecar_generated",
      min_delay_ms: value?.quick_ack?.min_delay_ms ?? 1000,
      provider: value?.quick_ack?.provider ?? "",
      model: value?.quick_ack?.model ?? "",
      timeout_ms: value?.quick_ack?.timeout_ms ?? 2500,
      max_tokens: value?.quick_ack?.max_tokens ?? 40,
      max_chars: value?.quick_ack?.max_chars ?? 120,
      templates: value?.quick_ack?.templates ?? ["Got it. Working on it..."],
    },
    intermediate_replies: {
      enabled: value?.intermediate_replies?.enabled ?? false,
      mode: value?.intermediate_replies?.mode ?? "sidecar_generated",
      provider: value?.intermediate_replies?.provider ?? "",
      model: value?.intermediate_replies?.model ?? "",
      timeout_ms: value?.intermediate_replies?.timeout_ms ?? 2500,
      max_tokens: value?.intermediate_replies?.max_tokens ?? 60,
      max_chars: value?.intermediate_replies?.max_chars ?? 180,
    },
    final_split: {
      enabled: value?.final_split?.enabled ?? true,
      min_chars: value?.final_split?.min_chars ?? 1200,
      max_messages: value?.final_split?.max_messages ?? 3,
      delay_ms: value?.final_split?.delay_ms ?? 500,
    },
  };
}
