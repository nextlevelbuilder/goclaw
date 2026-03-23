import { Navigate, useLocation } from "react-router";
import { useAuthStore } from "@/stores/use-auth-store";
import { ROUTES } from "@/lib/constants";

export function RequireAuth({ children }: { children: React.ReactNode }) {
  const token = useAuthStore((s) => s.token);
  const userId = useAuthStore((s) => s.userId);
  const senderID = useAuthStore((s) => s.senderID);
  const connected = useAuthStore((s) => s.connected);
  const tenantSelected = useAuthStore((s) => s.tenantSelected);
  const availableTenants = useAuthStore((s) => s.availableTenants);
  const isCrossTenant = useAuthStore((s) => s.isCrossTenant);
  const location = useLocation();

  // Not authenticated
  if ((!token && !senderID) || !userId) {
    return <Navigate to={ROUTES.LOGIN} state={{ from: location }} replace />;
  }

  // Connected but no tenant selected — show tenant selector
  // (only gate after WS is connected and tenants have loaded)
  if (connected && !tenantSelected && availableTenants.length > 0) {
    return <Navigate to={ROUTES.SELECT_TENANT} state={{ from: location }} replace />;
  }

  // Connected, no tenants, not cross-tenant — blocked
  if (connected && !tenantSelected && availableTenants.length === 0 && !isCrossTenant) {
    return <Navigate to={ROUTES.SELECT_TENANT} replace />;
  }

  return <>{children}</>;
}
