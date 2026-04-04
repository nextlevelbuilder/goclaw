import { useTranslation } from "react-i18next";
import { Check, Plus } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { ChatGPTOAuthAvailability } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import type { ChatGPTOAuthProviderQuota } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-quotas";
import type { ProviderData } from "@/types/provider";
import type { CodexPoolEntry } from "../codex-pool-entry-types";
import {
  routeBadgeVariant,
  routeLabelKey,
} from "../agent-display-utils";
import {
  getQuotaFailureKind,
  type ChatGPTOAuthRouteReadiness,
  type ChatGPTOAuthQuotaFailureKind,
} from "../chatgpt-oauth-quota-utils";
import { StateGroup } from "./chatgpt-oauth-state-group";

function statusBadgeVariant(
  availability: ChatGPTOAuthAvailability,
): "success" | "warning" | "outline" {
  if (availability === "ready") return "success";
  if (availability === "needs_sign_in") return "warning";
  return "outline";
}

function roleBadgeClass(role: "preferred" | "extra"): string {
  if (role === "preferred") {
    return "border-primary/35 bg-primary/12 text-foreground shadow-sm dark:border-primary/40 dark:bg-primary/18";
  }
  return "border-border/70 bg-background/80 text-muted-foreground";
}

export type SelectedEntry = CodexPoolEntry & {
  routeReadiness: ChatGPTOAuthRouteReadiness;
  failureKind: ChatGPTOAuthQuotaFailureKind | null;
};

interface MembershipSectionProps {
  membershipEditable: boolean;
  membershipManagedByLabel?: string;
  currentProvider: string;
  selectedEntries: SelectedEntry[];
  selectableExtraProviders: ProviderData[];
  selectedExtras: Set<string>;
  quotaByName?: Map<string, ChatGPTOAuthProviderQuota>;
  canEditMembership: boolean;
  mode: "inherit" | "custom";
  isLoading: boolean;
  onToggleProvider: (name: string) => void;
}

