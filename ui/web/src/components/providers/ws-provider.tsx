import { useEffect, useRef, useMemo, useCallback } from "react";
import { WsClient, type ConnectionState } from "@/api/ws-client";
import { HttpClient } from "@/api/http-client";
import { WsContext } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { useUiStore } from "@/stores/use-ui-store";
import { useWsQueryInvalidation } from "@/hooks/use-query-invalidation";
import { useWsEvent } from "@/hooks/use-ws-event";
import { TEAM_RELATED_EVENTS } from "@/api/protocol";
import { useTeamEventStore } from "@/stores/use-team-event-store";
import { brand, updateBrand } from "@/lib/brand";
import { useFeaturesStore } from "@/stores/use-features-store";

// In dev mode, connect directly to backend WS (bypass Vite proxy).
// In production, use relative "/ws" path.
const WS_URL = import.meta.env.VITE_WS_URL || "/ws";

/** Refresh Keycloak access token 60s before it expires. */
const REFRESH_MARGIN_MS = 60_000;

export function WsProvider({ children }: { children: React.ReactNode }) {
  const token = useAuthStore((s) => s.token);
  const userId = useAuthStore((s) => s.userId);
  const senderID = useAuthStore((s) => s.senderID);
  const keycloakRefreshToken = useAuthStore((s) => s.keycloakRefreshToken);

  const wsRef = useRef<WsClient | null>(null);

  // Create WsClient once - survives StrictMode remounts
  if (!wsRef.current) {
    wsRef.current = new WsClient(
      WS_URL,
      () => useAuthStore.getState().token,
      () => useAuthStore.getState().userId,
      () => useAuthStore.getState().senderID,
      (state: ConnectionState) => {
        useAuthStore.getState().setConnected(state === "connected");
      },
    );
    wsRef.current.onAuthFailure = () => {
      useAuthStore.getState().logout();
    };
    wsRef.current.onLockedTheme = (theme) => {
      useUiStore.getState().setLockedTheme(theme);
    };
    wsRef.current.onDefaultLanguage = (language) => {
      useUiStore.getState().setLockedLanguage(language as import("@/lib/constants").Language | null);
    };
    wsRef.current.onDefaultTimezone = (timezone) => {
      useUiStore.getState().setLockedTimezone(timezone);
    };
    wsRef.current.onBrand = (b) => {
      updateBrand({
        appName: b.app_name,
        appKey: b.app_key,
        tagline: b.tagline,
        logoUrl: b.logo_url,
      });
      document.title = `${brand.appName} Dashboard`;
    };
    wsRef.current.onFeatures = (features, role) => {
      useFeaturesStore.getState().setFeatures(features);
      useFeaturesStore.getState().setRole(role);
    };
  }
  const ws = wsRef.current;

  const http = useMemo(() => {
    const client = new HttpClient(
      "",
      () => useAuthStore.getState().token,
      () => useAuthStore.getState().userId,
    );
    client.onAuthFailure = () => {
      useAuthStore.getState().logout();
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

  // Keycloak token auto-refresh: schedule refresh before token expires.
  useEffect(() => {
    if (!keycloakRefreshToken || !token) return;

    // Decode JWT to get expiration time
    const expiresAt = getJwtExpiration(token);
    if (!expiresAt) return;

    const msUntilExpiry = expiresAt - Date.now();
    const refreshIn = Math.max(msUntilExpiry - REFRESH_MARGIN_MS, 5_000);

    const timer = setTimeout(async () => {
      try {
        const { keycloakRefreshToken: rt } = useAuthStore.getState();
        if (!rt) return;
        const newTokens = await refreshKeycloakToken(rt);
        if (newTokens) {
          const { userId: uid, displayName: dn } = useAuthStore.getState();
          useAuthStore.getState().setKeycloakAuth(
            newTokens.accessToken, uid, dn, newTokens.refreshToken,
          );
          // Reconnect WS with new token
          ws.disconnect();
          ws.connect();
        }
      } catch {
        // Refresh failed — let normal auth failure handle logout
      }
    }, refreshIn);

    return () => clearTimeout(timer);
  }, [token, keycloakRefreshToken, ws]);

  const value = useMemo(() => ({ ws, http }), [ws, http]);

  return (
    <WsContext.Provider value={value}>
      <WsQueryInvalidation />
      <WsTeamEventCapture />
      {children}
    </WsContext.Provider>
  );
}

/** Extract expiration timestamp (ms) from a JWT without external dependencies. */
function getJwtExpiration(token: string): number | null {
  try {
    const parts = token.split(".");
    if (parts.length !== 3) return null;
    const payload = JSON.parse(atob(parts[1]!.replace(/-/g, "+").replace(/_/g, "/")));
    return typeof payload.exp === "number" ? payload.exp * 1000 : null;
  } catch {
    return null;
  }
}

/** Refresh Keycloak access token using the refresh token. */
async function refreshKeycloakToken(refreshToken: string): Promise<{ accessToken: string; refreshToken: string } | null> {
  const configRes = await fetch("/v1/auth/keycloak/config");
  if (!configRes.ok) return null;
  const cfg = await configRes.json();

  const tokenUrl = `${cfg.url}/realms/${cfg.realm}/protocol/openid-connect/token`;
  const res = await fetch(tokenUrl, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      grant_type: "refresh_token",
      client_id: cfg.client_id,
      refresh_token: refreshToken,
    }),
  });
  if (!res.ok) return null;

  const tokens = await res.json();
  return {
    accessToken: tokens.access_token,
    refreshToken: tokens.refresh_token || refreshToken,
  };
}

function WsQueryInvalidation() {
  useWsQueryInvalidation();
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
