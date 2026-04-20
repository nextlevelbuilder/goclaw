import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Radio } from "lucide-react";
import { useChannelInstances } from "@/pages/channels/hooks/use-channel-instances";

interface BoundChannelsSectionProps {
  agentId: string;
}

export function BoundChannelsSection({ agentId }: BoundChannelsSectionProps) {
  const { t } = useTranslation("agents");
  const s = "configSections.boundChannels";
  const { instances } = useChannelInstances();
  const bound = instances.filter((inst) => inst.agent_id === agentId);

  return (
    <section className="space-y-3">
      <div className="flex items-center gap-2">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-blue-100 dark:bg-blue-900/30">
          <Radio className="h-4 w-4 text-blue-600 dark:text-blue-400" />
        </div>
        <div>
          <h3 className="text-sm font-semibold">{t(`${s}.title`)}</h3>
          <p className="text-xs text-muted-foreground">{t(`${s}.description`)}</p>
        </div>
      </div>

      {bound.length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {bound.map((inst) => (
            <Badge
              key={inst.id}
              variant={inst.enabled ? "default" : "secondary"}
              className="gap-1"
            >
              {inst.display_name || inst.name}
              <span className="text-xs opacity-70">{inst.channel_type}</span>
              {!inst.enabled && (
                <span className="text-xs opacity-50">({t(`${s}.disabled`)})</span>
              )}
            </Badge>
          ))}
        </div>
      ) : (
        <p className="text-xs text-muted-foreground italic">{t(`${s}.empty`)}</p>
      )}
    </section>
  );
}