export function MembershipSection({
  membershipEditable,
  membershipManagedByLabel,
  currentProvider,
  selectedEntries,
  selectableExtraProviders,
  selectedExtras,
  quotaByName,
  canEditMembership,
  mode,
  isLoading,
  onToggleProvider,
}: MembershipSectionProps) {
  const { t } = useTranslation("agents");

  return (
    <section className="space-y-2.5 [@media(max-height:760px)]:space-y-2">
      <div className="flex flex-wrap items-center gap-1.5">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {membershipEditable
            ? t("chatgptOAuthRouting.availableExtraAccountsLabel")
            : t("chatgptOAuthRouting.poolMembershipLabel")}
        </p>
      </div>

      {!membershipEditable ? (
        <div className="rounded-lg border border-dashed px-3 py-3 text-sm text-muted-foreground">
          {selectedEntries.length > 1
            ? t("chatgptOAuthRouting.membershipManagedAtProvider", {
                provider: membershipManagedByLabel || currentProvider,
              })
            : t("chatgptOAuthRouting.membershipConfigureProviderFirst", {
                provider: membershipManagedByLabel || currentProvider,
              })}
        </div>
      ) : isLoading ? (
        <div className="rounded-lg border border-dashed px-3 py-3 text-sm text-muted-foreground">
          {t("chatgptOAuthRouting.loadingAccounts")}
        </div>
      ) : selectableExtraProviders.length > 0 ? (
        <div className="grid gap-2 xl:grid-cols-2">
          {selectableExtraProviders.map((provider) => {
            const selected = selectedExtras.has(provider.name);
            const failureKind = getQuotaFailureKind(quotaByName?.get(provider.name));
            return (
              <button
                key={provider.name}
                type="button"
                className={cn(
                  "group relative flex h-10 w-full cursor-pointer items-center gap-2.5 rounded-lg border px-3 text-left text-sm transition-all duration-200 xl:h-11 [@media(max-height:760px)]:h-9",
                  "disabled:pointer-events-none disabled:opacity-50",
                  selected &&
                    "border-primary/50 bg-primary/12 text-foreground shadow-sm dark:border-primary/40 dark:bg-primary/8",
                  selected &&
                    failureKind &&
                    "border-amber-500/50 bg-amber-500/8 text-amber-900 dark:border-amber-500/40 dark:text-amber-200",
                  !selected &&
                    !failureKind &&
                    "border-dashed border-primary/30 bg-primary/5 text-foreground hover:border-solid hover:border-primary/60 hover:bg-primary/10 hover:shadow-sm active:scale-[0.98] dark:border-primary/25 dark:bg-primary/4 dark:hover:border-primary/50 dark:hover:bg-primary/8",
                  !selected &&
                    failureKind &&
                    "border-dashed border-amber-500/30 bg-amber-500/5 text-foreground hover:border-solid hover:border-amber-500/60 hover:bg-amber-500/10 active:scale-[0.98] dark:border-amber-500/25 dark:bg-amber-500/4",
                )}
                onClick={() => onToggleProvider(provider.name)}
                disabled={!canEditMembership || mode === "inherit"}
              >
                <span className={cn(
                  "flex h-6 w-6 shrink-0 items-center justify-center rounded-md transition-all duration-200",
                  selected
                    ? "bg-primary/25 text-primary dark:bg-primary/20"
                    : "bg-primary/10 text-primary/70 group-hover:bg-primary/20 group-hover:text-primary dark:bg-primary/8 dark:text-primary/60 dark:group-hover:bg-primary/15",
                )}>
                  {selected ? <Check className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
                </span>
                <span className="min-w-0 truncate font-medium">{provider.display_name || provider.name}</span>
                {!selected && (
                  <span className="ml-auto text-xs text-muted-foreground transition-colors group-hover:text-primary/80">
                    {t("chatgptOAuthRouting.clickToAdd")}
                  </span>
                )}
              </button>
            );
          })}
        </div>
      ) : (
        <div className="rounded-lg border border-dashed px-3 py-3 text-sm text-muted-foreground">
          {t("chatgptOAuthRouting.noReadyExtras")}
        </div>
      )}
    </section>
  );
}

interface SelectedAccountsSectionProps {
  selectedEntries: SelectedEntry[];
}

export function SelectedAccountsSection({ selectedEntries }: SelectedAccountsSectionProps) {
  const { t } = useTranslation("agents");

  return (
    <section className="space-y-3 [@media(max-height:760px)]:space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-1.5">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {t("chatgptOAuthRouting.selectedAccountsLabel")}
        </p>
        <Badge variant="outline" className="h-6 px-2 text-[11px]">
          {t("chatgptOAuthRouting.selectedCount", { count: selectedEntries.length })}
        </Badge>
      </div>

      {selectedEntries.length > 0 ? (
        <div className="rounded-lg border bg-muted/10 p-3 [@media(max-height:760px)]:p-2.5">
          <div className="grid gap-1.5 xl:grid-cols-2 [@media(max-height:760px)]:gap-1">
            {selectedEntries.map((entry) => (
              <div
                key={entry.name}
                className="rounded-lg border bg-background/80 px-2.5 py-2 [@media(max-height:760px)]:px-2 [@media(max-height:760px)]:py-1.5"
              >
                <div className="flex flex-wrap items-center gap-2">
                  <span className="min-w-0 truncate text-sm font-medium">
                    {entry.label}
                  </span>
                  <Badge
                    variant="outline"
                    className={cn(
                      "h-5 px-1.5 text-[10px] xl:h-6 xl:px-2 xl:text-xs",
                      roleBadgeClass(entry.role),
                    )}
                  >
                    {t(`chatgptOAuthRouting.role.${entry.role}`)}
                  </Badge>
                  {entry.availability !== "ready" && (
                    <Badge
                      variant={statusBadgeVariant(entry.availability)}
                      className="h-5 px-1.5 text-[10px] xl:h-6 xl:px-2 xl:text-xs"
                    >
                      {t(`chatgptOAuthRouting.status.${entry.availability}`)}
                    </Badge>
                  )}
                  {entry.routeReadiness !== "healthy" && (
                    <Badge
                      variant={routeBadgeVariant(entry.routeReadiness)}
                      className="h-5 px-1.5 text-[10px] xl:h-6 xl:px-2 xl:text-xs"
                    >
                      {t(routeLabelKey(entry.routeReadiness))}
                    </Badge>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      ) : (
        <div className="rounded-lg border border-dashed px-3 py-3 text-sm text-muted-foreground">
          {t("chatgptOAuthRouting.emptySelected")}
        </div>
      )}
    </section>
  );
}

interface PoolStateSectionProps {
  routerActiveEntries: SelectedEntry[];
  standbyEntries: SelectedEntry[];
  blockedEntries: SelectedEntry[];
  routeDetail: (entry: SelectedEntry) => string | undefined;
}

export function PoolStateSection({
  routerActiveEntries,
  standbyEntries,
  blockedEntries,
  routeDetail,
}: PoolStateSectionProps) {
  const { t } = useTranslation("agents");

  return (
    <section className="space-y-2.5 [@media(max-height:760px)]:space-y-2">
      <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {t("chatgptOAuthRouting.poolStateTitle")}
      </p>
      <div className="grid items-start gap-1.5 xl:grid-cols-3 [@media(max-height:760px)]:gap-1">
        <StateGroup
          title={t("chatgptOAuthRouting.routerActiveTitle")}
          count={routerActiveEntries.length}
          variant="success"
          entries={routerActiveEntries.map((e) => ({
            name: e.name,
            label: e.label,
            detail: routeDetail(e),
          }))}
          emptyLabel={t("chatgptOAuthRouting.emptyGroup")}
        />
        <StateGroup
          title={t("chatgptOAuthRouting.fallbackTitle")}
          count={standbyEntries.length}
          variant="warning"
          entries={standbyEntries.map((e) => ({
            name: e.name,
            label: e.label,
            detail: routeDetail(e),
          }))}
          emptyLabel={t("chatgptOAuthRouting.emptyGroup")}
        />
        <StateGroup
          title={t("chatgptOAuthRouting.blockedNowTitle")}
          count={blockedEntries.length}
          variant="destructive"
          entries={blockedEntries.map((e) => ({
            name: e.name,
            label: e.label,
            detail: routeDetail(e),
          }))}
          emptyLabel={t("chatgptOAuthRouting.emptyGroup")}
        />
      </div>
    </section>
  );
}
