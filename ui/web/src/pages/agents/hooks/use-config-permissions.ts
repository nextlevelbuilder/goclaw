import { useState, useCallback } from "react";
import { toast } from "@/stores/use-toast-store";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";

export interface ConfigPermission {
  id: string;
  agentId: string;
  scope: string;
  configType: string;
  userId: string;
  permission: string; // "allow" | "deny"
  grantedBy?: string;
  metadata?: Record<string, string>; // {displayName, username}
  createdAt: string;
  updatedAt: string;
}

export function useConfigPermissions(agentId: string | undefined) {
  const ws = useWs();
  const [permissions, setPermissions] = useState<ConfigPermission[]>([]);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    if (!agentId) return;
    setLoading(true);
    try {
      const res = await ws.call<{ permissions: ConfigPermission[] }>(
        Methods.CONFIG_PERMISSIONS_LIST,
        { agentId },
      );
      setPermissions(res.permissions ?? []);
    } catch {
      // silent — permissions may not be available for all users
    } finally {
      setLoading(false);
    }
  }, [ws, agentId]);

  const grant = useCallback(
    async (scope: string, configType: string, userId: string, permission: string, metadata?: Record<string, string>) => {
      if (!agentId) return;
      try {
        await ws.call(Methods.CONFIG_PERMISSIONS_GRANT, {
          agentId, scope, configType, userId, permission, metadata,
        });
        toast.success("Permission granted");
        await load();
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Failed to grant permission");
      }
    },
    [ws, agentId, load],
  );

  const revoke = useCallback(
    async (scope: string, configType: string, userId: string) => {
      if (!agentId) return;
      try {
        await ws.call(Methods.CONFIG_PERMISSIONS_REVOKE, {
          agentId, scope, configType, userId,
        });
        toast.success("Permission revoked");
        await load();
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Failed to revoke permission");
      }
    },
    [ws, agentId, load],
  );

  return { permissions, loading, load, grant, revoke };
}
