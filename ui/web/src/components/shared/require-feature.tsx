import { Navigate } from "react-router";
import { useFeaturesStore, type FeatureName } from "@/stores/use-features-store";
import { ROUTES } from "@/lib/constants";

interface RequireFeatureProps {
  feature: FeatureName;
  children: React.ReactNode;
}

export function RequireFeature({ feature, children }: RequireFeatureProps) {
  const isEnabled = useFeaturesStore((s) => s.isFeatureEnabled)(feature);

  if (!isEnabled) {
    return <Navigate to={ROUTES.OVERVIEW} replace />;
  }

  return <>{children}</>;
}
