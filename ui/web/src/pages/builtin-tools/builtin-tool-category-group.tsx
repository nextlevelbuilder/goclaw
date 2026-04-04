import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import type { BuiltinToolData } from "./hooks/use-builtin-tools";
import { ToolRow } from "./builtin-tool-row";

interface CategoryGroupProps {
  category: string;
  tools: BuiltinToolData[];
  onToggle: (tool: BuiltinToolData) => void;
  onSettings: (tool: BuiltinToolData) => void;
  tenantId: string | null;
  onSetTenantConfig: (name: string, enabled: boolean) => Promise<void>;
  onDeleteTenantConfig: (name: string) => Promise<void>;
}

export function CategoryGroup({
  category,
  tools,
  onToggle,
  onSettings,
  tenantId,
  onSetTenantConfig,
  onDeleteTenantConfig,
}: CategoryGroupProps) {
  const { t } = useTranslation("tools");
  return (
    <div className="rounded-lg border">
      <div className="flex items-center gap-2 border-b bg-muted/40 px-4 py-2">
        <span className="text-sm font-medium">{t(`builtin.categories.${category}`, category)}</span>
        <Badge variant="secondary" className="h-5 px-1.5 text-[11px]">
          {tools.length}
        </Badge>
      </div>
      <div className="divide-y">
        {tools.map((tool) => (
          <ToolRow
            key={tool.name}
            tool={tool}
            onToggle={onToggle}
            onSettings={onSettings}
            tenantId={tenantId}
            onSetTenantConfig={onSetTenantConfig}
            onDeleteTenantConfig={onDeleteTenantConfig}
          />
        ))}
      </div>
    </div>
  );
}
