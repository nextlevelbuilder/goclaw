import { useState, useEffect, useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Copy, Download } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { SearchInput } from "@/components/shared/search-input";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import { toast } from "@/stores/use-toast-store";
import type { SkillInfo, SkillFile, SkillVersions } from "@/types/skill";
import { buildTree } from "./skill-file-helpers";
import { FileBrowser } from "./skill-file-browser";
import { normalizeSkillDetailTab, parseSkillDetailVersionParam, shouldLoadSkillDetailFile } from "./lib/skill-detail-deeplink";
import { getSkillAccessModeKey } from "./lib/skill-access-mode";
import type { SkillExportFormat } from "./lib/skill-export-download";
import { SkillEvolutionPanel } from "./skill-evolution-panel";

interface SkillDetailDialogProps {
  skill: SkillInfo & { content: string };
  detailTab: string;
  selectedVersionParam: string | null;
  selectedFilePath: string | null;
  onStateChange: (updates: Record<string, string | null>) => void;
  onClose: () => void;
  exportFormat: SkillExportFormat;
  downloadLoading: boolean;
  onExportFormatChange: (format: SkillExportFormat) => void;
  onDownloadSkill: () => void;
  getSkillVersions: (id: string) => Promise<SkillVersions>;
  getSkillFiles: (id: string, version?: number) => Promise<SkillFile[]>;
  getSkillFileContent: (id: string, path: string, version?: number) => Promise<{ content: string; path: string; size: number }>;
}

