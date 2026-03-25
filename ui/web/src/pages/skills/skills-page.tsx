import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Zap, Pencil, RefreshCw, Upload, Trash2, ScanSearch, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { cn } from "@/lib/utils";
import { useSkills, type SkillInfo } from "./hooks/use-skills";
import { SkillDetailDialog } from "./skill-detail-dialog";
import { SkillUploadDialog } from "./skill-upload-dialog";
import { SkillEditDialog } from "./skill-edit-dialog";
import { MissingDepsPanel } from "./missing-deps-panel";
import { useRuntimes } from "./hooks/use-runtimes";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { usePagination } from "@/hooks/use-pagination";
import { useTenants } from "@/hooks/use-tenants";

const MASTER_TENANT_ID = "0193a5b0-7000-7000-8000-000000000001";

const visibilityColor: Record<string, string> = {
  public: "default",
  internal: "secondary",
  private: "outline",
};

type Tab = "core" | "custom";

export function SkillsPage() {
  const { t } = useTranslation("skills");
  const {
    skills, loading, refresh, getSkill, uploadSkill, updateSkill, deleteSkill,
    getSkillVersions, getSkillFiles, getSkillFileContent, rescanDeps, installSingleDep, toggleSkill,
    setTenantConfig, deleteTenantConfig,
  } = useSkills();
  const { runtimes } = useRuntimes();
  const { currentTenantId } = useTenants();
  const hasTenantScope = !!currentTenantId && currentTenantId !== MASTER_TENANT_ID;
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && skills.length === 0);
  const [tab, setTab] = useState<Tab>("core");
  const [search, setSearch] = useState("");
  const [selectedSkill, setSelectedSkill] = useState<(SkillInfo & { content: string }) | null>(null);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<SkillInfo | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<SkillInfo | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [rescanning, setRescanning] = useState(false);
  const [toggling, setToggling] = useState<string | null>(null);

  const coreSkills = skills.filter((s: SkillInfo) => s.is_system);
  const customSkills = skills.filter((s: SkillInfo) => !s.is_system);
  const tabSkills = tab === "core" ? coreSkills : customSkills;

  const allMissing = [...new Set(tabSkills.flatMap((s: SkillInfo) => s.missing_deps ?? []))];

  const filtered = tabSkills.filter(
    (s: SkillInfo) =>
      s.name.toLowerCase().includes(search.toLowerCase()) ||
      s.description.toLowerCase().includes(search.toLowerCase()),
  );

  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(filtered);

  useEffect(() => { resetPage(); }, [search, tab, resetPage]);

  const handleViewSkill = async (name: string) => {
    const detail = await getSkill(name);
    if (detail) setSelectedSkill(detail);
  };

  const handleUpload = async (file: File) => {
    await uploadSkill(file);
    refresh();
  };

  const handleCycleVisibility = async (skill: SkillInfo) => {
    if (!skill.id) return;
    const order = ["private", "internal", "public"] as const;
    const idx = order.indexOf(skill.visibility as typeof order[number]);
    const next = order[(idx + 1) % order.length];
    await updateSkill(skill.id, { visibility: next });
  };

  const handleDelete = async () => {
    if (!deleteTarget?.id) return;
    setDeleteLoading(true);
    try {
      await deleteSkill(deleteTarget.id);
      setDeleteTarget(null);
      refresh();
    } finally {
      setDeleteLoading(false);
    }
  };

  const handleRescanDeps = async () => {
    setRescanning(true);
    try {
      await rescanDeps();
    } finally {
      setRescanning(false);
    }
  };

  const handleToggle = async (skill: SkillInfo, enabled: boolean) => {
    if (!skill.id) return;
    setToggling(skill.id);
    try {
      await toggleSkill(skill.id, enabled);
    } finally {
      setToggling(null);
    }
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
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {t("refresh", { ns: "common" })}
            </Button>
          </div>
        }
      />

      {/* Tabs */}
      <div className="flex gap-1 border-b mt-4">
        <button
          type="button"
          className={cn(
            "px-3 py-1.5 text-sm font-medium border-b-2 -mb-px",
            tab === "core"
              ? "border-primary text-primary"
              : "border-transparent text-muted-foreground hover:text-foreground",
          )}
          onClick={() => setTab("core")}
        >
          {t("tabs.core")} ({coreSkills.length})
        </button>
        <button
          type="button"
          className={cn(
            "px-3 py-1.5 text-sm font-medium border-b-2 -mb-px",
            tab === "custom"
              ? "border-primary text-primary"
              : "border-transparent text-muted-foreground hover:text-foreground",
          )}
          onClick={() => setTab("custom")}
        >
          {t("tabs.custom")} ({customSkills.length})
        </button>
      </div>

      <div className="mt-4">
        <MissingDepsPanel
          missing={allMissing}
          onInstallItem={installSingleDep}
          runtimes={tab === "core" ? runtimes : undefined}
        />

        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("searchPlaceholder")}
          className="max-w-sm"
        />
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
                  <th className="px-4 py-3 text-left font-medium">{t("columns.name")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.description")}</th>
                  {tab === "custom" && <th className="px-4 py-3 text-left font-medium">{t("columns.author")}</th>}
                  <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
                  {tab === "custom" && <th className="px-4 py-3 text-left font-medium">{t("columns.visibility")}</th>}
                  <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {pageItems.map((skill: SkillInfo) => {
                  const isArchived = skill.status === "archived";
                  const isDisabled = skill.enabled === false;
                  const hasMissing = (skill.missing_deps?.length ?? 0) > 0;
                  return (
                  <tr key={skill.name} className={cn("border-b last:border-0 hover:bg-muted/30", (isArchived || isDisabled) && "opacity-60")}>
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2 flex-wrap">
                        <Zap className="h-4 w-4 text-muted-foreground shrink-0" />
                        <button
                          type="button"
                          className="font-medium text-left hover:underline cursor-pointer"
                          onClick={() => handleViewSkill(skill.slug ?? skill.name)}
                        >
                          {skill.name}
                        </button>
                        {skill.is_system && (
                          <Badge variant="outline" className="border-blue-500 text-blue-600 text-[10px]">
                            {t("system")}
                          </Badge>
                        )}
                        {skill.version ? (
                          <span className="text-xs text-muted-foreground">v{skill.version}</span>
                        ) : null}
                      </div>
                    </td>
                    <td className="max-w-xs truncate px-4 py-3 text-muted-foreground">
                      {skill.description || t("noDescription")}
                    </td>
                    {tab === "custom" && (
                      <td className="px-4 py-3 text-sm text-muted-foreground">
                        {skill.author || "—"}
                      </td>
                    )}
                    <td className="px-4 py-3">
                      <div className="flex flex-col gap-1">
                        <Badge
                          variant="outline"
                          className={cn(
                            "text-[10px] w-fit",
                            isArchived
                              ? "border-amber-500 text-amber-600 dark:border-amber-600 dark:text-amber-400"
                              : "border-emerald-500 text-emerald-600 dark:border-emerald-600 dark:text-emerald-400",
                          )}
                        >
                          {isArchived ? t("deps.statusArchived") : t("deps.statusActive")}
                        </Badge>
                        {hasMissing && (() => {
                          const deps = skill.missing_deps!.map((d) => d.replace(/^(pip|npm):/, ""));
                          const shown = deps.slice(0, 3);
                          const rest = deps.length - shown.length;
                          return (
                            <span className="text-[10px] text-amber-600 dark:text-amber-400 leading-tight">
                              {shown.join(", ")}{rest > 0 && `, +${rest}`}
                            </span>
                          );
                        })()}
                      </div>
                    </td>
                    {tab === "custom" && <td className="px-4 py-3">
                      {skill.visibility && (
                        skill.id ? (
                          <button
                            type="button"
                            onClick={() => handleCycleVisibility(skill)}
                            title={t("visibility.clickToCycle")}
                          >
                            <Badge
                              variant={visibilityColor[skill.visibility] as "default" | "secondary" | "outline"}
                              className="cursor-pointer hover:opacity-80 transition-opacity"
                            >
                              {skill.visibility}
                            </Badge>
                          </button>
                        ) : (
                          <Badge variant={visibilityColor[skill.visibility] as "default" | "secondary" | "outline"}>
                            {skill.visibility}
                          </Badge>
                        )
                      )}
                    </td>}
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-2">
                        {skill.id && (
                          <>
                            {hasTenantScope ? (
                              <SkillTenantOverride
                                skill={skill}
                                toggling={toggling === skill.id}
                                onSetTenantConfig={async (id, enabled) => {
                                  setToggling(id);
                                  try { await setTenantConfig(id, enabled); } finally { setToggling(null); }
                                }}
                                onDeleteTenantConfig={async (id) => {
                                  setToggling(id);
                                  try { await deleteTenantConfig(id); } finally { setToggling(null); }
                                }}
                              />
                            ) : (
                              <Switch
                                size="sm"
                                checked={skill.enabled !== false}
                                disabled={toggling === skill.id}
                                onCheckedChange={(checked) => handleToggle(skill, checked)}
                                title={skill.enabled !== false ? t("toggle.disable") : t("toggle.enable")}
                              />
                            )}
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => setEditTarget(skill)}
                              className="gap-1"
                            >
                              <Pencil className="h-3.5 w-3.5" />
                            </Button>
                            {!skill.is_system && (
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => setDeleteTarget(skill)}
                                className="gap-1 text-destructive hover:text-destructive"
                              >
                                <Trash2 className="h-3.5 w-3.5" />
                              </Button>
                            )}
                          </>
                        )}
                      </div>
                    </td>
                  </tr>
                  );
                })}
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
          onClose={() => setSelectedSkill(null)}
          getSkillVersions={getSkillVersions}
          getSkillFiles={getSkillFiles}
          getSkillFileContent={getSkillFileContent}
        />
      )}

      {editTarget && (
        <SkillEditDialog
          skill={editTarget}
          onClose={() => setEditTarget(null)}
          onSave={async (id, updates) => {
            await updateSkill(id, updates);
            setEditTarget(null);
          }}
        />
      )}

      <SkillUploadDialog
        open={uploadOpen}
        onOpenChange={setUploadOpen}
        onUpload={handleUpload}
      />

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
    </div>
  );
}

