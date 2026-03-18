import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Bot, Check, Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";

/** Extract the first emoji grapheme cluster from a string, or return empty. */
function extractSingleEmoji(str: string): string {
  const match = str.match(/\p{Emoji_Presentation}(\u200D\p{Emoji_Presentation})*/u)
    ?? str.match(/\p{Extended_Pictographic}(\uFE0F?\u200D\p{Extended_Pictographic})*/u);
  return match?.[0] ?? "";
}

interface PersonalitySectionProps {
  agentKey: string;
  emoji: string;
  onEmojiChange: (v: string) => void;
  displayName: string;
  onDisplayNameChange: (v: string) => void;
  frontmatter: string;
  onFrontmatterChange: (v: string) => void;
  status: string;
  onStatusChange: (v: string) => void;
  isDefault: boolean;
  onIsDefaultChange: (v: boolean) => void;
}

export function PersonalitySection({
  agentKey, emoji, onEmojiChange, displayName, onDisplayNameChange,
  frontmatter, onFrontmatterChange, status, onStatusChange, isDefault, onIsDefaultChange,
}: PersonalitySectionProps) {
  const { t } = useTranslation("agents");
  const [copied, setCopied] = useState(false);
  const [emojiEditing, setEmojiEditing] = useState(false);

  const copyAgentKey = async () => {
    await navigator.clipboard.writeText(agentKey);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <section className="space-y-4 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <h3 className="text-sm font-medium">{t("detail.personality")}</h3>

      {/* 2-column: emoji left, fields right */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-[auto_1fr]">
        {/* Emoji large preview */}
        <div className="flex flex-col items-center gap-1.5">
          <button
            type="button"
            onClick={() => setEmojiEditing(true)}
            className="flex h-12 w-12 items-center justify-center rounded-xl bg-primary/10 text-primary hover:bg-primary/20 transition-colors"
            title={t("identity.emojiHint")}
          >
            {emoji
              ? <span className="text-2xl leading-none">{emoji}</span>
              : <Bot className="h-6 w-6 text-muted-foreground" />}
          </button>
          {emojiEditing ? (
            <Input
              autoFocus
              value={emoji}
              onChange={(e) => onEmojiChange(extractSingleEmoji(e.target.value))}
              onBlur={() => setEmojiEditing(false)}
              placeholder="🤖"
              className="w-14 text-center text-base md:text-sm"
            />
          ) : (
            <span className="text-[10px] text-muted-foreground">{t("identity.emoji")}</span>
          )}
        </div>

        {/* Fields */}
        <div className="min-w-0 space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="displayName" className="text-xs">{t("identity.displayName")}</Label>
            <Input
              id="displayName"
              value={displayName}
              onChange={(e) => onDisplayNameChange(e.target.value)}
              placeholder={t("identity.displayNamePlaceholder")}
              className="text-base font-medium md:text-sm"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="frontmatter" className="text-xs">{t("identity.expertiseSummary")}</Label>
            <Textarea
              id="frontmatter"
              value={frontmatter}
              onChange={(e) => onFrontmatterChange(e.target.value)}
              placeholder={t("identity.expertiseSummaryPlaceholder")}
              rows={3}
              className="text-base resize-none md:text-sm"
            />
            {frontmatter && (
              <p className="text-xs text-muted-foreground italic truncate">
                {t("detail.llmSeesAs")}: "{frontmatter}"
              </p>
            )}
          </div>
        </div>
      </div>

      {/* Status + Default on same row */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label className="text-xs">{t("identity.status")}</Label>
          <Select value={status} onValueChange={onStatusChange}>
            <SelectTrigger className="text-base md:text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="active">{t("identity.active")}</SelectItem>
              <SelectItem value="inactive">{t("identity.inactive")}</SelectItem>
              {status === "summon_failed" && (
                <SelectItem value="summon_failed" disabled>{t("identity.summonFailed")}</SelectItem>
              )}
            </SelectContent>
          </Select>
        </div>
        <div className="flex items-end pb-1">
          <div className="flex items-center gap-2">
            <Switch id="isDefault" checked={isDefault} onCheckedChange={onIsDefaultChange} />
            <Label htmlFor="isDefault" className="text-sm font-normal">{t("identity.defaultAgent")}</Label>
          </div>
        </div>
      </div>

      {/* Agent Key read-only */}
      <div className="space-y-1.5">
        <Label className="text-xs text-muted-foreground">{t("identity.agentKey")}</Label>
        <div className="flex items-center gap-2">
          <span className="flex-1 rounded-md border bg-muted/50 px-3 py-1.5 font-mono text-xs text-muted-foreground truncate">
            {agentKey}
          </span>
          <Button variant="ghost" size="icon" className="shrink-0 size-7" onClick={copyAgentKey}>
            {copied
              ? <Check className="h-3 w-3 text-green-500" />
              : <Copy className="h-3 w-3" />}
          </Button>
        </div>
      </div>
    </section>
  );
}
