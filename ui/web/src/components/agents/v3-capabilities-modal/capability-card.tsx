import type { LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

interface CapabilityCardProps {
  icon: LucideIcon;
  title: string;
  description: string;
  className?: string;
}

export function CapabilityCard({
  icon: Icon,
  title,
  description,
  className,
}: CapabilityCardProps) {
  return (
    <div className={cn("rounded-lg border p-3 space-y-1.5", className)}>
      <div className="flex items-center gap-2">
        <Icon className="h-4 w-4 shrink-0 text-blue-500" />
        <span className="text-sm font-medium">{title}</span>
      </div>
      <p className="text-xs text-muted-foreground leading-relaxed">
        {description}
      </p>
    </div>
  );
}