function SkillTenantOverride({
  skill,
  toggling,
  onSetTenantConfig,
  onDeleteTenantConfig,
}: {
  skill: SkillInfo;
  toggling: boolean;
  onSetTenantConfig: (id: string, enabled: boolean) => Promise<void>;
  onDeleteTenantConfig: (id: string) => Promise<void>;
}) {
  const { t } = useTranslation("skills");
  const hasOverride = skill.tenant_enabled !== null && skill.tenant_enabled !== undefined;
  const checked = hasOverride ? (skill.tenant_enabled ?? false) : (skill.enabled !== false);
  const label = hasOverride
    ? skill.tenant_enabled ? t("tenant.enabled") : t("tenant.disabled")
    : t("tenant.default");
  const badgeVariant = hasOverride
    ? skill.tenant_enabled ? "default" : "secondary"
    : "outline";

  return (
    <div className="flex items-center gap-1">
      <TooltipProvider delayDuration={200}>
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge
              variant={badgeVariant as "default" | "secondary" | "outline"}
              className="h-5 cursor-default px-1.5 text-[10px] leading-none"
            >
              {label}
            </Badge>
          </TooltipTrigger>
          <TooltipContent side="top">
            <p className="text-xs">{t("tenant.overrideHint")}</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <Switch
        size="sm"
        checked={checked}
        disabled={toggling}
        onCheckedChange={(val) => onSetTenantConfig(skill.id!, val)}
        aria-label={t("tenant.overrideHint")}
      />
      {hasOverride && (
        <TooltipProvider delayDuration={200}>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                disabled={toggling}
                onClick={() => onDeleteTenantConfig(skill.id!)}
                className="h-5 w-5 p-0 text-muted-foreground hover:text-foreground"
              >
                <X className="h-3 w-3" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="top">
              <p className="text-xs">{t("tenant.resetDefault")}</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      )}
    </div>
  );
}
