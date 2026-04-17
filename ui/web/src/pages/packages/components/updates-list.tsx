import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowRight, Loader2 } from "lucide-react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { UpdateInfo } from "../hooks/use-updates";
import { SourcePill } from "./source-pill";
import { UpdateRowButton } from "./update-row-button";

const KNOWN_SOURCES = ["github", "pip", "npm"] as const;
type KnownSource = (typeof KNOWN_SOURCES)[number];

interface Props {
  updates: UpdateInfo[];
  availability?: Record<string, boolean>;
  loading?: boolean;
  isMaster: boolean;
  onUpdate: (pkg: string) => Promise<void> | void;
  onUpdateAll?: () => void;
}

/**
 * Unified updates table across all package sources (github / pip / npm).
 * - Renders a source filter dropdown when multiple sources have updates.
 * - Delegates per-row update action to UpdateRowButton.
 * - Mobile-safe: overflow-x-auto + min-w-[600px] per CLAUDE.md rules.
 */
export function UpdatesList({
  updates,
  availability,
  loading,
  isMaster,
  onUpdate,
}: Props) {
  const { t } = useTranslation("packages");
  const [sourceFilter, setSourceFilter] = useState<"all" | KnownSource>("all");

  // Sources not explicitly disabled (missing key → visible)
  const visibleSources = KNOWN_SOURCES.filter((s) => availability?.[s] !== false);

  // Only show filter when more than 1 source is visible
  const showFilter = visibleSources.length > 1 || sourceFilter !== "all";

  const filteredUpdates =
    sourceFilter === "all"
      ? updates
      : updates.filter((u) => u.source === sourceFilter);

  if (!loading && updates.length === 0) return null;

  return (
    <section className="space-y-2">
      {/* Filter row */}
      {showFilter && (
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">{t("updates.filter.label")}:</span>
          <Select
            value={sourceFilter}
            onValueChange={(v) => setSourceFilter(v as "all" | KnownSource)}
          >
            <SelectTrigger size="sm" className="w-36 text-base md:text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t("updates.filter.all")}</SelectItem>
              {visibleSources.map((src) => (
                <SelectItem key={src} value={src}>
                  {t(`updates.source.${src}`, { defaultValue: src })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )}

      {/* Updates table */}
      <div className="overflow-x-auto">
        <table className="min-w-[600px] w-full text-sm">
          <thead>
            <tr className="border-b">
              <th className="text-left py-2 px-3 font-medium text-muted-foreground w-20">
                {t("updates.filter.label")}
              </th>
              <th className="text-left py-2 px-3 font-medium text-muted-foreground">
                {t("table.name")}
              </th>
              <th className="text-left py-2 px-3 font-medium text-muted-foreground">
                {t("table.version")}
              </th>
              <th className="text-right py-2 px-3 font-medium text-muted-foreground">
                {t("table.actions")}
              </th>
            </tr>
          </thead>
          <tbody>
            {loading && filteredUpdates.length === 0 ? (
              <tr>
                <td colSpan={4} className="py-8 text-center text-muted-foreground">
                  <Loader2 className="h-5 w-5 animate-spin mx-auto" />
                </td>
              </tr>
            ) : filteredUpdates.length === 0 ? (
              <tr>
                <td colSpan={4} className="py-6 text-center text-muted-foreground text-sm">
                  {t("updates.empty")}
                </td>
              </tr>
            ) : (
              filteredUpdates.map((upd) => (
                <tr
                  key={`${upd.source}:${upd.name}`}
                  className="border-b last:border-0 hover:bg-muted/50 transition-colors"
                >
                  <td className="py-2 px-3">
                    <SourcePill source={upd.source} />
                  </td>
                  <td className="py-2 px-3 font-mono">{upd.name}</td>
                  <td className="py-2 px-3">
                    <span className="font-mono text-xs text-muted-foreground">
                      {upd.currentVersion}
                    </span>
                    <ArrowRight className="inline mx-1 w-3 h-3 text-muted-foreground" />
                    <span className="font-mono text-xs font-medium">
                      {upd.latestVersion}
                    </span>
                  </td>
                  <td className="py-2 px-3 text-right">
                    <UpdateRowButton
                      update={upd}
                      isMaster={isMaster}
                      onUpdate={onUpdate}
                      source={upd.source}
                    />
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}
