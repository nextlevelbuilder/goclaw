import { useState, useEffect, lazy, Suspense, useMemo } from "react";
import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { Zap, RefreshCw, Upload, ScanSearch, Download } from "lucide-react";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { cn } from "@/lib/utils";
import { toast } from "@/stores/use-toast-store";
import { useSkills, type SkillInfo } from "./hooks/use-skills";
import { SkillDetailDialog } from "./skill-detail-dialog";
import { SkillEditDialog } from "./skill-edit-dialog";
import { SkillAgentGrantsDialog } from "./skill-agent-grants-dialog";
import { SkillBulkActionsToolbar } from "./skill-bulk-actions-toolbar";

const SkillUploadDialog = lazy(() =>
  import("./skill-upload-dialog").then((m) => ({ default: m.SkillUploadDialog }))
);
import { MissingDepsPanel } from "./missing-deps-panel";
import { SkillTableRow } from "./skill-table-row";
import { useRuntimes } from "./hooks/use-runtimes";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { usePagination } from "@/hooks/use-pagination";
import { useTenants } from "@/hooks/use-tenants";
import { useAgents } from "@/pages/agents/hooks/use-agents";

const MASTER_TENANT_ID = "0193a5b0-7000-7000-8000-000000000001";

type Tab = "core" | "custom";

