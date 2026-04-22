import { useState, useMemo } from "react";
import { useParams, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { Webhook, Plus, RefreshCw, ArrowLeft, Pencil, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useMinLoading } from "@/hooks/use-min-loading";
import { toast } from "@/stores/use-toast-store";
import {
  useHooksList, useDeleteHook, useToggleHook, useCreateHook, useUpdateHook,
  type HookConfig,
} from "@/hooks/use-hooks";
import { HookListRow } from "./components/hook-list-row";
import { HookFormDialog } from "./components/hook-form-dialog";
import { HookTestPanel } from "./components/hook-test-panel";
import { HookOverviewTab } from "./components/hook-overview-tab";
import { HookHistoryTable } from "./components/hook-history-table";
import { BetaInfoCard } from "./components/beta-info-card";
import type { HookFormData } from "@/schemas/hooks.schema";

const HOOK_EVENTS = [
  "session_start", "user_prompt_submit", "pre_tool_use",
  "post_tool_use", "stop", "subagent_start", "subagent_stop",
] as const;

// parseHeaders accepts an empty string, an empty object string, or a JSON
// object. Returns {} for empty/whitespace-only input. Throws a typed Error
// with a friendly message on malformed JSON so the caller can surface via toast.
function parseHeaders(raw: string | undefined): Record<string, unknown> {
  const trimmed = (raw ?? "").trim();
  if (!trimmed) return {};
  try {
    const parsed = JSON.parse(trimmed);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>;
    }
    throw new Error("headers must be a JSON object");
  } catch (err) {
    // eslint-disable-next-line preserve-caught-error -- JSON.parse error message already captured verbatim in thrown message
    throw new Error(
      "Invalid headers JSON: " + (err instanceof Error ? err.message : String(err)),
    );
  }
}

function buildConfig(data: HookFormData): Record<string, unknown> {
  if (data.handler_type === "http") {
    return {
      url: data.url ?? "",
      method: data.method ?? "POST",
      headers: parseHeaders(data.headers),
      body_template: data.body_template ?? "",
    };
  }
  if (data.handler_type === "script") {
    // Backend goja handler reads cfg.Config.source (Phase 03). Zod caps at 32 KiB.
    return { source: data.script_source ?? "" };
  }
  // prompt
  return {
    prompt_template: data.prompt_template ?? "",
    model: data.model ?? "haiku",
    max_invocations_per_turn: data.max_invocations_per_turn ?? 5,
  };
}

