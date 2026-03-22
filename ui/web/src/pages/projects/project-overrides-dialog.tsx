import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Loader2, Plus, Trash2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { KeyValueEditor } from "@/components/shared/key-value-editor";
import type { ProjectData, ProjectMCPOverride } from "./hooks/use-projects";

interface ProjectOverridesDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  project: ProjectData;
  onLoadOverrides: (projectId: string) => Promise<ProjectMCPOverride[]>;
  onSaveOverride: (projectId: string, serverName: string, envOverrides: Record<string, string>) => Promise<void>;
  onRemoveOverride: (projectId: string, serverName: string) => Promise<void>;
}

export function ProjectOverridesDialog({
  open,
  onOpenChange,
  project,
  onLoadOverrides,
  onSaveOverride,
  onRemoveOverride,
}: ProjectOverridesDialogProps) {
  const { t } = useTranslation("projects");
  const [overrides, setOverrides] = useState<ProjectMCPOverride[]>([]);
  const [loadingList, setLoadingList] = useState(false);
  const [error, setError] = useState("");

  // New override form state
  const [showAddForm, setShowAddForm] = useState(false);
  const [newServerName, setNewServerName] = useState("");
  const [newEnv, setNewEnv] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [deletingServer, setDeletingServer] = useState<string | null>(null);

  const loadOverrides = useCallback(async () => {
    setLoadingList(true);
    setError("");
    try {
      const result = await onLoadOverrides(project.id);
      setOverrides(result);
    } catch {
      setError(t("overrides.failedLoad"));
    } finally {
      setLoadingList(false);
    }
  }, [onLoadOverrides, project.id, t]);

  useEffect(() => {
    if (open) {
      loadOverrides();
      setShowAddForm(false);
      setNewServerName("");
      setNewEnv({});
    }
  }, [open, loadOverrides]);

  const handleSave = async () => {
    if (!newServerName.trim()) return;
    setSaving(true);
    try {
      await onSaveOverride(project.id, newServerName.trim(), newEnv);
      setShowAddForm(false);
      setNewServerName("");
      setNewEnv({});
      await loadOverrides();
    } catch {
      // toast already shown by hook
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (serverName: string) => {
    setDeletingServer(serverName);
    try {
      await onRemoveOverride(project.id, serverName);
      await loadOverrides();
    } catch {
      // toast already shown by hook
    } finally {
      setDeletingServer(null);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("overrides.title", { name: project.name })}</DialogTitle>
          <p className="text-xs text-muted-foreground mt-1">{t("overrides.description")}</p>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto min-h-0 space-y-4 py-2">
          {loadingList ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          ) : error ? (
            <p className="text-sm text-destructive text-center py-4">{error}</p>
          ) : overrides.length === 0 && !showAddForm ? (
            <div className="text-center py-8">
              <p className="text-sm text-muted-foreground">{t("overrides.noOverrides")}</p>
              <p className="text-xs text-muted-foreground mt-1">{t("overrides.noOverridesDescription")}</p>
            </div>
          ) : (
            <div className="space-y-3">
              {overrides.map((ov) => (
                <div key={ov.server_name} className="rounded-md border p-3">
                  <div className="flex items-center justify-between mb-2">
                    <span className="font-mono text-sm font-medium">{ov.server_name}</span>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleDelete(ov.server_name)}
                      disabled={deletingServer === ov.server_name}
                      className="text-destructive hover:text-destructive h-7 w-7 p-0"
                    >
                      {deletingServer === ov.server_name ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <Trash2 className="h-3.5 w-3.5" />
                      )}
                    </Button>
                  </div>
                  <div className="space-y-1">
                    {Object.entries(ov.env_overrides).map(([key, value]) => (
                      <div key={key} className="flex items-center gap-2 text-xs font-mono">
                        <span className="text-muted-foreground">{key}</span>
                        <span>=</span>
                        <span className="truncate">{value}</span>
                      </div>
                    ))}
                    {Object.keys(ov.env_overrides).length === 0 && (
                      <span className="text-xs text-muted-foreground italic">No variables</span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}

          {showAddForm && (
            <div className="rounded-md border p-3 space-y-3">
              <div className="grid gap-1.5">
                <Label htmlFor="override-server">{t("overrides.serverName")}</Label>
                <Input
                  id="override-server"
                  value={newServerName}
                  onChange={(e) => setNewServerName(e.target.value)}
                  placeholder={t("overrides.serverNamePlaceholder")}
                  className="font-mono text-base md:text-sm"
                />
              </div>
              <div className="grid gap-1.5">
                <Label>{t("overrides.envOverrides")}</Label>
                <KeyValueEditor
                  value={newEnv}
                  onChange={setNewEnv}
                  keyPlaceholder={t("overrides.envKeyPlaceholder")}
                  valuePlaceholder={t("overrides.envValuePlaceholder")}
                  addLabel={t("overrides.addVariable")}
                />
              </div>
              <div className="flex gap-2 justify-end">
                <Button variant="outline" size="sm" onClick={() => setShowAddForm(false)} disabled={saving}>
                  {t("form.cancel")}
                </Button>
                <Button size="sm" onClick={handleSave} disabled={saving || !newServerName.trim()}>
                  {saving ? t("overrides.saving") : t("overrides.save")}
                </Button>
              </div>
            </div>
          )}
        </div>

        {!showAddForm && (
          <div className="pt-2 border-t">
            <Button variant="outline" size="sm" onClick={() => setShowAddForm(true)} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> {t("overrides.addOverride")}
            </Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
