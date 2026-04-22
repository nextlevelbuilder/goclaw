import { useCallback } from "react";
import { useQuery, useQueryClient, useMutation } from "@tanstack/react-query";
import i18next from "i18next";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { toast } from "@/stores/use-toast-store";

export interface HookConfig {
  id: string;
  tenant_id: string;
  agent_id?: string | null;
  agent_ids?: string[];
  name?: string;
  event: string;
  handler_type: "command" | "http" | "prompt" | "script";
  scope: "global" | "tenant" | "agent";
  config: Record<string, unknown>;
  matcher?: string;
  if_expr?: string;
  timeout_ms: number;
  on_timeout: "block" | "allow";
  priority: number;
  enabled: boolean;
  version: number;
  source: "ui" | "api" | "seed" | "builtin";
  metadata: Record<string, unknown>;
  created_by?: string;
  created_at: string;
  updated_at: string;
}

export interface HookTestResult {
  decision: "allow" | "block" | "error" | "timeout";
  reason?: string;
  durationMs: number;
  stdout?: string;
  stderr?: string;
  statusCode?: number;
  error?: string;
  updatedInput?: Record<string, unknown>;
}

export interface HookExecution {
  id: string;
  hook_id: string;
  session_key?: string;
  decision: string;
  duration_ms: number;
  error?: string;
  created_at: string;
}

const HOOKS_QUERY_KEY = ["hooks"] as const;
const hookDetailKey = (id: string) => ["hooks", id] as const;
const hookHistoryKey = (id: string) => ["hooks", id, "history"] as const;

export function useHooksList(filters?: {
  event?: string;
  scope?: string;
  agentId?: string;
  enabled?: boolean;
}) {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);

  return useQuery({
    queryKey: [...HOOKS_QUERY_KEY, filters ?? {}],
    queryFn: async () => {
      const res = await ws.call<{ hooks: HookConfig[] }>("hooks.list", filters ?? {});
      return res.hooks ?? [];
    },
    staleTime: 30_000,
    enabled: connected,
  });
}

export function useHook(id: string | undefined) {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);

  return useQuery({
    queryKey: hookDetailKey(id ?? ""),
    queryFn: async () => {
      const res = await ws.call<{ hooks: HookConfig[] }>("hooks.list", {});
      return res.hooks?.find((h) => h.id === id) ?? null;
    },
    staleTime: 30_000,
    enabled: connected && !!id,
  });
}

export function useHookHistory(hookId: string | undefined) {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);

  return useQuery({
    queryKey: hookHistoryKey(hookId ?? ""),
    queryFn: async () => {
      const res = await ws.call<{ executions: HookExecution[]; nextCursor: string; note?: string }>(
        "hooks.history",
        { hookId },
      );
      return res;
    },
    staleTime: 60_000,
    enabled: connected && !!hookId,
  });
}

function useInvalidateHooks() {
  const queryClient = useQueryClient();
  return useCallback(
    () => queryClient.invalidateQueries({ queryKey: HOOKS_QUERY_KEY }),
    [queryClient],
  );
}

export function useCreateHook() {
  const ws = useWs();
  const invalidate = useInvalidateHooks();

  return useMutation({
    mutationFn: (params: Partial<HookConfig>) =>
      ws.call<{ hookId: string }>("hooks.create", params),
    onSuccess: async () => {
      await invalidate();
      toast.success(i18next.t("hooks:toast.created"));
    },
    onError: (err) => {
      toast.error(
        i18next.t("hooks:toast.failedCreate"),
        err instanceof Error ? err.message : "",
      );
    },
  });
}

export function useUpdateHook() {
  const ws = useWs();
  const invalidate = useInvalidateHooks();

  return useMutation({
    mutationFn: ({ hookId, updates }: { hookId: string; updates: Record<string, unknown> }) =>
      ws.call<{ hookId: string }>("hooks.update", { hookId, updates }),
    onSuccess: async () => {
      await invalidate();
      toast.success(i18next.t("hooks:toast.updated"));
    },
    onError: (err) => {
      toast.error(
        i18next.t("hooks:toast.failedUpdate"),
        err instanceof Error ? err.message : "",
      );
    },
  });
}

export function useDeleteHook() {
  const ws = useWs();
  const invalidate = useInvalidateHooks();

  return useMutation({
    mutationFn: (hookId: string) => ws.call<{ hookId: string }>("hooks.delete", { hookId }),
    onSuccess: async () => {
      await invalidate();
      toast.success(i18next.t("hooks:toast.deleted"));
    },
    onError: (err) => {
      toast.error(
        i18next.t("hooks:toast.failedDelete"),
        err instanceof Error ? err.message : "",
      );
    },
  });
}

export function useToggleHook() {
  const ws = useWs();
  const invalidate = useInvalidateHooks();

  return useMutation({
    mutationFn: ({ hookId, enabled }: { hookId: string; enabled: boolean }) =>
      ws.call<{ hookId: string; enabled: boolean }>("hooks.toggle", { hookId, enabled }),
    onSuccess: async () => {
      await invalidate();
      toast.success(i18next.t("hooks:toast.toggled"));
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : String(err));
    },
  });
}

export function useTestHook() {
  const ws = useWs();

  return useMutation({
    mutationFn: ({
      config,
      sampleEvent,
    }: {
      config: Partial<HookConfig>;
      sampleEvent: { toolName: string; toolInput: Record<string, unknown>; rawInput?: string };
    }) =>
      ws.call<{ result: HookTestResult }>("hooks.test", { config, sampleEvent }),
    onError: (err) => {
      toast.error(
        i18next.t("hooks:toast.failedTest"),
        err instanceof Error ? err.message : "",
      );
    },
  });
}
