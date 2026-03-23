import { Navigate } from "react-router";
import { useAuthStore } from "@/stores/use-auth-store";
import { ROUTES } from "@/lib/constants";

/** Renders children only if user has admin role. Redirects to overview otherwise. */
export function RequireAdmin({ children }: { children: React.ReactNode }) {
  const role = useAuthStore((s) => s.role);
  if (role !== "admin") {
    return <Navigate to={ROUTES.OVERVIEW} replace />;
  }
  return <>{children}</>;
}

/** Renders children only if user has admin or operator role. */
export function RequireOperator({ children }: { children: React.ReactNode }) {
  const role = useAuthStore((s) => s.role);
  if (role !== "admin" && role !== "operator") {
    return <Navigate to={ROUTES.OVERVIEW} replace />;
  }
  return <>{children}</>;
}

/** Renders children only if user has cross-tenant access (system owner). */
export function RequireCrossTenant({ children }: { children: React.ReactNode }) {
  const isCrossTenant = useAuthStore((s) => s.isCrossTenant);
  if (!isCrossTenant) {
    return <Navigate to={ROUTES.OVERVIEW} replace />;
  }
  return <>{children}</>;
}
