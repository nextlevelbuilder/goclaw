import { Navigate } from "react-router";
import { ROUTES } from "@/lib/constants";
import { useFeaturesStore } from "@/stores/use-features-store";
import { ROUTE_FEATURE_MAP } from "@/lib/feature-registry";

interface RequireFeatureProps {
  featureKey?: string;
  route?: string;
  children: React.ReactNode;
}

export function RequireFeature({ featureKey, route, children }: RequireFeatureProps) {
  const isFeatureEnabled = useFeaturesStore((s) => s.isFeatureEnabled);

  const key = featureKey ?? (route ? ROUTE_FEATURE_MAP[route] : undefined);

  if (key && !isFeatureEnabled(key)) {
    return <Navigate to={ROUTES.OVERVIEW} replace />;
  }

  return <>{children}</>;
}
