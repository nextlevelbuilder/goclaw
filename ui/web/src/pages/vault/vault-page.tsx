import { useState, useMemo, lazy, Suspense } from "react";
import { useTranslation } from "react-i18next";
import { Search, FileArchive, Plus, PanelLeftOpen } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useTeams } from "@/pages/teams/hooks/use-teams";
import { useIsMobile } from "@/hooks/use-media-query";
import { useVaultDocuments, useVaultGraphData } from "./hooks/use-vault";
import { VaultDocumentSidebar } from "./vault-document-sidebar";
import { VaultDetailPanel } from "./vault-detail-panel";
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
  const { teams } = useTeams();
  const isMobile = useIsMobile();

  const [selectedAgent, setSelectedAgent] = useState("");
  const [selectedTeam, setSelectedTeam] = useState("");
  const [selectedDoc, setSelectedDoc] = useState<VaultDocument | null>(null);
  const [selectedDocId, setSelectedDocId] = useState<string | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [searchResultDoc, setSearchResultDoc] = useState<VaultDocument | null>(null);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [page, setPage] = useState(0);

  const { documents, total, loading } = useVaultDocuments(selectedAgent, {
    teamId: selectedTeam || undefined,
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

  // Sidebar click → select doc + highlight graph node
  const handleSidebarSelect = (doc: VaultDocument) => {
    setSelectedDoc(doc);
    setSelectedDocId(doc.id);
    if (isMobile) setSidebarOpen(false);
  };

  // Graph single-click → highlight only
  const handleNodeSelect = (docId: string | null) => {
    setSelectedDocId(docId);
    if (!docId) setSelectedDoc(null);
  };

  // Graph double-click → open detail + highlight
  const handleNodeDoubleClick = (doc: VaultDocument) => {
    setSelectedDoc(doc);
    setSelectedDocId(doc.id);
  };

  const handleCloseDetail = () => { setSelectedDoc(null); setSelectedDocId(null); };

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
        />
      </div>

      {/* Main: header + graph + detail panel */}
      <div className="flex min-w-0 flex-1 flex-col">
        {/* Header */}
        <div className="flex items-center gap-2 px-3 py-2 border-b flex-wrap shrink-0">
          {isMobile && (
            <Button variant="ghost" size="xs" className="h-7 w-7 p-0" onClick={() => setSidebarOpen(true)}>
              <PanelLeftOpen className="h-4 w-4" />
            </Button>
          )}
          <FileArchive className="h-4 w-4 text-indigo-500 shrink-0" />
          <span className="text-sm font-semibold mr-auto">{t("title")}</span>

          {/* Filters */}
          <select value={selectedAgent} onChange={(e) => handleAgentChange(e.target.value)}
            className="text-base md:text-sm border rounded px-2 py-1 bg-background h-8">
            <option value="">{t("allAgents")}</option>
            {(agents ?? []).map((a) => <option key={a.id} value={a.id}>{a.display_name || a.agent_key}</option>)}
          </select>
          {(teams ?? []).length > 0 && (
            <select value={selectedTeam} onChange={(e) => handleTeamChange(e.target.value)}
              className="text-base md:text-sm border rounded px-2 py-1 bg-background h-8">
              <option value="">{t("allTeams", "All teams")}</option>
              {(teams ?? []).map((team) => <option key={team.id} value={team.id}>{team.name}</option>)}
            </select>
          )}
          <Button size="sm" variant="outline" onClick={() => setSearchOpen(true)} disabled={!selectedAgent}>
            <Search className="h-3.5 w-3.5" />
          </Button>
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <span>
                  <Button size="sm" onClick={() => setCreateOpen(true)} disabled={!selectedAgent}>
                    <Plus className="h-3.5 w-3.5" />
                  </Button>
                </span>
              </TooltipTrigger>
              {!selectedAgent && <TooltipContent>{t("selectAgentFirst", "Select an agent first")}</TooltipContent>}
            </Tooltip>
          </TooltipProvider>
        </div>

        {/* Graph */}
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

        {/* Detail panel (bottom, collapsible) */}
        <VaultDetailPanel
          doc={selectedDoc}
          open={!!selectedDoc}
          onClose={handleCloseDetail}
          onDeleted={handleCloseDetail}
        />
      </div>

      {/* Search dialog — result opens legacy modal */}
      {selectedAgent && (
        <VaultSearchDialog
          agentId={selectedAgent} open={searchOpen} onOpenChange={setSearchOpen}
          onSelectResult={setSearchResultDoc}
        />
      )}
      {selectedAgent && <VaultCreateDialog agentId={selectedAgent} open={createOpen} onOpenChange={setCreateOpen} />}

      {/* Legacy detail dialog for search results only */}
      <Suspense fallback={null}>
        <VaultDetailDialog
          doc={searchResultDoc} open={!!searchResultDoc}
          onOpenChange={(open) => !open && setSearchResultDoc(null)}
          onDeleted={() => setSearchResultDoc(null)}
        />
      </Suspense>
    </div>
  );
}
