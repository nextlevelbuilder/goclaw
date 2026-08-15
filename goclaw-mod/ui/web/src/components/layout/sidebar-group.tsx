import { cn } from "@/lib/utils";

interface SidebarGroupProps {
  label: string;
  collapsed?: boolean;
  children: React.ReactNode;
}

export function SidebarGroup({ label, collapsed, children }: SidebarGroupProps) {
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
