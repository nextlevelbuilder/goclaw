import type { LucideIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

interface V3FeatureCardProps {
  icon: LucideIcon;
  iconColor: string;
  title: string;
  stat: string;
  comparison: string;
  description: string;
}

export function V3FeatureCard({ icon: Icon, iconColor, title, stat, comparison, description }: V3FeatureCardProps) {
  return (
    <div className="flex gap-3 rounded-lg border p-3">
      <Icon className={cn("h-5 w-5 shrink-0 mt-0.5", iconColor)} />
      <div className="min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <p className="text-sm font-medium">{title}</p>
          <Badge variant="outline" className="text-2xs px-1.5 py-0">{stat}</Badge>
        </div>
        <p className="text-xs text-orange-600/80 dark:text-orange-400/80 mt-0.5">{comparison}</p>
        <p className="text-xs text-muted-foreground mt-1">{description}</p>
      </div>
    </div>
  );
}
