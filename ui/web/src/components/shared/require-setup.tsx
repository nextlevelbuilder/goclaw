import { Navigate } from "react-router";
import { useBootstrapStatus } from "@/pages/setup/hooks/use-bootstrap-status";
import { ROUTES } from "@/lib/constants";

function SetupLoader() {
  return (
    <div className="flex h-dvh items-center justify-center">
      <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
    </div>
  );
}

export function RequireSetup({ children }: { children: React.ReactNode }) {
  const { needsSetup, loading } = useBootstrapStatus();

  if (loading) return <SetupLoader />;
  if (needsSetup) return <Navigate to={ROUTES.SETUP} replace />;

  return <>{children}</>;
}
