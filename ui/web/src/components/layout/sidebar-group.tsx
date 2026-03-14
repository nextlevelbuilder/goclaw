import { cn } from "@/lib/utils";
import { useFeaturesStore, type FeatureName } from "@/stores/use-features-store";

interface SidebarGroupProps {
  label: string;
  collapsed?: boolean;
  children: React.ReactNode;
  /** Feature keys for this group's children. Group hides when none are enabled. */
  features?: FeatureName[];
}

export function SidebarGroup({ label, collapsed, children, features }: SidebarGroupProps) {
  const fe = useFeaturesStore((s) => s.isFeatureEnabled);

  // Hide the entire group if a features list is provided and none are enabled
  if (features && features.length > 0 && !features.some((f) => fe(f))) {
    return null;
  }

  return (
    <div className="space-y-1">
      {!collapsed && (
        <p className="px-3 py-1 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          {label}
        </p>
      )}
      {collapsed && <div className="mx-auto my-1 h-px w-6 bg-border" />}
      <div className={cn("space-y-0.5", collapsed && "flex flex-col items-center")}>
        {children}
      </div>
    </div>
  );
}
