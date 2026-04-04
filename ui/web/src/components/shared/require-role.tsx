import { Navigate } from "react-router";
import { useAuthStore } from "@/stores/use-auth-store";
import { ROUTES } from "@/lib/constants";

/** Check if role meets minimum level. Owner > Admin > Operator > Viewer. */
function hasMinRole(role: string, minRole: string): boolean {
  const levels: Record<string, number> = { owner: 4, admin: 3, operator: 2, viewer: 1 };
  return (levels[role] ?? 0) >= (levels[minRole] ?? 0);
}

/** Renders children only if user has admin role or higher. Redirects to overview otherwise. */
export function RequireAdmin({ children }: { children: React.ReactNode }) {
  const role = useAuthStore((s) => s.role);
  if (!hasMinRole(role, "admin")) {
    return <Navigate to={ROUTES.OVERVIEW} replace />;
  }
  return <>{children}</>;
}

/** Renders children only if user has admin or operator role or higher. */
export function RequireOperator({ children }: { children: React.ReactNode }) {
  const role = useAuthStore((s) => s.role);
  if (!hasMinRole(role, "operator")) {
    return <Navigate to={ROUTES.OVERVIEW} replace />;
  }
  return <>{children}</>;
}

/** Renders children only if user is the system owner. */
export function RequireOwner({ children }: { children: React.ReactNode }) {
  const isOwner = useAuthStore((s) => s.isOwner);
  if (!isOwner) {
    return <Navigate to={ROUTES.OVERVIEW} replace />;
  }
  return <>{children}</>;
}

/** @deprecated Use RequireOwner instead. */
export const RequireCrossTenant = RequireOwner;