export function SkillDetailDialog({
  skill,
  detailTab,
  selectedVersionParam,
  selectedFilePath,
  onStateChange,
  onClose,
  exportFormat,
  downloadLoading,
  onExportFormatChange,
  onDownloadSkill,
  getSkillVersions,
  getSkillFiles,
  getSkillFileContent,
}: SkillDetailDialogProps) {
  const { t } = useTranslation("skills");
  const hasFiles = !!skill.id;
  const hasEvolution = !!skill.id;
  const activeDetailTab = normalizeSkillDetailTab(detailTab, hasFiles, hasEvolution);
  const accessModeKey = getSkillAccessModeKey(skill.visibility);
  const accessModeLabel = accessModeKey === "unknown"
    ? t("accessMode.unknown", { value: skill.visibility || t("unknownOwner") })
    : t(`accessMode.${accessModeKey}`);

  // Version state
  const [versions, setVersions] = useState<SkillVersions | null>(null);
  const [selectedVersion, setSelectedVersion] = useState<number | null>(
    parseSkillDetailVersionParam(selectedVersionParam),
  );

  // File tree state
  const [files, setFiles] = useState<SkillFile[]>([]);
  const [filesLoading, setFilesLoading] = useState(false);
  const [activePath, setActivePath] = useState<string | null>(null);
  const [fileQuery, setFileQuery] = useState("");

  // File content state
  const [fileContent, setFileContent] = useState<{ content: string; path: string; size: number } | null>(null);
  const [contentLoading, setContentLoading] = useState(false);

  const filteredFiles = useMemo(
    () => filterSkillFiles(files, fileQuery, activePath),
    [activePath, fileQuery, files],
  );
  const tree = useMemo(() => buildTree(filteredFiles), [filteredFiles]);

  useEffect(() => {
    setVersions(null);
    setSelectedVersion(parseSkillDetailVersionParam(selectedVersionParam));
    setFiles([]);
    setActivePath(null);
    setFileQuery("");
    setFileContent(null);
  }, [skill.id, selectedVersionParam]);

  const loadVersions = useCallback(async () => {
    if (!skill.id || versions) return;
    const v = await getSkillVersions(skill.id);
    setVersions(v);
    if (!selectedVersionParam) {
      setSelectedVersion(v.current);
    }
  }, [skill.id, versions, selectedVersionParam, getSkillVersions]);

  const loadFiles = useCallback(async (version?: number) => {
    if (!skill.id) return;
    setFilesLoading(true);
    try {
      const f = await getSkillFiles(skill.id, version);
      setFiles(f);
      setActivePath(null);
      setFileContent(null);
    } finally {
      setFilesLoading(false);
    }
  }, [skill.id, getSkillFiles]);

  const loadFileContent = useCallback(async (path: string) => {
    if (!skill.id) return;
    setActivePath(path);
    setContentLoading(true);
    try {
      const c = await getSkillFileContent(skill.id, path, selectedVersion ?? undefined);
      setFileContent(c);
    } finally {
      setContentLoading(false);
    }
  }, [skill.id, selectedVersion, getSkillFileContent]);

  useEffect(() => {
    if (selectedVersion != null) {
      loadFiles(selectedVersion);
    }
  }, [selectedVersion, loadFiles]);

  useEffect(() => {
    if (activeDetailTab !== "files" || !hasFiles) return;
    loadVersions();
    const versionParam = parseSkillDetailVersionParam(selectedVersionParam);
    if (versionParam !== null && versionParam !== selectedVersion) {
      setSelectedVersion(versionParam);
      return;
    }
    if (selectedVersion == null && skill.version) {
      setSelectedVersion(skill.version);
    }
  }, [activeDetailTab, hasFiles, loadVersions, selectedVersion, selectedVersionParam, skill.version]);

  useEffect(() => {
    if (!shouldLoadSkillDetailFile(activeDetailTab, selectedFilePath, files.length, activePath)) return;
    loadFileContent(selectedFilePath);
  }, [activePath, activeDetailTab, files.length, loadFileContent, selectedFilePath]);

  const handleTabChange = (tab: string) => {
    onStateChange({ detailTab: tab, version: tab === "files" ? selectedVersionParam : null, file: tab === "files" ? activePath : null });
    if (tab === "files" && hasFiles) {
      loadVersions();
      if (files.length === 0 && !filesLoading) {
        loadFiles(selectedVersion ?? undefined);
      }
    }
  };

  const handleVersionChange = (v: string) => {
    const next = Number(v);
    setSelectedVersion(next);
    onStateChange({ version: v, file: null });
  };

  const handleFileSelect = (path: string) => {
    onStateChange({
      detailTab: "files",
      version: selectedVersion != null ? String(selectedVersion) : null,
      file: path,
    });
    loadFileContent(path);
  };

  const copyDeeplink = async () => {
    const url = new URL(window.location.href);
    url.pathname = "/skills";
    const next = new URLSearchParams(url.search);
    next.set("skill", skill.id || skill.slug || skill.name);
    next.set("detailTab", activeDetailTab);
    if (activeDetailTab === "files" && selectedVersion != null) next.set("version", String(selectedVersion));
    else next.delete("version");
    if (activeDetailTab === "files" && activePath) next.set("file", activePath);
    else next.delete("file");
    url.search = next.toString();

    try {
      await navigator.clipboard.writeText(url.toString());
      toast.success(t("detail.copySuccess"));
    } catch (err) {
      toast.error(t("detail.copyFailed"), err instanceof Error ? err.message : String(err));
    }
  };

  useEffect(() => {
    if (hasFiles) loadVersions();
  }, [hasFiles, loadVersions]);

  const headerVersion = selectedVersion ?? versions?.current ?? skill.version;

  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="max-h-[85vh] md:min-h-[60vh] overflow-hidden flex flex-col sm:max-w-2xl md:max-w-4xl lg:max-w-5xl xl:max-w-6xl 2xl:max-w-7xl">
        <DialogHeader>
          <div className="flex flex-col gap-2 pr-8 sm:flex-row sm:items-start sm:justify-between">
            <DialogTitle className="flex min-w-0 flex-wrap items-center gap-2">
              {skill.name}
              <Badge variant="outline">{skill.source || "file"}</Badge>
              {skill.visibility && (
                <Badge variant="secondary">{accessModeLabel}</Badge>
              )}
            </DialogTitle>
            <div className="flex shrink-0 flex-wrap items-center gap-2">
              <Select value={exportFormat} onValueChange={(value) => onExportFormatChange(value as SkillExportFormat)}>
                <SelectTrigger className="h-8 w-[104px]" aria-label={t("export.format")}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="zip">ZIP</SelectItem>
                  <SelectItem value="tar.gz">tar.gz</SelectItem>
                  <SelectItem value="tgz">tgz</SelectItem>
                </SelectContent>
              </Select>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-8 gap-1"
                disabled={downloadLoading || !skill.id}
                onClick={onDownloadSkill}
              >
                <Download className="h-3.5 w-3.5" />
                {downloadLoading ? t("export.downloading") : t("export.download")}
              </Button>
              <Button type="button" variant="outline" size="sm" className="h-8 gap-1" onClick={copyDeeplink}>
                <Copy className="h-3.5 w-3.5" />
                {t("detail.copyLink")}
              </Button>
              {versions && versions.versions.length > 1 ? (
                <>
                <span className="text-sm text-muted-foreground">{t("detail.version")}</span>
                <Select
                  value={String(headerVersion ?? versions.current)}
                  onValueChange={handleVersionChange}
                >
                  <SelectTrigger className="h-8 w-40">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {versions.versions.map((v) => (
                      <SelectItem key={v} value={String(v)}>
                        v{v}{v === versions.current ? ` ${t("detail.current")}` : ""}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                </>
              ) : headerVersion ? (
                <Badge variant="outline" className="w-fit shrink-0 font-normal">
                  v{headerVersion}
                </Badge>
              ) : null}
            </div>
          </div>
          {skill.description && (
            <p className="text-sm text-muted-foreground">{skill.description}</p>
          )}
          <div className="flex flex-wrap gap-1 pt-1 text-xs text-muted-foreground">
            {skill.author && <span>{t("columns.author")}: {skill.author}</span>}
            {skill.creator_agent && (
              <span>{t("agents.creator")}: {skill.creator_agent.display_name || skill.creator_agent.agent_key || skill.creator_agent.id}</span>
            )}
            {skill.manager_agents && skill.manager_agents.length > 0 && (
              <span>{t("agents.managers")}: {skill.manager_agents.map((agent) => agent.display_name || agent.agent_key || agent.id).join(", ")}</span>
            )}
          </div>
          {skill.tags && skill.tags.length > 0 && (
            <div className="flex flex-wrap gap-1 pt-1">
              {skill.tags.map((tag) => (
                <Badge key={tag} variant="outline" className="text-xs">{tag}</Badge>
              ))}
            </div>
          )}
        </DialogHeader>

        <Tabs value={activeDetailTab} className="flex-1 overflow-hidden flex flex-col" onValueChange={handleTabChange}>
          <TabsList>
            <TabsTrigger value="content">{t("detail.content")}</TabsTrigger>
            {hasFiles && <TabsTrigger value="files">{t("detail.files")}</TabsTrigger>}
            {hasEvolution && <TabsTrigger value="evolution">{t("evolution.tab")}</TabsTrigger>}
          </TabsList>

          <TabsContent value="content" className="flex-1 overflow-y-auto mt-2 -mx-4 px-4 sm:-mx-6 sm:px-6">
            {skill.content ? (
              <div className="overflow-hidden rounded-md border bg-muted/30 p-4">
                <MarkdownRenderer content={skill.content} />
              </div>
            ) : (
              <div className="rounded-md border border-dashed bg-muted/20 p-6 text-sm text-muted-foreground">
                {t("detail.noContent")}
              </div>
            )}
          </TabsContent>

          {hasFiles && (
            <TabsContent value="files" className="flex-1 overflow-hidden flex flex-col mt-2 gap-2">
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                <SearchInput
                  value={fileQuery}
                  onChange={setFileQuery}
                  placeholder={t("detail.fileSearch")}
                  className="w-full sm:max-w-xs"
                />
                <span className="text-xs text-muted-foreground">
                  {t("detail.filesShown", { shown: filteredFiles.length, total: files.length })}
                </span>
              </div>
              <FileBrowser
                tree={tree}
                filesLoading={filesLoading}
                activePath={activePath}
                onSelect={handleFileSelect}
                contentLoading={contentLoading}
                fileContent={fileContent}
              />
            </TabsContent>
          )}

          {hasEvolution && (
            <TabsContent value="evolution" className="flex-1 overflow-y-auto mt-2 -mx-4 px-4 sm:-mx-6 sm:px-6">
              <SkillEvolutionPanel skill={skill} active={activeDetailTab === "evolution"} />
            </TabsContent>
          )}
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}

function filterSkillFiles(files: SkillFile[], query: string, activePath: string | null): SkillFile[] {
  const q = query.trim().toLowerCase();
  if (!q) return files;

  const byPath = new Map(files.map((file) => [file.path, file]));
  const visible = new Set<string>();

  for (const file of files) {
    const matches = file.path.toLowerCase().includes(q) || file.name.toLowerCase().includes(q) || file.path === activePath;
    if (!matches) continue;
    visible.add(file.path);
    let parentPath = file.path.includes("/") ? file.path.slice(0, file.path.lastIndexOf("/")) : "";
    while (parentPath) {
      visible.add(parentPath);
      const nextSlash = parentPath.lastIndexOf("/");
      parentPath = nextSlash >= 0 ? parentPath.slice(0, nextSlash) : "";
    }
  }

  return files.filter((file) => visible.has(file.path) || (file.isDir && byPath.has(file.path) && visible.has(file.path)));
}
