import { useState, useEffect } from "react";
import { formatUserLabel } from "@/lib/format-user-label";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useMemoryDocuments } from "../hooks/use-memory";
import type { AgentData } from "@/types/agent";
import { memoryCreateSchema, type MemoryCreateFormData } from "@/schemas/memory.schema";

interface MemoryCreateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Pre-selected agent from parent filter (optional) */
  agentId?: string;
  agentName?: string;
  /** Known user/group IDs from existing docs */
  knownUserIds?: string[];
}

export function MemoryCreateDialog({ open, onOpenChange, agentId: parentAgentId, knownUserIds = [] }: MemoryCreateDialogProps) {
  const { t } = useTranslation("memory");
  const { t: tc } = useTranslation("common");
  const { agents } = useAgents();

  // UI-only state
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const form = useForm<MemoryCreateFormData>({
    resolver: zodResolver(memoryCreateSchema),
    mode: "onChange",
    defaultValues: {
      selectedAgentId: "",
      path: "",
      content: "",
      scopeMode: "global",
      selectedUserId: "",
      customUserId: "",
      autoIndex: true,
    },
  });

  const { register, watch, setValue, reset, formState: { errors } } = form;
  const selectedAgentId = watch("selectedAgentId");
  const scopeMode = watch("scopeMode");
  const selectedUserId = watch("selectedUserId");
  const autoIndex = watch("autoIndex");

  const effectiveAgentId = selectedAgentId || parentAgentId || "";
  const { createDocument, indexDocument } = useMemoryDocuments({ agentId: effectiveAgentId || undefined });

  const selectedAgent: AgentData | undefined = agents.find((a) => a.id === effectiveAgentId);

  useEffect(() => {
    if (open) {
      reset({
        selectedAgentId: parentAgentId || "",
        path: "",
        content: "",
        scopeMode: "global",
        selectedUserId: "",
        customUserId: "",
        autoIndex: true,
      });
      setError("");
    }
  }, [open, parentAgentId, reset]);

  const resolvedUserId = (): string | undefined => {
    const data = form.getValues();
    if (data.scopeMode === "global") return undefined;
    if (data.scopeMode === "existing") return data.selectedUserId || undefined;
    return data.customUserId.trim() || undefined;
  };

  const handleSubmit = async () => {
    const data = form.getValues();
    if (!effectiveAgentId) {
      setError(t("createDialog.agentRequired") ?? "Please select an agent");
      return;
    }
    if (!data.path.trim()) {
      setError(t("createDialog.pathRequired") ?? "Path is required");
      return;
    }
    if (!data.content.trim()) {
      setError(t("createDialog.contentRequired") ?? "Content is required");
      return;
    }

    setLoading(true);
    setError("");
    try {
      const uid = resolvedUserId();
      await createDocument(data.path.trim(), data.content, uid);
      if (data.autoIndex) {
        await indexDocument(data.path.trim(), uid);
      }
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("toast.failedCreate"));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !loading && onOpenChange(v)}>
      <DialogContent aria-describedby={undefined} className="max-w-3xl max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{t("createDialog.title")}</DialogTitle>
        </DialogHeader>

        <div className="grid gap-4 py-2 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
          {/* Agent selector */}
          <div className="grid gap-1.5">
            <Label htmlFor="mc-agent">{t("createDialog.agentId")}</Label>
            <select
              id="mc-agent"
              value={selectedAgentId || parentAgentId || ""}
              onChange={(e) => setValue("selectedAgentId", e.target.value)}
              className="h-9 rounded-md border bg-background px-3 text-base md:text-sm"
            >
              <option value="">{t("createDialog.agentIdPlaceholder")}</option>
              {agents.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.display_name || a.agent_key}
                </option>
              ))}
            </select>
            {selectedAgent?.workspace && (
              <p className="font-mono text-2xs text-muted-foreground">{selectedAgent.workspace}</p>
            )}
          </div>

          {/* Scope selector */}
          <div className="grid gap-1.5">
            <Label>{t("createDialog.scope")}</Label>
            <div className="flex gap-2">
              <Button
                type="button"
                variant={scopeMode === "global" ? "default" : "outline"}
                size="sm"
                onClick={() => setValue("scopeMode", "global")}
              >
                {t("scopeLabel.global")}
              </Button>
              {knownUserIds.length > 0 && (
                <Button
                  type="button"
                  variant={scopeMode === "existing" ? "default" : "outline"}
                  size="sm"
                  onClick={() => setValue("scopeMode", "existing")}
                >
                  {t("createDialog.existingScope")}
                </Button>
              )}
              <Button
                type="button"
                variant={scopeMode === "custom" ? "default" : "outline"}
                size="sm"
                onClick={() => setValue("scopeMode", "custom")}
              >
                {t("createDialog.customScope")}
              </Button>
            </div>
            {scopeMode === "existing" && knownUserIds.length > 0 && (
              <select
                value={selectedUserId}
                onChange={(e) => setValue("selectedUserId", e.target.value)}
                className="h-9 rounded-md border bg-background px-3 text-base md:text-sm"
              >
                <option value="">{t("createDialog.selectGroupUser")}</option>
                {knownUserIds.map((uid) => (
                  <option key={uid} value={uid}>
                    {formatUserLabel(uid)}
                  </option>
                ))}
              </select>
            )}
            {scopeMode === "custom" && (
              <Input
                placeholder="e.g. group:telegram:-100123456"
                className="font-mono text-sm"
                {...register("customUserId")}
              />
            )}
            <p className="text-xs text-muted-foreground">
              {t("createDialog.scopeHint")}
            </p>
          </div>

          {/* Path */}
          <div className="grid gap-1.5">
            <Label htmlFor="mc-path">{t("createDialog.path")}</Label>
            <Input
              id="mc-path"
              placeholder={t("createDialog.pathPlaceholder")}
              className="font-mono text-sm"
              {...register("path")}
            />
            {errors.path && (
              <p className="text-xs text-destructive">{errors.path.message}</p>
            )}
          </div>

          {/* Content */}
          <div className="grid gap-1.5">
            <Label htmlFor="mc-content">{t("createDialog.content")}</Label>
            <Textarea
              id="mc-content"
              placeholder={t("createDialog.contentPlaceholder")}
              className="font-mono text-xs min-h-[200px]"
              rows={10}
              {...register("content")}
            />
            {errors.content && (
              <p className="text-xs text-destructive">{errors.content.message}</p>
            )}
          </div>

          <div className="flex items-center gap-2">
            <Switch id="mc-index" checked={autoIndex} onCheckedChange={(v) => setValue("autoIndex", v)} />
            <Label htmlFor="mc-index">{t("createDialog.autoIndex")}</Label>
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            {tc("cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={loading}>
            {loading ? t("createDialog.creating") : t("createDialog.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
