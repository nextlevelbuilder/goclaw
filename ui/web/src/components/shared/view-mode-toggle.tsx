import { LayoutGrid, List } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

interface ViewModeToggleProps {
  mode: "card" | "list";
  onChange: (mode: "card" | "list") => void;
}

export function ViewModeToggle({ mode, onChange }: ViewModeToggleProps) {
  const { t } = useTranslation("common");

  return (
    <TooltipProvider>
      <div className="ml-auto flex items-center gap-0.5 rounded-md border p-0.5">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant={mode === "card" ? "default" : "ghost"}
              size="xs"
              className="h-7 w-7 p-0"
              onClick={() => onChange("card")}
            >
              <LayoutGrid className="h-3.5 w-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("viewCard")}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant={mode === "list" ? "default" : "ghost"}
              size="xs"
              className="h-7 w-7 p-0"
              onClick={() => onChange("list")}
            >
              <List className="h-3.5 w-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("viewList")}</TooltipContent>
        </Tooltip>
      </div>
    </TooltipProvider>
  );
}
