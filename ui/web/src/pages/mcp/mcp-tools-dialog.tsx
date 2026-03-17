import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Loader2, Wrench, Search } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import type { MCPServerData, MCPToolInfo } from "./hooks/use-mcp";

interface MCPToolsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  server: MCPServerData;
  onLoadTools: (serverId: string) => Promise<MCPToolInfo[]>;
}

export function MCPToolsDialog({ open, onOpenChange, server, onLoadTools }: MCPToolsDialogProps) {
  const { t } = useTranslation("mcp");
  const [tools, setTools] = useState<MCPToolInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");

  useEffect(() => {
    if (!open) return;
    setLoading(true);
    setError("");
    setSearch("");
    onLoadTools(server.id)
      .then(setTools)
      .catch(() => setError(t("tools.failedLoad")))
      .finally(() => setLoading(false));
  }, [open, server.id, onLoadTools]);

  const prefix = server.tool_prefix || `mcp_${server.name.replace(/-/g, "_")}`;
  const q = search.toLowerCase();
  const filtered = tools.filter(
    (t) => t.name.toLowerCase().includes(q) || (t.description ?? "").toLowerCase().includes(q),
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Wrench className="h-4 w-4" />
            {t("tools.title", { name: server.display_name || server.name })}
          </DialogTitle>
          <p className="text-xs text-muted-foreground font-mono mt-1">
            {t("tools.prefix")} {prefix}
          </p>
        </DialogHeader>

        {loading ? (
          <div className="flex flex-col items-center justify-center py-12 gap-2 text-muted-foreground">
            <Loader2 className="h-6 w-6 animate-spin" />
            <span className="text-sm">{t("tools.discovering")}</span>
          </div>
        ) : error ? (
          <div className="flex flex-col items-center justify-center py-12 gap-2">
            <p className="text-sm text-destructive">{error}</p>
          </div>
        ) : tools.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-12 gap-2 text-muted-foreground">
            <Wrench className="h-8 w-8 opacity-40" />
            <p className="text-sm">{t("tools.noToolsTitle")}</p>
            <p className="text-xs">{t("tools.noToolsDescription")}</p>
          </div>
        ) : (
          <div className="flex flex-col gap-3 min-h-0">
            <div className="flex items-center gap-2">
              <div className="relative flex-1">
                <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
                <Input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder={t("tools.filterPlaceholder")}
                  className="pl-8 h-8 text-sm"
                />
              </div>
              <Badge variant="secondary" className="shrink-0">
                {filtered.length} / {tools.length}
              </Badge>
            </div>

            <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6 max-h-[50vh]">
              {filtered.length === 0 ? (
                <p className="text-sm text-muted-foreground text-center py-6">{t("tools.noMatch")}</p>
              ) : (
                <div className="grid gap-1.5">
                  {filtered.map((tool) => (
                    <div
                      key={tool.name}
                      className="px-3 py-2 rounded-md bg-muted/40 hover:bg-muted/70 transition-colors"
                    >
                      <div className="flex items-center gap-2">
                        <Wrench className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                        <span className="font-mono text-sm truncate">{tool.name}</span>
                      </div>
                      {tool.description && (
                        <p className="text-xs text-muted-foreground mt-0.5 ml-[22px] line-clamp-2">
                          {tool.description}
                        </p>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
