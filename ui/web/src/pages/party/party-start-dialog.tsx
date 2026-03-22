import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

const TEAM_PRESETS = [
  "payment_feature",
  "security_review",
  "sprint_planning",
  "architecture_decision",
  "ux_review",
  "incident_response",
] as const;

const ALL_PERSONAS = [
  { key: "tony-stark-persona", emoji: "\ud83d\udcca", label: "Product Manager" },
  { key: "morpheus-persona", emoji: "\ud83d\udd27", label: "Tech Lead" },
  { key: "batman-persona", emoji: "\ud83d\udd12", label: "Security Analyst" },
  { key: "columbo-persona", emoji: "\ud83e\uddea", label: "QA Engineer" },
  { key: "scotty-persona", emoji: "\u2699\ufe0f", label: "DevOps Engineer" },
  { key: "edna-mode-persona", emoji: "\ud83c\udfa8", label: "UX Designer" },
  { key: "spider-man-persona", emoji: "\ud83c\udf10", label: "Frontend Dev" },
  { key: "ethan-hunt-persona", emoji: "\ud83d\udcf1", label: "Mobile Dev" },
  { key: "sherlock-persona", emoji: "\ud83d\udcbc", label: "Business Analyst" },
  { key: "judge-dredd-persona", emoji: "\ud83d\udccb", label: "Compliance Officer" },
  { key: "gandalf-persona", emoji: "\ud83c\udfc3", label: "Scrum Master" },
  { key: "neo-persona", emoji: "\ud83c\udfd7\ufe0f", label: "Architect" },
  { key: "spock-persona", emoji: "\ud83d\uddc4\ufe0f", label: "DBA" },
  { key: "nick-fury-persona", emoji: "\ud83d\udc54", label: "Executive" },
] as const;

interface PartyStartDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onStart: (topic: string, teamPreset?: string, personaKeys?: string[]) => Promise<void>;
}

export function PartyStartDialog({ open, onOpenChange, onStart }: PartyStartDialogProps) {
  const { t } = useTranslation("party");
  const [topic, setTopic] = useState("");
  const [selectedPreset, setSelectedPreset] = useState<string | null>(null);
  const [isCustom, setIsCustom] = useState(false);
  const [selectedPersonas, setSelectedPersonas] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(false);

  const togglePersona = (key: string) => {
    setSelectedPersonas((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  };

  const handleSelectPreset = (preset: string) => {
    setSelectedPreset(preset);
    setIsCustom(false);
    setSelectedPersonas(new Set());
  };

  const handleSelectCustom = () => {
    setSelectedPreset(null);
    setIsCustom(true);
  };

  const canStart = topic.trim().length > 0 && (selectedPreset || (isCustom && selectedPersonas.size >= 2));

  const handleStart = async () => {
    if (!canStart) return;
    setLoading(true);
    try {
      await onStart(
        topic.trim(),
        selectedPreset ?? undefined,
        isCustom ? Array.from(selectedPersonas) : undefined,
      );
      onOpenChange(false);
      setTopic("");
      setSelectedPreset(null);
      setIsCustom(false);
      setSelectedPersonas(new Set());
    } catch {
      // error handled upstream
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] flex flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("newParty")}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4 py-4 px-0.5 -mx-0.5 overflow-y-auto min-h-0">
          {/* Topic input */}
          <div className="space-y-2">
            <Label htmlFor="partyTopic">{t("topic")}</Label>
            <Input
              id="partyTopic"
              value={topic}
              onChange={(e) => setTopic(e.target.value)}
              placeholder="e.g., Design payment reconciliation service..."
            />
          </div>

          {/* Team presets */}
          <div className="space-y-2">
            <Label>{t("selectTeam")}</Label>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
              {TEAM_PRESETS.map((preset) => (
                <button
                  key={preset}
                  type="button"
                  onClick={() => handleSelectPreset(preset)}
                  className={cn(
                    "rounded-md border px-3 py-2 text-sm text-left transition-colors cursor-pointer",
                    selectedPreset === preset
                      ? "border-primary bg-primary/10 text-primary"
                      : "border-border hover:border-primary/50 hover:bg-muted/50",
                  )}
                >
                  {t(`presets.${preset}`)}
                </button>
              ))}
            </div>
          </div>

          {/* Custom team option */}
          <div className="space-y-2">
            <button
              type="button"
              onClick={handleSelectCustom}
              className={cn(
                "w-full rounded-md border px-3 py-2 text-sm text-left transition-colors cursor-pointer",
                isCustom
                  ? "border-primary bg-primary/10 text-primary"
                  : "border-border hover:border-primary/50 hover:bg-muted/50",
              )}
            >
              {t("customTeam")}
            </button>

            {isCustom && (
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
                {ALL_PERSONAS.map((persona) => (
                  <button
                    key={persona.key}
                    type="button"
                    onClick={() => togglePersona(persona.key)}
                    className={cn(
                      "flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs transition-colors cursor-pointer",
                      selectedPersonas.has(persona.key)
                        ? "border-primary bg-primary/10 text-primary"
                        : "border-border hover:border-primary/50 hover:bg-muted/50",
                    )}
                  >
                    <span>{persona.emoji}</span>
                    <span className="truncate">{persona.label}</span>
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            Cancel
          </Button>
          <Button onClick={handleStart} disabled={!canStart || loading}>
            {loading ? "..." : t("start")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
