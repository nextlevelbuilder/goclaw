import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useBitrixPortals } from "./use-bitrix-portals";

// Sentinel values used to detect special items inside the Select
// onValueChange callback — Radix Select doesn't support per-item onClick, so
// we hijack the value and route it before setting state.
const CREATE_SENTINEL = "__bitrix_portal_create__";
const RESUME_PREFIX = "__bitrix_portal_resume__:";

interface BitrixPortalSelectProps {
  value: string;
  onChange: (portalName: string) => void;
  /** Called when user picks "+ Create new portal" or "Connect first portal".
   *  Parent component owns the modal lifecycle and re-selects the created
   *  portal once the install completes. */
  onCreateRequest: () => void;
  /** Called when user clicks a pending (not-yet-installed) portal. Parent
   *  opens the create modal directly at the authorize step so the admin can
   *  resume the OAuth flow without losing the portal row. */
  onResumeAuthorize?: (portalName: string) => void;
}

// BitrixPortalSelect replaces the free-text Portal input on the Bitrix24
// channel form. Loads the portal list from bitrix.portals.list, disables
// portals whose install hasn't completed (admin must authorize first), and
// surfaces a "+ Create new portal" affordance at the bottom of the dropdown.
//
// Special-cased in channel-fields.tsx::FieldRenderer for key="portal" +
// channelType="bitrix24". Not generalised to FieldDef because we don't have
// a second use case yet.
export function BitrixPortalSelect({ value, onChange, onCreateRequest, onResumeAuthorize }: BitrixPortalSelectProps) {
  const { t } = useTranslation("channels");
  const { data: portals = [], isLoading, isError } = useBitrixPortals();

  if (isLoading) {
    return <Skeleton className="h-9 w-full" />;
  }

  if (isError) {
    return (
      <p className="text-xs text-destructive">
        {t("bitrix24.portalSelect.loadError", { defaultValue: "Failed to load portals. Refresh to retry." })}
      </p>
    );
  }

  // Empty state — guide the user straight to the create flow.
  if (portals.length === 0) {
    return (
      <div className="grid gap-2">
        <p className="text-xs text-muted-foreground">
          {t("bitrix24.portalSelect.empty", { defaultValue: "No Bitrix24 portals connected yet." })}
        </p>
        <Button type="button" variant="outline" size="sm" onClick={onCreateRequest}>
          + {t("bitrix24.portalSelect.createFirst", { defaultValue: "Connect your first Bitrix24 portal" })}
        </Button>
      </div>
    );
  }

  return (
    <Select
      value={value}
      onValueChange={(v) => {
        if (v === CREATE_SENTINEL) {
          onCreateRequest();
          return;
        }
        if (v.startsWith(RESUME_PREFIX)) {
          const portalName = v.slice(RESUME_PREFIX.length);
          onResumeAuthorize?.(portalName);
          return;
        }
        onChange(v);
      }}
    >
      <SelectTrigger>
        <SelectValue placeholder={t("bitrix24.portalSelect.placeholder", { defaultValue: "Select a portal..." })} />
      </SelectTrigger>
      <SelectContent>
        {portals.map((p) => {
          // Pending portals get their own sentinel so we can route the click
          // to onResumeAuthorize. Installed portals carry the bare name as
          // value (what the form actually stores).
          const itemValue = p.installed ? p.name : `${RESUME_PREFIX}${p.name}`;
          return (
            <SelectItem key={p.name} value={itemValue}>
              <div className="flex items-center gap-2">
                <span>{p.name}</span>
                <span className="text-xs text-muted-foreground">({p.domain})</span>
                {!p.installed && (
                  <span className="text-xs text-amber-600">
                    ⚠ {t("bitrix24.portalSelect.pendingBadge", { defaultValue: "Pending install — click to resume" })}
                  </span>
                )}
              </div>
            </SelectItem>
          );
        })}
        <SelectSeparator />
        <SelectItem value={CREATE_SENTINEL} className="text-primary">
          + {t("bitrix24.portalSelect.createNew", { defaultValue: "Create new portal" })}
        </SelectItem>
      </SelectContent>
    </Select>
  );
}
