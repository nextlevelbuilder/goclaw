import { useState, useEffect } from "react";
import { Webhook, Plus, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { toast } from "@/stores/use-toast-store";
import {
  useHooksList, useDeleteHook, useToggleHook, useCreateHook, useUpdateHook,
  type HookConfig,
} from "@/hooks/use-hooks";
import { HookListRow } from "@/pages/hooks/components/hook-list-row";
import { HookFormDialog } from "@/pages/hooks/components/hook-form-dialog";
import { HookTestPanel } from "@/pages/hooks/components/hook-test-panel";
import type { HookFormData } from "@/schemas/hooks.schema";

interface AgentHooksTabProps {
  agentId: string;
  initialCreateOpen?: boolean;
  onCreateOpenChange?: (open: boolean) => void;
}

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
    return { source: data.script_source ?? "" };
  }
  return {
    prompt_template: data.prompt_template ?? "",
    model: data.model ?? "haiku",
    max_invocations_per_turn: data.max_invocations_per_turn ?? 5,
  };
}

export function AgentHooksTab({ agentId, initialCreateOpen, onCreateOpenChange }: AgentHooksTabProps) {
  const { t } = useTranslation("agents");
  const { t: th } = useTranslation("hooks");

  const [showCreate, setShowCreate] = useState(false);
  const [editTarget, setEditTarget] = useState<HookConfig | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<HookConfig | null>(null);
  const [testTarget, setTestTarget] = useState<HookConfig | null>(null);

  // Handle initial create open from parent (overview card "Add hook" button)
  useEffect(() => {
    if (initialCreateOpen) {
      setShowCreate(true);
      onCreateOpenChange?.(false);
    }
  }, [initialCreateOpen, onCreateOpenChange]);

  const { data: hooks = [], isPending: loading, isFetching: refreshing, refetch } = useHooksList({
    agentId,
    scope: "agent",
  });
  const spinning = useMinLoading(refreshing);
  const createMutation = useCreateHook();
  const updateMutation = useUpdateHook();
  const deleteMutation = useDeleteHook();
  const toggleMutation = useToggleHook();

  const handleCreate = async (data: HookFormData) => {
    let config: Record<string, unknown>;
    try {
      config = buildConfig(data);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
      return;
    }
    // Pre-populate agent_ids with current agent if scope is agent
    const agentIds = data.scope === "agent"
      ? Array.from(new Set([...(data.agent_ids ?? []), agentId]))
      : data.agent_ids ?? [];
    await createMutation.mutateAsync({
      name: data.name ?? "",
      agent_ids: agentIds,
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

  // Default values for create form with agent pre-selected
  const createInitial: Partial<HookConfig> = {
    agent_ids: [agentId],
    scope: "agent",
    event: "pre_tool_use",
    handler_type: "script",
    timeout_ms: 5000,
    on_timeout: "block",
    priority: 100,
    enabled: true,
  };

  if (loading && hooks.length === 0) {
    return <TableSkeleton />;
  }

  if (hooks.length === 0) {
    return (
      <>
        <EmptyState
          icon={Webhook}
          title={t("hooks.tab.emptyTitle")}
          description={t("hooks.tab.emptyDesc")}
          action={
            <Button size="sm" onClick={() => setShowCreate(true)} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> {t("hooks.tab.createFirst")}
            </Button>
          }
        />
        <HookFormDialog
          open={showCreate}
          onOpenChange={setShowCreate}
          onSubmit={handleCreate}
          initial={createInitial as HookConfig}
        />
      </>
    );
  }

  return (
    <>
      <div className="flex items-center justify-between gap-3 mb-4">
        <Button variant="outline" size="sm" onClick={() => refetch()} disabled={spinning} className="gap-1">
          <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} />
          {t("hooks.tab.refresh")}
        </Button>
        <Button size="sm" onClick={() => setShowCreate(true)} className="gap-1">
          <Plus className="h-3.5 w-3.5" /> {t("hooks.tab.create")}
        </Button>
      </div>

      <div className="flex flex-col gap-2">
        {hooks.map((hook) => (
          <HookListRow
            key={hook.id}
            hook={hook}
            onClick={() => setEditTarget(hook)}
            onToggle={(enabled) => toggleMutation.mutate({ hookId: hook.id, enabled })}
            onEdit={() => setEditTarget(hook)}
            onDelete={() => setDeleteTarget(hook)}
            onTest={() => setTestTarget(hook)}
          />
        ))}
      </div>

      <HookFormDialog
        open={showCreate}
        onOpenChange={setShowCreate}
        onSubmit={handleCreate}
        initial={createInitial as HookConfig}
      />

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
          title={th("actions.delete")}
          description={t("hooks.tab.deleteConfirm", { event: deleteTarget.event })}
          confirmLabel={th("actions.delete")}
          variant="destructive"
          onConfirm={async () => {
            await deleteMutation.mutateAsync(deleteTarget.id);
            setDeleteTarget(null);
          }}
        />
      )}

      {testTarget && (
        <Dialog open onOpenChange={(o) => { if (!o) setTestTarget(null); }}>
          <DialogContent className="max-h-[90vh] flex flex-col max-sm:inset-0 max-sm:rounded-none sm:max-w-5xl lg:max-w-6xl">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2 text-base">
                {th("test.title")}
                <span className="font-mono text-sm text-muted-foreground">{testTarget.event}</span>
              </DialogTitle>
            </DialogHeader>
            <div className="flex-1 overflow-y-auto -mx-4 px-4 sm:-mx-6 sm:px-6">
              <HookTestPanel hook={testTarget} />
            </div>
          </DialogContent>
        </Dialog>
      )}
    </>
  );
}
