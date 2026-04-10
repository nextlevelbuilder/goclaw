import { useState, useEffect, useMemo, useRef } from "react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useAgentLinks, type AgentLinkData, type CreateLinkParams } from "../hooks/use-agent-links";

const DIRECTIONS = ["outbound", "inbound", "bidirectional"] as const;

interface LinkCreateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editLink?: AgentLinkData | null;
  onSaved: () => void;
}

export function LinkCreateDialog({
  open, onOpenChange, editLink, onSaved,
}: LinkCreateDialogProps) {
  const { t } = useTranslation("teams");
  const { agents, refresh } = useAgents();
  const { createLink, updateLink } = useAgentLinks();

  const [sourceAgent, setSourceAgent] = useState("");
  const [targetAgent, setTargetAgent] = useState("");
  const [direction, setDirection] = useState<string>("outbound");
  const [description, setDescription] = useState("");
  const [maxConcurrent, setMaxConcurrent] = useState(5);
  const [saving, setSaving] = useState(false);

  const didRefresh = useRef(false);
  useEffect(() => {
    if (open && !didRefresh.current) { didRefresh.current = true; refresh(); }
    if (!open) didRefresh.current = false;
  }, [open, refresh]);

  // Populate fields when editing
  useEffect(() => {
    if (editLink) {
      setSourceAgent(editLink.source_agent_id);
      setTargetAgent(editLink.target_agent_id);
      setDirection(editLink.direction);
      setDescription(editLink.description ?? "");
      setMaxConcurrent(editLink.max_concurrent ?? 5);
    } else {
      setSourceAgent("");
      setTargetAgent("");
      setDirection("outbound");
      setDescription("");
      setMaxConcurrent(5);
    }
  }, [editLink, open]);

  const allAgentOptions = useMemo(
    () => agents
      .filter((a) => a.status === "active")
      .map((a) => ({ value: a.id, label: a.display_name || a.agent_key })),
    [agents],
  );

  // Exclude selected source from target options and vice versa
  const sourceOptions = useMemo(() => targetAgent ? allAgentOptions.filter((o) => o.value !== targetAgent) : allAgentOptions, [allAgentOptions, targetAgent]);
  const targetOptions = useMemo(() => sourceAgent ? allAgentOptions.filter((o) => o.value !== sourceAgent) : allAgentOptions, [allAgentOptions, sourceAgent]);

  const isEdit = !!editLink;
  const canSave = sourceAgent && targetAgent && direction && sourceAgent !== targetAgent;

  const handleSave = async () => {
    if (!canSave) return;
    setSaving(true);
    try {
      if (isEdit && editLink) {
        await updateLink({
          linkId: editLink.id,
          direction,
          description,
          maxConcurrent,
        });
      } else {
        const params: CreateLinkParams = {
          sourceAgent,
          targetAgent,
          direction,
          description,
          maxConcurrent,
        };
        await createLink(params);
      }
      onSaved();
      onOpenChange(false);
    } catch {
      // toast handled by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {isEdit ? t("links.editLink") : t("links.create")}
          </DialogTitle>
          <DialogDescription className="sr-only">{t("links.title")}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {/* Source agent */}
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("links.source")} *</label>
            {isEdit ? (
              <div className="rounded-md border bg-muted/40 px-3 py-2 text-sm text-muted-foreground">
                {editLink?.source_agent_key || editLink?.source_agent_id}
              </div>
            ) : (
              <Combobox
                value={sourceAgent}
                onChange={setSourceAgent}
                options={sourceOptions}
                placeholder={t("links.selectAgent")}
              />
            )}
          </div>

          {/* Target agent */}
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("links.target")} *</label>
            {isEdit ? (
              <div className="rounded-md border bg-muted/40 px-3 py-2 text-sm text-muted-foreground">
                {editLink?.target_display_name || editLink?.target_agent_key || editLink?.target_agent_id}
              </div>
            ) : (
              <Combobox
                value={targetAgent}
                onChange={setTargetAgent}
                options={targetOptions}
                placeholder={t("links.selectAgent")}
              />
            )}
          </div>

          {/* Direction */}
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("links.direction")} *</label>
            <Select value={direction} onValueChange={setDirection}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {DIRECTIONS.map((d) => (
                  <SelectItem key={d} value={d}>{t(`links.${d}`)}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Description */}
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("links.description")}</label>
            <input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t("links.descriptionPlaceholder")}
              className="w-full rounded-md border bg-background px-3 py-2 text-base md:text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>

          {/* Max concurrent */}
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("links.maxConcurrent")}</label>
            <input
              type="number"
              min={1}
              max={50}
              value={maxConcurrent}
              onChange={(e) => setMaxConcurrent(Math.max(1, Math.min(50, Number(e.target.value) || 5)))}
              className="w-full rounded-md border bg-background px-3 py-2 text-base md:text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
              {t("create.cancel")}
            </Button>
            <Button onClick={handleSave} disabled={!canSave || saving}>
              {saving && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
              {isEdit ? t("links.save") : t("links.create")}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
