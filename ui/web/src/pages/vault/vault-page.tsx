import { useState, lazy, Suspense } from "react";
import { useTranslation } from "react-i18next";
import { Search, FileArchive, TableProperties, GitGraph, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useVaultDocuments, useVaultAllLinks } from "./hooks/use-vault";
import { VaultDocumentsTable } from "./vault-documents-table";
import { VaultSearchDialog } from "./vault-search-dialog";
import { VaultCreateDialog } from "./vault-create-dialog";
import type { VaultDocument } from "@/types/vault";

const VaultGraphView = lazy(() =>
  import("./vault-graph-view").then((m) => ({ default: m.VaultGraphView })),
);
const VaultDetailDialog = lazy(() =>
  import("./vault-detail-dialog").then((m) => ({ default: m.VaultDetailDialog }))
);

export function VaultPage() {
  const { t } = useTranslation("vault");
  const { agents } = useAgents();

  const [selectedAgent, setSelectedAgent] = useState("");
  const [selectedDoc, setSelectedDoc] = useState<VaultDocument | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [viewMode, setViewMode] = useState<"table" | "graph">("table");

  const { documents, loading } = useVaultDocuments(selectedAgent, { limit: 50 });
  const { links } = useVaultAllLinks(selectedAgent, viewMode === "graph" ? documents : []);

  return (
    <div className="p-3 sm:p-4 space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="flex items-center gap-2">
          <FileArchive className="h-5 w-5 text-indigo-500" />
          <div>
            <h1 className="text-lg font-semibold">{t("title")}</h1>
            <p className="text-xs text-muted-foreground">{t("description")}</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <span>
                  <Button
                    size="sm"
                    onClick={() => setCreateOpen(true)}
                    disabled={!selectedAgent}
                  >
                    <Plus className="h-4 w-4 mr-1" />
                    {t("create")}
                  </Button>
                </span>
              </TooltipTrigger>
              {!selectedAgent && (
                <TooltipContent>{t("selectAgentFirst", "Select an agent first")}</TooltipContent>
              )}
            </Tooltip>
          </TooltipProvider>
          <Button
            size="sm" variant="outline"
            onClick={() => setSearchOpen(true)}
            disabled={!selectedAgent}
          >
            <Search className="h-4 w-4 mr-1" />
            {t("search")}
          </Button>
          {/* Table / Graph toggle */}
          <TooltipProvider>
            <div className="flex items-center gap-0.5 rounded-md border p-0.5">
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant={viewMode === "table" ? "default" : "ghost"}
                    size="xs" className="h-7 w-7 p-0"
                    onClick={() => setViewMode("table")}
                  >
                    <TableProperties className="h-3.5 w-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t("viewTable", "Table")}</TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant={viewMode === "graph" ? "default" : "ghost"}
                    size="xs" className="h-7 w-7 p-0"
                    onClick={() => setViewMode("graph")}
                    disabled={!selectedAgent}
                  >
                    <GitGraph className="h-3.5 w-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t("viewGraph", "Graph")}</TooltipContent>
              </Tooltip>
            </div>
          </TooltipProvider>
        </div>
      </div>

      {/* Agent filter */}
      <div className="flex items-center gap-2">
        <label className="text-xs text-muted-foreground shrink-0">{t("filterAgent")}:</label>
        <select
          value={selectedAgent}
          onChange={(e) => setSelectedAgent(e.target.value)}
          className="text-base md:text-sm border rounded px-2 py-1 bg-background"
        >
          <option value="">{t("allAgents")}</option>
          {(agents ?? []).map((a) => (
            <option key={a.id} value={a.id}>
              {a.display_name || a.agent_key}
            </option>
          ))}
        </select>
      </div>

      {/* Content: Table or Graph */}
      {viewMode === "table" ? (
        <VaultDocumentsTable
          documents={documents}
          agents={agents}
          loading={loading}
          onSelect={setSelectedDoc}
        />
      ) : (
        <Suspense fallback={<div className="h-[400px] animate-pulse rounded-md bg-muted" />}>
          <VaultGraphView
            documents={documents}
            links={links}
            onNodeClick={setSelectedDoc}
          />
        </Suspense>
      )}

      {/* Detail dialog */}
      <Suspense fallback={null}>
        <VaultDetailDialog
          doc={selectedDoc}
          open={!!selectedDoc}
          onOpenChange={(open) => !open && setSelectedDoc(null)}
          onDeleted={() => setSelectedDoc(null)}
        />
      </Suspense>

      {/* Create document dialog */}
      {selectedAgent && (
        <VaultCreateDialog
          agentId={selectedAgent}
          open={createOpen}
          onOpenChange={setCreateOpen}
        />
      )}

      {/* Search dialog */}
      {selectedAgent && (
        <VaultSearchDialog
          agentId={selectedAgent}
          open={searchOpen}
          onOpenChange={setSearchOpen}
          onSelectResult={setSelectedDoc}
        />
      )}
    </div>
  );
}
