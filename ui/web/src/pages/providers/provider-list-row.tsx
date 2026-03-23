import { Cpu, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { PROVIDER_TYPE_BADGE, ProviderApiKeyBadge } from "./provider-utils";
import type { ProviderData } from "./hooks/use-providers";

interface ProviderListRowProps {
  provider: ProviderData;
  onClick: () => void;
  onDelete?: () => void;
}

export function ProviderListRow({ provider, onClick, onDelete }: ProviderListRowProps) {
  const { t: tc } = useTranslation("common");
  const displayName = provider.display_name || provider.name;
  const tb = PROVIDER_TYPE_BADGE[provider.provider_type] ?? { label: provider.provider_type, variant: "outline" as const };

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onClick(); } }}
      className="flex w-full cursor-pointer items-center gap-3 rounded-lg border bg-card px-4 py-3 text-left transition-all hover:border-primary/30 hover:shadow-sm"
    >
      {/* Icon */}
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
        <Cpu className="h-4 w-4" />
      </div>

      {/* Name + slug */}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-semibold">{displayName}</span>
          <span className={`inline-block h-2 w-2 shrink-0 rounded-full ${provider.enabled ? "bg-emerald-500" : "bg-muted-foreground/40"}`} />
        </div>
        {provider.display_name && (
          <div className="truncate text-xs text-muted-foreground">{provider.name}</div>
        )}
      </div>

      {/* Type badge */}
      <div className="hidden shrink-0 sm:block">
        <Badge variant={tb.variant} className="text-[11px]">{tb.label}</Badge>
      </div>

      {/* API key status */}
      <div className="hidden shrink-0 md:block">
        <ProviderApiKeyBadge provider={provider} />
      </div>

      {/* Enabled */}
      <div className="hidden shrink-0 text-[11px] text-muted-foreground lg:block">
        {provider.enabled ? tc("enabled") : tc("disabled")}
      </div>

      {/* Delete */}
      {onDelete && (
        <Button
          variant="ghost"
          size="xs"
          className="shrink-0 text-muted-foreground hover:text-destructive"
          onClick={(e) => { e.stopPropagation(); onDelete(); }}
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      )}
    </div>
  );
}
