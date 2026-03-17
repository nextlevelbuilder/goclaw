import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Copy, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

/** Extract the first emoji (single grapheme cluster) from a string, or empty. */
function extractSingleEmoji(str: string): string {
  // Match first emoji (handles compound emoji like flags, skin tones)
  const match = str.match(/\p{Emoji_Presentation}(\u200D\p{Emoji_Presentation})*/u)
    ?? str.match(/\p{Extended_Pictographic}(\uFE0F?\u200D\p{Extended_Pictographic})*/u);
  return match?.[0] ?? "";
}

interface IdentitySectionProps {
  agentKey: string;
  emoji?: string;
  onEmojiChange?: (v: string) => void;
  displayName: string;
  onDisplayNameChange: (v: string) => void;
  frontmatter: string;
  onFrontmatterChange: (v: string) => void;
  status: string;
  onStatusChange: (v: string) => void;
  isDefault: boolean;
  onIsDefaultChange: (v: boolean) => void;
}

export function IdentitySection({
  agentKey,
  emoji,
  onEmojiChange,
  displayName,
  onDisplayNameChange,
  frontmatter,
  onFrontmatterChange,
  status,
  onStatusChange,
  isDefault,
  onIsDefaultChange,
}: IdentitySectionProps) {
  const { t } = useTranslation("agents");
  const [copied, setCopied] = useState(false);

  const copyAgentKey = async () => {
    await navigator.clipboard.writeText(agentKey);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <section className="space-y-4">
      <h3 className="text-sm font-medium text-muted-foreground">{t("identity.title")}</h3>
      <div className="space-y-4 rounded-lg border p-4">
        <div className="space-y-2">
          <Label>{t("identity.agentKey")}</Label>
          <div className="flex items-center gap-2">
            <Input value={agentKey} disabled className="font-mono text-sm" />
            <Button
              variant="ghost"
              size="icon"
              className="shrink-0"
              onClick={copyAgentKey}
            >
              {copied ? (
                <Check className="h-3.5 w-3.5 text-green-500" />
              ) : (
                <Copy className="h-3.5 w-3.5" />
              )}
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">
            {t("identity.agentKeyHint")}
          </p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="displayName">{t("identity.displayName")}</Label>
          <div className="flex gap-2">
            <Input
              id="emoji"
              value={emoji ?? ""}
              onChange={(e) => {
                const picked = extractSingleEmoji(e.target.value);
                onEmojiChange?.(picked);
              }}
              onFocus={(e) => e.target.select()}
              placeholder="🤖"
              className="w-14 shrink-0 text-center text-base md:text-lg"
              title={t("identity.emojiHint")}
            />
            <Input
              id="displayName"
              value={displayName}
              onChange={(e) => onDisplayNameChange(e.target.value)}
              placeholder={t("identity.displayNamePlaceholder")}
            />
          </div>
          <p className="text-xs text-muted-foreground">
            {t("identity.displayNameHint")}
          </p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="frontmatter">{t("identity.expertiseSummary")}</Label>
          <Input
            id="frontmatter"
            value={frontmatter}
            onChange={(e) => onFrontmatterChange(e.target.value)}
            placeholder={t("identity.expertiseSummaryPlaceholder")}
          />
          <p className="text-xs text-muted-foreground">
            {t("identity.expertiseSummaryHint")}
          </p>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label>{t("identity.status")}</Label>
            <Select value={status} onValueChange={onStatusChange}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="active">{t("identity.active")}</SelectItem>
                <SelectItem value="inactive">{t("identity.inactive")}</SelectItem>
                {status === "summon_failed" && (
                  <SelectItem value="summon_failed" disabled>
                    {t("identity.summonFailed")}
                  </SelectItem>
                )}
              </SelectContent>
            </Select>
          </div>
          <div className="flex items-end pb-2">
            <div className="flex items-center gap-2">
              <Switch checked={isDefault} onCheckedChange={onIsDefaultChange} />
              <Label>{t("identity.defaultAgent")}</Label>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