export function HooksPage() {
  // Route params — single source of truth (CLAUDE.md)
  const { id: detailId } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { t } = useTranslation("hooks");
  const { t: tc } = useTranslation("common");

  // Filters
  const [search, setSearch] = useState("");
  const [filterEvent, setFilterEvent] = useState<string>("all");
  const [filterScope, setFilterScope] = useState<string>("all");

  // Dialog state
  const [showCreate, setShowCreate] = useState(false);
  const [editTarget, setEditTarget] = useState<HookConfig | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<HookConfig | null>(null);
  const [testTarget, setTestTarget] = useState<HookConfig | null>(null);

  // Data
  const { data: hooks = [], isPending: loading, isFetching: refreshing, refetch } = useHooksList();
  // Keep the spinner visible briefly so quick reloads produce a visible animation
  // matching the pattern used by the cron page (useMinLoading).
  const spinning = useMinLoading(refreshing);
  const createMutation = useCreateHook();
  const updateMutation = useUpdateHook();
  const deleteMutation = useDeleteHook();
  const toggleMutation = useToggleHook();

  const filtered = useMemo(() => {
    return hooks.filter((h) => {
      if (filterEvent !== "all" && h.event !== filterEvent) return false;
      if (filterScope !== "all" && h.scope !== filterScope) return false;
      if (search) {
        const q = search.toLowerCase();
        if (!h.event.includes(q) && !h.handler_type.includes(q) && !(h.matcher ?? "").includes(q)) return false;
      }
      return true;
    });
  }, [hooks, filterEvent, filterScope, search]);

  const handleCreate = async (data: HookFormData) => {
    let config: Record<string, unknown>;
    try {
      config = buildConfig(data);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
      return;
    }
    await createMutation.mutateAsync({
      name: data.name ?? "",
      agent_ids: data.agent_ids ?? [],
      event: data.event,
      handler_type: data.handler_type,
      scope: data.scope,
      matcher: data.matcher || undefined,
      if_expr: data.if_expr || undefined,
      timeout_ms: data.timeout_ms,
      on_timeout: data.on_timeout,
      priority: data.priority,
      enabled: data.enabled,
      config,
    });
  };

  const handleUpdate = async (data: HookFormData) => {
    if (!editTarget) return;
    let config: Record<string, unknown>;
    try {
      config = buildConfig(data);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
      return;
    }
    await updateMutation.mutateAsync({
      hookId: editTarget.id,
      updates: {
        name: data.name ?? "",
        agent_ids: data.agent_ids ?? [],
        event: data.event,
        handler_type: data.handler_type,
        scope: data.scope,
        matcher: data.matcher || undefined,
        if_expr: data.if_expr || undefined,
        timeout_ms: data.timeout_ms,
        on_timeout: data.on_timeout,
        priority: data.priority,
        enabled: data.enabled,
        config,
      },
    });
    setEditTarget(null);
  };

  // Detail view — find hook by ID from params
  const detailHook = detailId ? hooks.find((h) => h.id === detailId) : null;

  if (detailId && !loading) {
    if (!detailHook) {
      return (
        <div className="p-6">
          <Button variant="ghost" size="sm" onClick={() => navigate("/hooks")} className="gap-1 mb-4">
            <ArrowLeft className="h-3.5 w-3.5" /> Back
          </Button>
          <p className="text-sm text-muted-foreground">Hook not found.</p>
        </div>
      );
    }

    return (
      <div className="p-4 sm:p-6 pb-10">
        <div className="mb-4 flex items-center gap-3">
          <Button variant="ghost" size="sm" onClick={() => navigate("/hooks")} className="gap-1">
            <ArrowLeft className="h-3.5 w-3.5" /> {tc("back") ?? "Back"}
          </Button>
          <span className="font-mono text-sm text-muted-foreground">{detailHook.event}</span>
          <div className="flex-1" />
          <Button
            variant="outline" size="sm"
            onClick={() => setEditTarget(detailHook)}
            className="gap-1"
          >
            <Pencil className="h-3.5 w-3.5" /> {t("actions.edit")}
          </Button>
          <Button
            variant="outline" size="sm"
            className="gap-1 text-destructive hover:text-destructive"
            onClick={() => setDeleteTarget(detailHook)}
          >
            <Trash2 className="h-3.5 w-3.5" /> {t("actions.delete")}
          </Button>
        </div>

        <Tabs defaultValue="overview">
          <TabsList className="mb-4">
            <TabsTrigger value="overview">{t("tabs.overview")}</TabsTrigger>
            <TabsTrigger value="test">{t("tabs.test")}</TabsTrigger>
            <TabsTrigger value="history">{t("tabs.history")}</TabsTrigger>
          </TabsList>
          <TabsContent value="overview">
            <HookOverviewTab hook={detailHook} />
          </TabsContent>
          <TabsContent value="test">
            <HookTestPanel hook={detailHook} />
          </TabsContent>
          <TabsContent value="history">
            <HookHistoryTable hookId={detailHook.id} />
          </TabsContent>
        </Tabs>

        {editTarget && (
          <HookFormDialog
            open
            onOpenChange={(o) => { if (!o) setEditTarget(null); }}
            onSubmit={handleUpdate}
            initial={editTarget}
          />
        )}
        {deleteTarget && (
          <ConfirmDialog
            open
            onOpenChange={() => setDeleteTarget(null)}
            title={t("actions.delete")}
            description={`Delete hook for "${deleteTarget.event}"? This cannot be undone.`}
            confirmLabel={t("actions.delete")}
            variant="destructive"
            onConfirm={async () => {
              await deleteMutation.mutateAsync(deleteTarget.id);
              setDeleteTarget(null);
              navigate("/hooks");
            }}
          />
        )}
      </div>
    );
  }

  // List view
  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("subtitle")}
        actions={
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => refetch()} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} />
              {tc("refresh") ?? "Refresh"}
            </Button>
            <Button size="sm" onClick={() => setShowCreate(true)} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> {t("actions.create")}
            </Button>
          </div>
        }
      />

      <div className="mt-4">
        <BetaInfoCard />
      </div>

      {/* Filters */}
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <SearchInput value={search} onChange={setSearch} placeholder={t("filters.event")} className="max-w-xs" />
        <Select value={filterEvent} onValueChange={setFilterEvent}>
          <SelectTrigger className="w-44 text-base md:text-sm">
            <SelectValue placeholder={t("filters.event")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("filters.all")}</SelectItem>
            {HOOK_EVENTS.map((e) => <SelectItem key={e} value={e}>{e}</SelectItem>)}
          </SelectContent>
        </Select>
        <Select value={filterScope} onValueChange={setFilterScope}>
          <SelectTrigger className="w-36 text-base md:text-sm">
            <SelectValue placeholder={t("filters.scope")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("filters.all")}</SelectItem>
            {["global", "tenant", "agent"].map((s) => <SelectItem key={s} value={s}>{s}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>

      <div className="mt-6">
        {loading && hooks.length === 0 ? (
          <TableSkeleton />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Webhook}
            title={t("empty")}
            description=""
            action={
              <Button size="sm" onClick={() => setShowCreate(true)} className="gap-1">
                <Plus className="h-3.5 w-3.5" /> {t("actions.create")}
              </Button>
            }
          />
        ) : (
          <div className="flex flex-col gap-2">
            {filtered.map((hook) => (
              <HookListRow
                key={hook.id}
                hook={hook}
                onClick={() => navigate(`/hooks/${hook.id}`)}
                onToggle={(enabled) => toggleMutation.mutate({ hookId: hook.id, enabled })}
                onEdit={() => setEditTarget(hook)}
                onDelete={() => setDeleteTarget(hook)}
                onTest={() => setTestTarget(hook)}
              />
            ))}
          </div>
        )}
      </div>

      <HookFormDialog
        open={showCreate}
        onOpenChange={setShowCreate}
        onSubmit={handleCreate}
        initial={null}
      />

      {editTarget && !detailId && (
        <HookFormDialog
          open
          onOpenChange={(o) => { if (!o) setEditTarget(null); }}
          onSubmit={handleUpdate}
          initial={editTarget}
        />
      )}

      {deleteTarget && (
        <ConfirmDialog
          open
          onOpenChange={() => setDeleteTarget(null)}
          title={t("actions.delete")}
          description={`Delete hook for "${deleteTarget.event}"? This cannot be undone.`}
          confirmLabel={t("actions.delete")}
          variant="destructive"
          onConfirm={async () => {
            await deleteMutation.mutateAsync(deleteTarget.id);
            setDeleteTarget(null);
          }}
        />
      )}

      {/* Test dialog — centered modal with 2-col input/result layout. */}
      {testTarget && (
        <Dialog open onOpenChange={(o) => { if (!o) setTestTarget(null); }}>
          <DialogContent className="max-h-[90vh] flex flex-col max-sm:inset-0 max-sm:rounded-none sm:max-w-5xl lg:max-w-6xl">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2 text-base">
                {t("test.title")}
                <span className="font-mono text-sm text-muted-foreground">{testTarget.event}</span>
              </DialogTitle>
            </DialogHeader>
            <div className="flex-1 overflow-y-auto -mx-4 px-4 sm:-mx-6 sm:px-6">
              <HookTestPanel hook={testTarget} />
            </div>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
