import { useEffect, useRef, useMemo, useCallback } from "react";
import { WsClient, type ConnectionState } from "@/api/ws-client";
import { HttpClient } from "@/api/http-client";
import { WsContext } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { useWsQueryInvalidation } from "@/hooks/use-query-invalidation";
import { LOCAL_STORAGE_KEYS } from "@/lib/constants";
import { useWsEvent } from "@/hooks/use-ws-event";
import { TEAM_RELATED_EVENTS, Methods } from "@/api/protocol";
import { useTeamEventStore } from "@/stores/use-team-event-store";
import type { TenantMembership } from "@/types/tenant";

// In dev mode, connect directly to backend WS (bypass Vite proxy).
// In production, use relative "/ws" path.
const WS_URL = import.meta.env.VITE_WS_URL || "/ws";

export function WsProvider({ children }: { children: React.ReactNode }) {
  const token = useAuthStore((s) => s.token);
  const userId = useAuthStore((s) => s.userId);
  const senderID = useAuthStore((s) => s.senderID);

  const wsRef = useRef<WsClient | null>(null);

  // Create WsClient once - survives StrictMode remounts
  if (!wsRef.current) {
    wsRef.current = new WsClient(
      WS_URL,
      () => useAuthStore.getState().token,
      () => useAuthStore.getState().userId,
      () => useAuthStore.getState().senderID,
      (state: ConnectionState) => {
        const store = useAuthStore.getState();
        const isConnected = state === "connected";
        if (isConnected && wsRef.current) {
          store.setConnected(true, { version: wsRef.current.serverVersion });
        } else {
          store.setConnected(false);
        }
        if (isConnected && wsRef.current) {
          const client = wsRef.current;
          store.setRole(client.role || "");
          store.setTenant(client.tenantId, client.tenantName, client.tenantSlug, client.crossTenant);
          // Fetch tenant memberships asynchronously
          client.call<{ tenants: TenantMembership[] }>(Methods.TENANTS_MINE)
            .then((res) => {
              const tenants = res?.tenants ?? [];
              store.setAvailableTenants(tenants);

              // Auto-select tenant if applicable
              const savedScope = localStorage.getItem(LOCAL_STORAGE_KEYS.TENANT_ID);
              if (savedScope && tenants.some((t) => t.slug === savedScope)) {
                // Already scoped via localStorage — auto-select
                store.setTenantSelected(true);
              } else if (!client.crossTenant && tenants.length === 1) {
                // Non-admin with single tenant — auto-select
                // eslint-disable-next-line @typescript-eslint/no-non-null-assertion
                localStorage.setItem(LOCAL_STORAGE_KEYS.TENANT_ID, tenants[0]!.slug);
                store.setTenantSelected(true);
              } else if (!client.crossTenant && tenants.length === 0) {
                // No tenants — leave tenantSelected=false (blocked)
              } else if (client.crossTenant && !savedScope && tenants.length > 0) {
                // Cross-tenant admin without scope — auto-select first tenant
                // eslint-disable-next-line @typescript-eslint/no-non-null-assertion
                localStorage.setItem(LOCAL_STORAGE_KEYS.TENANT_ID, tenants[0]!.slug);
                window.location.reload();
                return;
              } else if (client.crossTenant && !savedScope) {
                // Cross-tenant admin, no tenants available — use server default (MasterTenantID)
                store.setTenantSelected(true);
              } else {
                store.setTenantSelected(true);
              }
            })
            .catch(() => {
              // Non-critical: silently ignore if not supported
            });
        }
        if (state === "disconnected") {
          store.setTenant("", "", "", false);
          store.setAvailableTenants([]);
          store.setTenantSelected(false);
        }
      },
    );
    wsRef.current.onAuthFailure = () => {
      // Don't logout if authenticated via browser pairing (no token)
      const state = useAuthStore.getState();
      if (state.senderID && !state.token) return;
      state.logout();
    };
  }
  const ws = wsRef.current;

  const http = useMemo(() => {
    const client = new HttpClient(
      "",
      () => useAuthStore.getState().token,
      () => useAuthStore.getState().userId,
      () => useAuthStore.getState().senderID,
    );
    client.onAuthFailure = () => {
      // Don't logout if authenticated via browser pairing (no token)
      const state = useAuthStore.getState();
      if (state.senderID && !state.token) return;
      state.logout();
    };
    return client;
  }, []);

  // Auto-connect when credentials are available (token or sender_id), disconnect when not.
  useEffect(() => {
    if ((token || senderID) && userId) {
      ws.connect();
    } else {
      ws.disconnect();
    }
  }, [token, userId, senderID, ws]);

  const value = useMemo(() => ({ ws, http }), [ws, http]);

  return (
    <WsContext.Provider value={value}>
      <WsQueryInvalidation />
      <WsTeamEventCapture />
      <WsTenantRevocationListener />
      {children}
    </WsContext.Provider>
  );
}

function WsQueryInvalidation() {
  useWsQueryInvalidation();
  return null;
}

/** Force logout when admin revokes user's tenant access. */
function WsTenantRevocationListener() {
  const handler = useCallback((raw: unknown) => {
    const { payload } = raw as { event: string; payload: { user_id?: string } };
    const state = useAuthStore.getState();
    if (payload?.user_id && payload.user_id === state.userId) {
      state.logout();
    }
  }, []);
  useWsEvent("tenant.access.revoked", handler);
  return null;
}

/** Captures all team-related WS events into the Zustand store. */
function WsTeamEventCapture() {
  const addEvent = useTeamEventStore((s) => s.addEvent);

  const handler = useCallback(
    (raw: unknown) => {
      const { event, payload } = raw as { event: string; payload: unknown };
      if (!TEAM_RELATED_EVENTS.has(event)) return;
      // Skip noisy chunk/thinking subtypes for agent events
      if (event === "agent") {
        const p = payload as { type?: string };
        if (p.type === "chunk" || p.type === "thinking") return;
      }
      addEvent(event, payload);
    },
    [addEvent],
  );

  useWsEvent("*", handler);
  return null;
}
