import { useState, useEffect, useMemo, useCallback, lazy, Suspense } from "react";
import { useTranslation } from "react-i18next";
import { Search, FileArchive, Plus, PanelLeftOpen, FolderSync, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useHttp } from "@/hooks/use-ws";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useTeams } from "@/pages/teams/hooks/use-teams";
import { useIsMobile } from "@/hooks/use-media-query";
import { useRescanWorkspace } from "./hooks/use-vault";
import { useVaultTree } from "./hooks/use-vault-tree";
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

export function VaultPage() {
  const { t } = useTranslation("vault");
  const http = useHttp();
  const { agents } = useAgents();
  const { teams, load: loadTeams } = useTeams();
  const isMobile = useIsMobile();

  useEffect(() => { loadTeams(); }, [loadTeams]);

  const [selectedAgent, setSelectedAgent] = useState("");
  const [selectedTeam, setSelectedTeam] = useState("");
  const [docType, setDocType] = useState("");
  const [detailDoc, setDetailDoc] = useState<VaultDocument | null>(null);
  const [selectedDocId, setSelectedDocId] = useState<string | null>(null);
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);

  const { rescan, isPending: rescanPending } = useRescanWorkspace();
  const enrichment = useEnrichmentProgress();
  const enriching = enrichment?.running ?? false;

  const treeFilter = useMemo(() => ({
    agent_id: selectedAgent || undefined,
    doc_type: docType || undefined,
    team_id: selectedTeam || undefined,
  }), [selectedAgent, docType, selectedTeam]);
  const { tree, meta, loading, loadRoot, loadSubtree } = useVaultTree(treeFilter);

  useEffect(() => { loadRoot(); }, [loadRoot]);

  const handleAgentChange = (v: string) => { setSelectedAgent(v); };
  const handleTeamChange = (v: string) => { setSelectedTeam(v); };
  const handleDocTypeChange = (v: string) => { setDocType(v); };

  // Tree file click → fetch full doc for detail modal
  const handleTreeSelect = useCallback(async (path: string) => {
    setSelectedPath(path);
    const entry = meta.get(path);
    if (!entry?.docId) return;
    setSelectedDocId(entry.docId);
    try {
      const doc = await http.get<VaultDocument>(`/v1/vault/documents/${entry.docId}`);
      setDetailDoc(doc);
    } catch { /* http layer handles toast */ }
    if (isMobile) setSidebarOpen(false);
  }, [meta, http, isMobile]);

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
      {isMobile && sidebarOpen && (
        <div className="fixed inset-0 z-40 bg-black/50" onClick={() => setSidebarOpen(false)} />
      )}

      {/* Sidebar */}
      <div className={
        isMobile
          ? `fixed inset-y-0 left-0 z-50 w-80 max-w-[85vw] transition-transform duration-200 ${sidebarOpen ? "translate-x-0" : "-translate-x-full"}`
          : "w-80 md:w-80 lg:w-96 shrink-0"
      }>
        <VaultDocumentSidebar
          tree={tree}
          meta={meta}
          selectedPath={selectedPath}
          onSelect={handleTreeSelect}
          onLoadMore={loadSubtree}
          loading={loading}
          docType={docType}
          onDocTypeChange={handleDocTypeChange}
          agentId={selectedAgent}
          teamId={selectedTeam}
        />
      </div>

      {/* Main: header + graph + detail panel */}
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex h-10 items-center gap-2 px-3 border-b shrink-0">
          {isMobile && (
            <Button variant="ghost" size="xs" className="h-7 w-7 p-0" onClick={() => setSidebarOpen(true)}>
              <PanelLeftOpen className="h-4 w-4" />
            </Button>
          )}
          <FileArchive className="h-4 w-4 text-indigo-500 shrink-0" />
          <span className="text-sm font-semibold mr-auto">{t("title")}</span>

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

        {enrichment && enrichment.total > 0 && (
          <div className="px-3 py-1.5 flex items-center gap-2 text-xs text-muted-foreground">
            <div className="flex-1 h-1.5 rounded-full bg-muted overflow-hidden">
              <div className="h-full bg-primary rounded-full transition-all duration-300"
                style={{ width: `${Math.round((enrichment.done / enrichment.total) * 100)}%` }} />
            </div>
            <span className="shrink-0">
              {enrichment.running
                ? `${t("enriching", "Enriching")} ${enrichment.done}/${enrichment.total}`
                : t("enrichComplete", "Enrichment complete")}
            </span>
          </div>
        )}

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

      {selectedAgent && (
        <VaultSearchDialog
          agentId={selectedAgent} open={searchOpen} onOpenChange={setSearchOpen}
          onSelectResult={(doc) => { setDetailDoc(doc); setSelectedDocId(doc.id); }}
        />
      )}
      <VaultCreateDialog open={createOpen} onOpenChange={setCreateOpen} defaultAgentId={selectedAgent} defaultTeamId={selectedTeam} />

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
