import { useState, useEffect } from "react";
import { Save } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { BehaviorUxCard } from "./behavior-ux-card";
import { BehaviorRateCard } from "./behavior-rate-card";
import { BehaviorSessionsCard } from "./behavior-sessions-card";
import { BehaviorSecurityCard } from "./behavior-security-card";
import { BehaviorPendingCompactionCard, type PendingCompactionValues } from "./behavior-pending-compaction-card";

 

interface Props {
  config: Record<string, any>;
  onPatch: (updates: Record<string, unknown>) => Promise<void>;
  saving: boolean;
}

/** State container for Behavior tab — composes 4 sub-cards, patches multiple config keys. */
export function BehaviorSection({ config, onPatch, saving }: Props) {
  const { t } = useTranslation("config");
  const gw = config.gateway ?? {};
  const ag = config.agents?.defaults ?? {};
  const tl = config.tools ?? {};
  const ss = config.sessions ?? {};
  const ch = config.channels ?? {};

  // UX toggles (from gateway + agents.defaults)
  const [ux, setUx] = useState({
    tool_status: gw.tool_status !== false,
    block_reply: gw.block_reply ?? false,
    intent_classify: ag.intent_classify !== false,
  });

  // Rate limiting (from gateway)
  const [rate, setRate] = useState<{ max_message_chars?: number; rate_limit_rpm?: number; inbound_debounce_ms?: number }>({
    max_message_chars: gw.max_message_chars,
    rate_limit_rpm: gw.rate_limit_rpm,
    inbound_debounce_ms: gw.inbound_debounce_ms,
  });

  // Sessions
  const [sessions, setSessions] = useState<{ scope?: string; dm_scope?: string }>({
    scope: ss.scope,
    dm_scope: ss.dm_scope,
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

  const [dirty, setDirty] = useState(false);

  // Reset drafts when external config changes
  useEffect(() => {
    setUx({
      tool_status: gw.tool_status !== false,
      block_reply: gw.block_reply ?? false,
      intent_classify: ag.intent_classify !== false,
    });
    setRate({
      max_message_chars: gw.max_message_chars,
      rate_limit_rpm: gw.rate_limit_rpm,
      inbound_debounce_ms: gw.inbound_debounce_ms,
    });
    setSessions({ scope: ss.scope, dm_scope: ss.dm_scope });
    setSecurity({
      injection_action: gw.injection_action,
      scrub_credentials: tl.scrub_credentials,
    });
    setPendingCompaction(ch.pending_compaction ?? {});
    setDirty(false);
  }, [config]);  

  const markDirty = <T,>(setter: React.Dispatch<React.SetStateAction<T>>) =>
    (v: T) => { setter(v); setDirty(true); };

  const handleSave = () => {
    // Strip masked/secret values to avoid overwriting real secrets with "***"
    const gwClean = Object.fromEntries(
      Object.entries(gw).filter(([, v]) => typeof v !== "string" || v !== "***"),
    );
    onPatch({
      gateway: {
        ...gwClean,
        tool_status: ux.tool_status,
        block_reply: ux.block_reply,
        max_message_chars: rate.max_message_chars,
        rate_limit_rpm: rate.rate_limit_rpm,
        inbound_debounce_ms: rate.inbound_debounce_ms,
        injection_action: security.injection_action,
      },
      agents: {
        ...config.agents,
        defaults: { ...ag, intent_classify: ux.intent_classify },
      },
      tools: { ...tl, scrub_credentials: security.scrub_credentials },
      sessions: { ...ss, ...sessions },
      channels: { ...ch, pending_compaction: pendingCompaction },
    });
  };

  return (
    <div className="space-y-4">
      <BehaviorUxCard value={ux} onChange={markDirty(setUx)} />
      <BehaviorRateCard value={rate} onChange={markDirty(setRate)} />
      <BehaviorSessionsCard value={sessions} onChange={markDirty(setSessions)} />
      <BehaviorSecurityCard value={security} onChange={markDirty(setSecurity)} />
      <BehaviorPendingCompactionCard value={pendingCompaction} onChange={markDirty(setPendingCompaction)} />

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
