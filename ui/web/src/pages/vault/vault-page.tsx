import { useState, useEffect, useMemo, useCallback, lazy, Suspense } from "react";
import { useTranslation } from "react-i18next";
import { Search, FileArchive, Plus, PanelLeftOpen, FolderSync, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useTeams } from "@/pages/teams/hooks/use-teams";
import { useIsMobile } from "@/hooks/use-media-query";
import { useVaultDocuments, useVaultGraphData, useRescanWorkspace } from "./hooks/use-vault";
import { useEnrichmentProgress } from "./hooks/use-enrichment-progress";
import { VaultDocumentSidebar } from "./vault-document-sidebar";
import { VaultSearchDialog } from "./vault-search-dialog";
import { VaultCreateDialog } from "./vault-create-dialog";
import type { VaultDocument } from "@/types/vault";

const VaultGraphView = lazy(() =>
  import("./vault-graph-view").then((m) => ({ default: m.VaultGraphView })),
);
const VaultDetailDialog = lazy(() =>
  import("./vault-detail-dialog").then((m) => ({ default: m.VaultDetailDialog })),
);

const PAGE_SIZE = 100;

export function VaultPage() {
  const { t } = useTranslation("vault");
  const { agents } = useAgents();
  const { teams, load: loadTeams } = useTeams();
  const isMobile = useIsMobile();

  useEffect(() => { loadTeams(); }, [loadTeams]);

  const [selectedAgent, setSelectedAgent] = useState("");
  const [selectedTeam, setSelectedTeam] = useState("");
  const [docType, setDocType] = useState("");
  const [detailDoc, setDetailDoc] = useState<VaultDocument | null>(null);
  const [selectedDocId, setSelectedDocId] = useState<string | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [page, setPage] = useState(0);

  const { rescan, isPending: rescanPending } = useRescanWorkspace();
  const enrichment = useEnrichmentProgress();
  const enriching = enrichment?.running ?? false;

  const { documents, total, loading } = useVaultDocuments(selectedAgent, {
    teamId: selectedTeam || undefined,
    docType: docType || undefined,
    limit: PAGE_SIZE,
    offset: page * PAGE_SIZE,
  });
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  // Link counts from graph data (client-side computation)
  const { links } = useVaultGraphData(selectedAgent, { teamId: selectedTeam || undefined });
  const linkCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const l of links) {
      counts.set(l.from_doc_id, (counts.get(l.from_doc_id) ?? 0) + 1);
      counts.set(l.to_doc_id, (counts.get(l.to_doc_id) ?? 0) + 1);
    }
    return counts;
  }, [links]);

  const handleAgentChange = (v: string) => { setSelectedAgent(v); setPage(0); };
  const handleTeamChange = (v: string) => { setSelectedTeam(v); setPage(0); };
  const handleDocTypeChange = (v: string) => { setDocType(v); setPage(0); };

  // Sidebar click → open detail modal + highlight graph node
  const handleSidebarSelect = (doc: VaultDocument) => {
    setDetailDoc(doc);
    setSelectedDocId(doc.id);
    if (isMobile) setSidebarOpen(false);
  };

  // Graph single-click → highlight only
  const handleNodeSelect = useCallback((docId: string | null) => {
    setSelectedDocId(docId);
  }, []);

  // Graph double-click → open detail modal + highlight
  const handleNodeDoubleClick = useCallback((doc: VaultDocument) => {
    setDetailDoc(doc);
    setSelectedDocId(doc.id);
  }, []);

  const handleCloseDetail = () => { setDetailDoc(null); };

  return (
    <div className="relative flex h-full overflow-hidden">
      {/* Mobile drawer backdrop */}
      {isMobile && sidebarOpen && (
        <div className="fixed inset-0 z-40 bg-black/50" onClick={() => setSidebarOpen(false)} />
      )}

      {/* Sidebar */}
      <div className={
        isMobile
          ? `fixed inset-y-0 left-0 z-50 w-80 max-w-[85vw] transition-transform duration-200 ${sidebarOpen ? "translate-x-0" : "-translate-x-full"}`
          : "w-80 md:w-[280px] lg:w-80 shrink-0"
      }>
        <VaultDocumentSidebar
          documents={documents}
          selectedId={selectedDocId}
          linkCounts={linkCounts}
          onSelect={handleSidebarSelect}
          loading={loading}
          page={page}
          totalPages={totalPages}
          total={total}
          onPageChange={setPage}
          docType={docType}
          onDocTypeChange={handleDocTypeChange}
          agentId={selectedAgent}
          teamId={selectedTeam}
        />
      </div>

      {/* Main: header + graph + detail panel */}
      <div className="flex min-w-0 flex-1 flex-col">
        {/* Header */}
        <div className="flex h-10 items-center gap-2 px-3 border-b shrink-0">
          {isMobile && (
            <Button variant="ghost" size="xs" className="h-7 w-7 p-0" onClick={() => setSidebarOpen(true)}>
              <PanelLeftOpen className="h-4 w-4" />
            </Button>
          )}
          <FileArchive className="h-4 w-4 text-indigo-500 shrink-0" />
          <span className="text-sm font-semibold mr-auto">{t("title")}</span>

          {/* Filters */}
          <select value={selectedAgent} onChange={(e) => handleAgentChange(e.target.value)}
            className="text-base md:text-sm border rounded px-2 py-1 bg-background h-7">
            <option value="">{t("allAgents")}</option>
            {(agents ?? []).map((a) => <option key={a.id} value={a.id}>{a.display_name || a.agent_key}</option>)}
          </select>
          <select value={selectedTeam} onChange={(e) => handleTeamChange(e.target.value)}
            className="text-base md:text-sm border rounded px-2 py-1 bg-background h-7">
            <option value="">{t("allTeams", "All teams")}</option>
            {(teams ?? []).map((team) => <option key={team.id} value={team.id}>{team.name}</option>)}
          </select>
          <Button size="sm" variant="outline" onClick={() => setSearchOpen(true)} disabled={!selectedAgent}>
            <Search className="h-3.5 w-3.5" />
          </Button>
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button size="sm" variant="outline" onClick={() => rescan()} disabled={rescanPending || enriching}>
                  {(rescanPending || enriching) ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <FolderSync className="h-3.5 w-3.5" />}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{enriching ? t("enriching", "Enriching documents...") : t("rescanTooltip", "Rescan workspace")}</TooltipContent>
            </Tooltip>
          </TooltipProvider>
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <span>
                  <Button size="sm" onClick={() => setCreateOpen(true)}>
                    <Plus className="h-3.5 w-3.5" />
                  </Button>
                </span>
              </TooltipTrigger>
            </Tooltip>
          </TooltipProvider>
        </div>

        {/* Enrichment progress bar */}
        {enrichment && enrichment.total > 0 && (
          <div className="px-3 py-1.5 flex items-center gap-2 text-xs text-muted-foreground">
            <div className="flex-1 h-1.5 rounded-full bg-muted overflow-hidden">
              <div
                className="h-full bg-primary rounded-full transition-all duration-300"
                style={{ width: `${Math.round((enrichment.done / enrichment.total) * 100)}%` }}
              />
            </div>
            <span className="shrink-0">
              {enrichment.running
                ? `${t("enriching", "Enriching")} ${enrichment.done}/${enrichment.total}`
                : t("enrichComplete", "Enrichment complete")}
            </span>
          </div>
        )}

        {/* Graph — takes full remaining height */}
        <div className="flex-1 min-h-0 relative">
          <Suspense fallback={<div className="h-full animate-pulse bg-muted" />}>
            <VaultGraphView
              agentId={selectedAgent}
              teamId={selectedTeam || undefined}
              selectedDocId={selectedDocId}
              onNodeSelect={handleNodeSelect}
              onNodeDoubleClick={handleNodeDoubleClick}
            />
          </Suspense>
        </div>
      </div>

      {/* Search dialog — result opens detail modal */}
      {selectedAgent && (
        <VaultSearchDialog
          agentId={selectedAgent} open={searchOpen} onOpenChange={setSearchOpen}
          onSelectResult={(doc) => { setDetailDoc(doc); setSelectedDocId(doc.id); }}
        />
      )}
      <VaultCreateDialog open={createOpen} onOpenChange={setCreateOpen} defaultAgentId={selectedAgent} defaultTeamId={selectedTeam} />

      {/* Detail dialog for all document views */}
      <Suspense fallback={null}>
        <VaultDetailDialog
          doc={detailDoc} open={!!detailDoc}
          onOpenChange={(open) => { if (!open) handleCloseDetail(); }}
          onDeleted={handleCloseDetail}
        />
      </Suspense>
    </div>
  );
}