export function SkillsPage() {
  const { t } = useTranslation("skills");
  const {
    skills, loading, refresh, getSkill, uploadSkill, updateSkill, deleteSkill,
    listAgentGrants, grantSkillToAgent, grantSkillToAgents, revokeSkillFromAgent,
    deleteSkills, toggleSkills,
    getSkillVersions, getSkillFiles, getSkillFileContent, rescanDeps, installDeps, installSingleDep, toggleSkill,
    setTenantConfig, deleteTenantConfig,
  } = useSkills();
  const [params, setParams] = useSearchParams();
  const { runtimes } = useRuntimes();
  const { currentTenantId } = useTenants();
  const { agents } = useAgents();
  const hasTenantScope = !!currentTenantId && currentTenantId !== MASTER_TENANT_ID;
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && skills.length === 0);
  const urlTab = params.get("tab") === "custom" ? "custom" : "core";
  const tab: Tab = urlTab;
  const [search, setSearch] = useState("");
  const [selectedSkill, setSelectedSkill] = useState<(SkillInfo & { content: string }) | null>(null);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<SkillInfo | null>(null);
  const [grantsTarget, setGrantsTarget] = useState<SkillInfo | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<SkillInfo | null>(null);
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [bulkLoading, setBulkLoading] = useState(false);
  const [rescanning, setRescanning] = useState(false);
  const [installingDeps, setInstallingDeps] = useState(false);
  const [toggling, setToggling] = useState<string | null>(null);

  const coreSkills = useMemo(() => skills.filter((s: SkillInfo) => s.is_system), [skills]);
  const customSkills = useMemo(() => skills.filter((s: SkillInfo) => !s.is_system), [skills]);
  const tabSkills = tab === "core" ? coreSkills : customSkills;
  const allMissing = useMemo(
    () => [...new Set(tabSkills.flatMap((s: SkillInfo) => s.missing_deps ?? []))],
    [tabSkills],
  );
  const filtered = useMemo(
    () => tabSkills.filter(
      (s: SkillInfo) =>
        s.name.toLowerCase().includes(search.toLowerCase()) ||
        s.description.toLowerCase().includes(search.toLowerCase()),
    ),
    [search, tabSkills],
  );
  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(filtered);
  const selectedSkills = filtered.filter((skill) => skill.id && selectedIds.has(skill.id));
  const selectedCustomSkills = selectedSkills.filter((skill) => !skill.is_system);
  const pageSelectableIds = pageItems.map((skill) => skill.id).filter((id): id is string => !!id);
  const allPageSelected = pageSelectableIds.length > 0 && pageSelectableIds.every((id) => selectedIds.has(id));
  const somePageSelected = pageSelectableIds.some((id) => selectedIds.has(id)) && !allPageSelected;

  useEffect(() => { resetPage(); }, [search, tab, resetPage]);
  useEffect(() => { setSelectedIds(new Set()); }, [search, tab]);
  useEffect(() => {
    setSelectedIds((current) => {
      const valid = new Set(filtered.map((skill) => skill.id).filter(Boolean));
      const next = new Set(Array.from(current).filter((id) => valid.has(id)));
      return next.size === current.size ? current : next;
    });
  }, [filtered]);

  const setParamValues = (updates: Record<string, string | null>) => {
    const next = new URLSearchParams(params);
    for (const [key, value] of Object.entries(updates)) {
      if (value) next.set(key, value);
      else next.delete(key);
    }
    setParams(next, { replace: true });
  };

  const setTab = (nextTab: Tab) => {
    const next = new URLSearchParams(params);
    next.set("tab", nextTab);
    next.delete("skill");
    next.delete("detailTab");
    next.delete("version");
    next.delete("file");
    setParams(next, { replace: true });
  };

  const closeDetail = () => {
    const next = new URLSearchParams(params);
    next.delete("skill");
    next.delete("detailTab");
    next.delete("version");
    next.delete("file");
    setParams(next, { replace: true });
    setSelectedSkill(null);
  };

  const handleViewSkill = async (skill: SkillInfo) => {
    const next = new URLSearchParams(params);
    next.set("tab", skill.is_system ? "core" : "custom");
    next.set("skill", skill.id || skill.slug || skill.name);
    next.set("detailTab", "content");
    next.delete("version");
    next.delete("file");
    setParams(next, { replace: true });
  };

  useEffect(() => {
    const skillRef = params.get("skill");
    if (!skillRef) {
      setSelectedSkill(null);
      return;
    }
    let cancelled = false;
    getSkill(skillRef).then((detail) => {
      if (!cancelled) setSelectedSkill(detail);
    });
    return () => { cancelled = true; };
  }, [params, getSkill]);

  const handleCycleVisibility = async (skill: SkillInfo) => {
    if (!skill.id) return;
    const order = ["private", "internal", "public"] as const;
    const idx = order.indexOf(skill.visibility as typeof order[number]);
    await updateSkill(skill.id, { visibility: order[(idx + 1) % order.length] });
  };

  const handleDelete = async () => {
    if (!deleteTarget?.id) return;
    setDeleteLoading(true);
    try { await deleteSkill(deleteTarget.id); setDeleteTarget(null); refresh(); }
    finally { setDeleteLoading(false); }
  };

  const toggleSelectSkill = (skill: SkillInfo) => {
    if (!skill.id) return;
    setSelectedIds((current) => {
      const next = new Set(current);
      if (next.has(skill.id!)) next.delete(skill.id!);
      else next.add(skill.id!);
      return next;
    });
  };

  const toggleSelectPage = () => {
    setSelectedIds((current) => {
      const next = new Set(current);
      if (allPageSelected) {
        for (const id of pageSelectableIds) next.delete(id);
      } else {
        for (const id of pageSelectableIds) next.add(id);
      }
      return next;
    });
  };

  const runBulkAction = async (action: () => Promise<void>, successKey: string, count: number) => {
    setBulkLoading(true);
    try {
      await action();
      setSelectedIds(new Set());
      toast.success(t(successKey, { count }));
    } catch (err) {
      toast.error(t("bulk.failed"), err instanceof Error ? err.message : String(err));
    } finally {
      setBulkLoading(false);
    }
  };

  const handleBulkToggle = (enabled: boolean) => {
    const ids = selectedSkills.map((skill) => skill.id).filter((id): id is string => !!id);
    runBulkAction(async () => {
      if (hasTenantScope) {
        for (const id of ids) await setTenantConfig(id, enabled);
      } else {
        await toggleSkills(ids, enabled);
      }
    }, enabled ? "bulk.enabled" : "bulk.disabled", ids.length);
  };

  const handleBulkGrantAllAgents = () => {
    const agentIds = agents.map((agent) => agent.id).filter(Boolean);
    runBulkAction(async () => {
      for (const skill of selectedCustomSkills) {
        if (skill.id) await grantSkillToAgents(skill.id, agentIds, skill.version ?? 1, true);
      }
    }, "bulk.grantedAllAgents", selectedCustomSkills.length);
  };

  const handleBulkDelete = async () => {
    const ids = selectedCustomSkills.map((skill) => skill.id).filter((id): id is string => !!id);
    setDeleteLoading(true);
    try {
      await deleteSkills(ids);
      setBulkDeleteOpen(false);
      setSelectedIds(new Set());
      toast.success(t("bulk.deleted", { count: ids.length }));
    } catch (err) {
      toast.error(t("bulk.failed"), err instanceof Error ? err.message : String(err));
    } finally {
      setDeleteLoading(false);
    }
  };

  const handleRescanDeps = async () => {
    setRescanning(true);
    try { await rescanDeps(); } finally { setRescanning(false); }
  };

  const handleInstallDeps = async () => {
    setInstallingDeps(true);
    try { await installDeps(); } finally { setInstallingDeps(false); }
  };

  const handleToggle = async (skill: SkillInfo, enabled: boolean) => {
    if (!skill.id) return;
    setToggling(skill.id);
    try { await toggleSkill(skill.id, enabled); } finally { setToggling(null); }
  };

  const handleSetTenantConfig = async (id: string, enabled: boolean) => {
    setToggling(id);
    try { await setTenantConfig(id, enabled); } finally { setToggling(null); }
  };

  const handleDeleteTenantConfig = async (id: string) => {
    setToggling(id);
    try { await deleteTenantConfig(id); } finally { setToggling(null); }
  };

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            {tab === "custom" && (
              <Button variant="outline" size="sm" onClick={() => setUploadOpen(true)} className="gap-1">
                <Upload className="h-3.5 w-3.5" /> {t("upload.button")}
              </Button>
            )}
            <Button variant="outline" size="sm" onClick={handleRescanDeps} disabled={rescanning} className="gap-1">
              <ScanSearch className="h-3.5 w-3.5" /> {t("deps.rescan")}
            </Button>
            <Button variant="outline" size="sm" onClick={handleInstallDeps} disabled={installingDeps || allMissing.length === 0} className="gap-1">
              <Download className="h-3.5 w-3.5" /> {installingDeps ? t("deps.installing") : t("deps.installAll")}
            </Button>
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {t("refresh", { ns: "common" })}
            </Button>
          </div>
        }
      />

      <div className="flex gap-1 border-b mt-4">
        {(["core", "custom"] as Tab[]).map((tabKey) => (
          <button
            key={tabKey}
            type="button"
            className={cn(
              "px-3 py-1.5 text-sm font-medium border-b-2 -mb-px",
              tab === tabKey
                ? "border-primary text-primary"
                : "border-transparent text-muted-foreground hover:text-foreground",
            )}
            onClick={() => setTab(tabKey)}
          >
            {t(`tabs.${tabKey}`)} ({tabKey === "core" ? coreSkills.length : customSkills.length})
          </button>
        ))}
      </div>

      <SkillBulkActionsToolbar
        selectedCount={selectedSkills.length}
        customSelectedCount={selectedCustomSkills.length}
        agentCount={agents.length}
        loading={bulkLoading || deleteLoading}
        onEnable={() => handleBulkToggle(true)}
        onDisable={() => handleBulkToggle(false)}
        onGrantAllAgents={handleBulkGrantAllAgents}
        onDelete={() => setBulkDeleteOpen(true)}
        onClear={() => setSelectedIds(new Set())}
      />

      <div className="mt-4">
        <MissingDepsPanel missing={allMissing} onInstallItem={installSingleDep} runtimes={tab === "core" ? runtimes : undefined} />
        <SearchInput value={search} onChange={setSearch} placeholder={t("searchPlaceholder")} className="max-w-sm" />
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Zap}
            title={search ? t("noMatchTitle") : t("emptyTitle")}
            description={search ? t("noMatchDescription") : t("emptyDescription")}
          />
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="w-10 px-4 py-3">
                    <input
                      type="checkbox"
                      checked={allPageSelected}
                      ref={(el) => { if (el) el.indeterminate = somePageSelected; }}
                      onChange={toggleSelectPage}
                      aria-label={t("bulk.selectPage")}
                      className="h-4 w-4 cursor-pointer accent-primary"
                    />
                  </th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.name")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.description")}</th>
                  {tab === "custom" && <th className="px-4 py-3 text-left font-medium">{t("columns.agents")}</th>}
                  <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
                  {tab === "custom" && <th className="px-4 py-3 text-left font-medium">{t("columns.visibility")}</th>}
                  <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {pageItems.map((skill: SkillInfo) => (
                  <SkillTableRow
                    key={skill.name}
                    skill={skill}
                    tab={tab}
                    hasTenantScope={hasTenantScope}
                    toggling={toggling}
                    selected={!!skill.id && selectedIds.has(skill.id)}
                    onToggleSelect={toggleSelectSkill}
                    onView={handleViewSkill}
                    onEdit={setEditTarget}
                    onManageGrants={setGrantsTarget}
                    onDelete={setDeleteTarget}
                    onToggle={handleToggle}
                    onCycleVisibility={handleCycleVisibility}
                    onSetTenantConfig={handleSetTenantConfig}
                    onDeleteTenantConfig={handleDeleteTenantConfig}
                  />
                ))}
              </tbody>
            </table>
            <Pagination
              page={pagination.page}
              pageSize={pagination.pageSize}
              total={pagination.total}
              totalPages={pagination.totalPages}
              onPageChange={setPage}
              onPageSizeChange={setPageSize}
            />
          </div>
        )}
      </div>

      {selectedSkill && (
        <SkillDetailDialog
          skill={selectedSkill}
          detailTab={params.get("detailTab") || "content"}
          selectedVersionParam={params.get("version")}
          selectedFilePath={params.get("file")}
          onStateChange={setParamValues}
          onClose={closeDetail}
          getSkillVersions={getSkillVersions}
          getSkillFiles={getSkillFiles}
          getSkillFileContent={getSkillFileContent}
        />
      )}

      {editTarget && (
        <SkillEditDialog
          skill={editTarget}
          onClose={() => setEditTarget(null)}
          onSave={async (id, updates) => { await updateSkill(id, updates); setEditTarget(null); }}
        />
      )}

      {grantsTarget && (
        <SkillAgentGrantsDialog
          skill={grantsTarget}
          onClose={() => setGrantsTarget(null)}
          onLoad={listAgentGrants}
          onGrant={grantSkillToAgent}
          onGrantAll={grantSkillToAgents}
          onRevoke={revokeSkillFromAgent}
        />
      )}

      <Suspense fallback={null}>
        <SkillUploadDialog open={uploadOpen} onOpenChange={setUploadOpen} onUpload={uploadSkill} />
      </Suspense>

      <ConfirmDeleteDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("delete.title")}
        description={t("delete.description", { name: deleteTarget?.name })}
        confirmValue={deleteTarget?.name || ""}
        confirmLabel={t("delete.confirmLabel")}
        onConfirm={handleDelete}
        loading={deleteLoading}
      />
      <ConfirmDeleteDialog
        open={bulkDeleteOpen}
        onOpenChange={(open) => !open && setBulkDeleteOpen(false)}
        title={t("bulk.deleteTitle")}
        description={t("bulk.deleteDescription", { count: selectedCustomSkills.length })}
        confirmValue="DELETE"
        confirmLabel={t("delete.confirmLabel")}
        onConfirm={handleBulkDelete}
        loading={deleteLoading}
      />
    </div>
  );
}
