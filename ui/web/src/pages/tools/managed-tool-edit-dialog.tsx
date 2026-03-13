import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { X } from "lucide-react";
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
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { ManagedToolInfo } from "@/types/tool";

interface ManagedToolEditDialogProps {
  tool: ManagedToolInfo;
  onClose: () => void;
  onSave: (id: string, updates: Record<string, unknown>) => Promise<unknown>;
}

export function ManagedToolEditDialog({ tool, onClose, onSave }: ManagedToolEditDialogProps) {
  const { t } = useTranslation("tools");
  const [name, setName] = useState(tool.name);
  const [description, setDescription] = useState(tool.description);
  const [visibility, setVisibility] = useState(tool.visibility ?? "private");
  const [runtime, setRuntime] = useState(tool.runtime ?? "");
  const [entryPoint, setEntryPoint] = useState(tool.entry_point ?? "");
  const [tags, setTags] = useState<string[]>(tool.tags ?? []);
  const [tagInput, setTagInput] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    setName(tool.name);
    setDescription(tool.description);
    setVisibility(tool.visibility ?? "private");
    setRuntime(tool.runtime ?? "");
    setEntryPoint(tool.entry_point ?? "");
    setTags(tool.tags ?? []);
  }, [tool]);

  const addTag = () => {
    const tag = tagInput.trim().toLowerCase();
    if (tag && !tags.includes(tag)) {
      setTags([...tags, tag]);
    }
    setTagInput("");
  };

  const removeTag = (tag: string) => {
    setTags(tags.filter((t) => t !== tag));
  };

  const handleSave = async () => {
    if (!tool.id) return;
    setLoading(true);
    setError("");
    try {
      await onSave(tool.id, { name, description, visibility, runtime, entry_point: entryPoint, tags });
      onClose();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t("managed.edit.toast.failed"));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("managed.edit.title")}</DialogTitle>
        </DialogHeader>

        <div className="flex flex-col gap-4">
          <div className="space-y-1.5">
            <Label htmlFor="tool-name">{t("managed.edit.name")}</Label>
            <Input
              id="tool-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="text-base md:text-sm"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="tool-desc">{t("managed.edit.description")}</Label>
            <Textarea
              id="tool-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t("managed.edit.descriptionPlaceholder")}
              rows={3}
              className="text-base md:text-sm"
            />
          </div>

          <div className="space-y-1.5">
            <Label>{t("managed.edit.visibility")}</Label>
            <Select value={visibility} onValueChange={setVisibility}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="private">Private</SelectItem>
                <SelectItem value="internal">Internal</SelectItem>
                <SelectItem value="public">Public</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="tool-runtime">{t("managed.edit.runtime")}</Label>
            <Input
              id="tool-runtime"
              value={runtime}
              onChange={(e) => setRuntime(e.target.value)}
              placeholder={t("managed.edit.runtimePlaceholder")}
              className="text-base md:text-sm"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="tool-entry-point">{t("managed.edit.entryPoint")}</Label>
            <Input
              id="tool-entry-point"
              value={entryPoint}
              onChange={(e) => setEntryPoint(e.target.value)}
              placeholder={t("managed.edit.entryPointPlaceholder")}
              className="text-base md:text-sm"
            />
          </div>

          <div className="space-y-1.5">
            <Label>{t("managed.edit.tags")}</Label>
            <div className="flex gap-2">
              <Input
                value={tagInput}
                onChange={(e) => setTagInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") { e.preventDefault(); addTag(); }
                }}
                placeholder="Add tag..."
                className="flex-1 text-base md:text-sm"
              />
              <Button type="button" variant="outline" size="sm" onClick={addTag}>
                Add
              </Button>
            </div>
            {tags.length > 0 && (
              <div className="mt-2 flex flex-wrap gap-1">
                {tags.map((tag) => (
                  <Badge key={tag} variant="secondary" className="gap-1">
                    {tag}
                    <button type="button" onClick={() => removeTag(tag)} className="hover:text-destructive">
                      <X className="h-3 w-3" />
                    </button>
                  </Badge>
                ))}
              </div>
            )}
          </div>
        </div>

        {error && <p className="text-sm text-destructive">{error}</p>}

        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={loading}>
            {t("managed.edit.cancel")}
          </Button>
          <Button onClick={handleSave} disabled={loading || !name.trim()}>
            {loading ? t("managed.edit.saving") : t("managed.edit.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
