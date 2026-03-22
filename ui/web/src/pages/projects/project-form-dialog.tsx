import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { slugify, isValidSlug } from "@/lib/slug";
import type { ProjectData, ProjectInput } from "./hooks/use-projects";

const CHANNEL_TYPES = [
  { value: "", label: "—" },
  { value: "telegram", label: "Telegram" },
  { value: "zalo_oa", label: "Zalo OA" },
  { value: "discord", label: "Discord" },
  { value: "slack", label: "Slack" },
  { value: "feishu", label: "Feishu/Lark" },
  { value: "whatsapp", label: "WhatsApp" },
  { value: "google_chat", label: "Google Chat" },
];

const STATUS_OPTIONS = [
  { value: "active", label: "Active" },
  { value: "archived", label: "Archived" },
];

interface ProjectFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  project?: ProjectData | null;
  onSubmit: (data: ProjectInput) => Promise<unknown>;
}

export function ProjectFormDialog({ open, onOpenChange, project, onSubmit }: ProjectFormDialogProps) {
  const { t } = useTranslation("projects");
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [channelType, setChannelType] = useState("");
  const [chatId, setChatId] = useState("");
  const [description, setDescription] = useState("");
  const [status, setStatus] = useState("active");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [autoSlug, setAutoSlug] = useState(true);

  useEffect(() => {
    if (open) {
      setName(project?.name ?? "");
      setSlug(project?.slug ?? "");
      setChannelType(project?.channel_type ?? "");
      setChatId(project?.chat_id ?? "");
      setDescription(project?.description ?? "");
      setStatus(project?.status ?? "active");
      setError("");
      setAutoSlug(!project);
    }
  }, [open, project]);

  const handleNameChange = (value: string) => {
    setName(value);
    if (autoSlug) {
      setSlug(slugify(value));
    }
  };

  const handleSlugChange = (value: string) => {
    setAutoSlug(false);
    setSlug(slugify(value));
  };

  const handleSubmit = async () => {
    if (!name.trim() || !slug.trim()) {
      setError(t("form.errors.nameRequired"));
      return;
    }
    if (!isValidSlug(slug)) {
      setError(t("form.errors.slugInvalid"));
      return;
    }

    setLoading(true);
    setError("");
    try {
      await onSubmit({
        name: name.trim(),
        slug: slug.trim(),
        channel_type: channelType || null,
        chat_id: chatId.trim() || null,
        description: description.trim() || null,
        status,
      });
      onOpenChange(false);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t("form.saving"));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !loading && onOpenChange(v)}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{project ? t("form.editTitle") : t("form.createTitle")}</DialogTitle>
        </DialogHeader>

        <div className="grid gap-4 py-2 px-0.5 -mx-0.5 overflow-y-auto min-h-0">
          <div className="grid gap-1.5">
            <Label htmlFor="proj-name">{t("form.name")}</Label>
            <Input id="proj-name" value={name} onChange={(e) => handleNameChange(e.target.value)} placeholder="XPOS" className="text-base md:text-sm" />
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="proj-slug">{t("form.slug")}</Label>
            <Input id="proj-slug" value={slug} onChange={(e) => handleSlugChange(e.target.value)} placeholder="xpos" className="font-mono text-base md:text-sm" />
            <p className="text-xs text-muted-foreground">{t("form.slugHint")}</p>
          </div>

          <div className="grid gap-1.5">
            <Label>{t("form.channelType")}</Label>
            <Select value={channelType || "__none__"} onValueChange={(v) => setChannelType(v === "__none__" ? "" : v)}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {CHANNEL_TYPES.map((ct) => (
                  <SelectItem key={ct.value || "__none__"} value={ct.value || "__none__"}>{ct.label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="proj-chatid">{t("form.chatId")}</Label>
            <Input id="proj-chatid" value={chatId} onChange={(e) => setChatId(e.target.value)} placeholder={t("form.chatIdPlaceholder")} className="font-mono text-base md:text-sm" />
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="proj-desc">{t("form.description")}</Label>
            <Textarea
              id="proj-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t("form.descriptionPlaceholder")}
              rows={2}
              className="text-base md:text-sm"
            />
          </div>

          <div className="grid gap-1.5">
            <Label>{t("form.status")}</Label>
            <Select value={status} onValueChange={setStatus}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {STATUS_OPTIONS.map((s) => (
                  <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>{t("form.cancel")}</Button>
          <Button onClick={handleSubmit} disabled={loading}>
            {loading ? t("form.saving") : project ? t("form.update") : t("form.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
