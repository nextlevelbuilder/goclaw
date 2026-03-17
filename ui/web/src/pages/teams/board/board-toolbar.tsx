import { memo } from "react";
import { Button } from "@/components/ui/button";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { RefreshCw, Plus, LayoutGrid, List, FolderOpen } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useBoardStore } from "../stores/use-board-store";
import type { GroupBy } from "../stores/use-board-store";
import type { ScopeEntry } from "@/types/team";

type StatusFilter = "all" | "pending" | "in_progress" | "completed";

interface BoardToolbarProps {
  statusFilter: StatusFilter;
  onStatusFilter: (f: StatusFilter) => void;
  scopes: ScopeEntry[];
  selectedScope: ScopeEntry | null;
  onScopeChange: (s: ScopeEntry | null) => void;
  spinning: boolean;
  onRefresh: () => void;
  onCreateTask: () => void;
  onWorkspace?: () => void;
}

const STATUS_FILTERS: { value: StatusFilter; labelKey: string }[] = [
  { value: "all", labelKey: "tasks.filters.all" },
  { value: "pending", labelKey: "tasks.filters.pending" },
  { value: "in_progress", labelKey: "tasks.filters.inProgress" },
  { value: "completed", labelKey: "tasks.filters.completed" },
];

const GROUP_OPTIONS: { value: GroupBy; labelKey: string }[] = [
  { value: "status", labelKey: "board.groupByStatus" },
  { value: "owner", labelKey: "board.groupByOwner" },
];

export const BoardToolbar = memo(function BoardToolbar({
  statusFilter, onStatusFilter,
  scopes, selectedScope, onScopeChange,
  spinning, onRefresh, onCreateTask, onWorkspace,
}: BoardToolbarProps) {
  const { t } = useTranslation("teams");
  const { viewMode, setViewMode, groupBy, setGroupBy } = useBoardStore();
  const filters = STATUS_FILTERS;

  return (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <div className="flex flex-wrap items-center gap-2">
        {/* Status filter */}
        <div className="flex rounded-lg border bg-muted/50 p-0.5">
          {filters.map((f) => (
            <button
              key={f.value}
              onClick={() => onStatusFilter(f.value)}
              className={
                "cursor-pointer rounded-md px-2.5 py-1 text-xs font-medium transition-colors " +
                (statusFilter === f.value
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground")
              }
            >
              {t(f.labelKey)}
            </button>
          ))}
        </div>

        {/* Scope filter */}
        {scopes.length > 0 && (
          <Select
            value={selectedScope?.chat_id ?? "__all__"}
            onValueChange={(v) => {
              if (v === "__all__") onScopeChange(null);
              else onScopeChange({ channel: "", chat_id: v });
            }}
          >
            <SelectTrigger className="h-8 w-40 text-xs">
              <SelectValue placeholder={t("scope.all")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">{t("scope.all")}</SelectItem>
              {scopes.map((s) => (
                <SelectItem key={s.chat_id} value={s.chat_id}>
                  {s.chat_id.length > 16 ? s.chat_id.slice(0, 16) + "\u2026" : s.chat_id}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}

        {/* Group by toggle (only in kanban view) */}
        {viewMode === "kanban" && (
          <div className="flex rounded-lg border bg-muted/50 p-0.5">
            {GROUP_OPTIONS.map((g) => (
              <button
                key={g.value}
                onClick={() => setGroupBy(g.value)}
                className={
                  "cursor-pointer rounded-md px-2.5 py-1 text-xs font-medium transition-colors " +
                  (groupBy === g.value
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground")
                }
              >
                {t(g.labelKey)}
              </button>
            ))}
          </div>
        )}
      </div>

      <div className="flex items-center gap-2">
        {/* Workspace */}
        {onWorkspace && (
          <Button variant="outline" size="sm" onClick={onWorkspace} className="gap-1.5 h-8 text-xs">
            <FolderOpen className="h-3.5 w-3.5" />
            {t("workspace.title")}
          </Button>
        )}

        {/* View toggle */}
        <div className="flex rounded-lg border bg-muted/50 p-0.5">
          <button
            onClick={() => setViewMode("kanban")}
            className={"cursor-pointer rounded-md p-1.5 " + (viewMode === "kanban" ? "bg-background shadow-sm" : "text-muted-foreground")}
          >
            <LayoutGrid className="h-4 w-4" />
          </button>
          <button
            onClick={() => setViewMode("list")}
            className={"cursor-pointer rounded-md p-1.5 " + (viewMode === "list" ? "bg-background shadow-sm" : "text-muted-foreground")}
          >
            <List className="h-4 w-4" />
          </button>
        </div>

        <Button variant="outline" size="icon" className="h-8 w-8" onClick={onCreateTask}>
          <Plus className="h-4 w-4" />
        </Button>
        <Button variant="outline" size="sm" onClick={onRefresh} disabled={spinning} className="gap-1">
          <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} />
        </Button>
      </div>
    </div>
  );
});
