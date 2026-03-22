import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Play, MessageCircleQuestion, FileText, LogOut } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { cn } from "@/lib/utils";
import type { PartyMode } from "./hooks/use-party";

interface PartyControlsProps {
  sessionId: string;
  currentMode: PartyMode;
  status: "idle" | "active" | "closed";
  onRunRound: (sessionId: string, mode?: PartyMode) => Promise<void>;
  onAskQuestion: (sessionId: string, text: string) => Promise<void>;
  onGetSummary: (sessionId: string) => Promise<void>;
  onExit: (sessionId: string) => Promise<void>;
}

const MODE_OPTIONS: { value: PartyMode; labelKey: string }[] = [
  { value: "standard", labelKey: "mode.standard" },
  { value: "deep", labelKey: "mode.deep" },
  { value: "token_ring", labelKey: "mode.token_ring" },
];

export function PartyControls({
  sessionId,
  currentMode,
  status,
  onRunRound,
  onAskQuestion,
  onGetSummary,
  onExit,
}: PartyControlsProps) {
  const { t } = useTranslation("party");
  const [selectedMode, setSelectedMode] = useState<PartyMode>(currentMode);
  const [questionOpen, setQuestionOpen] = useState(false);
  const [questionText, setQuestionText] = useState("");
  const [exitOpen, setExitOpen] = useState(false);
  const [runningAction, setRunningAction] = useState<string | null>(null);

  const isClosed = status === "closed";

  const handleRunRound = async () => {
    setRunningAction("round");
    try {
      await onRunRound(sessionId, selectedMode);
    } finally {
      setRunningAction(null);
    }
  };

  const handleQuestion = async () => {
    if (!questionText.trim()) return;
    setRunningAction("question");
    try {
      await onAskQuestion(sessionId, questionText.trim());
      setQuestionText("");
      setQuestionOpen(false);
    } finally {
      setRunningAction(null);
    }
  };

  const handleSummary = async () => {
    setRunningAction("summary");
    try {
      await onGetSummary(sessionId);
    } finally {
      setRunningAction(null);
    }
  };

  const handleExit = async () => {
    setRunningAction("exit");
    try {
      await onExit(sessionId);
      setExitOpen(false);
    } finally {
      setRunningAction(null);
    }
  };

  return (
    <div className="border-t bg-background px-4 py-3">
      <div className="flex flex-wrap items-center gap-2">
        {/* Mode toggle */}
        <div className="flex rounded-md border bg-muted/30">
          {MODE_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              data-testid={`mode-${opt.value}`}
              type="button"
              disabled={isClosed}
              onClick={() => setSelectedMode(opt.value)}
              className={cn(
                "px-2.5 py-1 text-xs font-medium transition-colors cursor-pointer first:rounded-l-md last:rounded-r-md",
                selectedMode === opt.value
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground",
                isClosed && "opacity-50 cursor-not-allowed",
              )}
            >
              {t(opt.labelKey)}
            </button>
          ))}
        </div>

        <div className="h-5 w-px bg-border" />

        {/* Continue [C] */}
        <Button
          size="sm"
          onClick={handleRunRound}
          disabled={isClosed || runningAction !== null}
          className="gap-1"
        >
          <Play className="h-3.5 w-3.5" />
          {t("controls.continue")}
        </Button>

        {/* Question [Q] */}
        {questionOpen ? (
          <div className="flex items-center gap-1.5">
            <Input
              value={questionText}
              onChange={(e) => setQuestionText(e.target.value)}
              placeholder="Ask a question..."
              className="h-8 w-48 text-xs"
              onKeyDown={(e) => {
                if (e.key === "Enter") handleQuestion();
                if (e.key === "Escape") setQuestionOpen(false);
              }}
              autoFocus
            />
            <Button
              size="sm"
              variant="secondary"
              onClick={handleQuestion}
              disabled={!questionText.trim() || runningAction !== null}
            >
              Send
            </Button>
          </div>
        ) : (
          <Button
            size="sm"
            variant="outline"
            onClick={() => setQuestionOpen(true)}
            disabled={isClosed || runningAction !== null}
            className="gap-1"
          >
            <MessageCircleQuestion className="h-3.5 w-3.5" />
            {t("controls.question")}
          </Button>
        )}

        {/* Summary [D] */}
        <Button
          size="sm"
          variant="outline"
          onClick={handleSummary}
          disabled={isClosed || runningAction !== null}
          className="gap-1"
        >
          <FileText className="h-3.5 w-3.5" />
          {t("controls.summary")}
        </Button>

        {/* Spacer */}
        <div className="flex-1" />

        {/* Exit [E] */}
        <Button
          size="sm"
          variant="destructive"
          onClick={() => setExitOpen(true)}
          disabled={isClosed || runningAction !== null}
          className="gap-1"
        >
          <LogOut className="h-3.5 w-3.5" />
          {t("controls.exit")}
        </Button>
      </div>

      <ConfirmDialog
        open={exitOpen}
        onOpenChange={setExitOpen}
        title={t("controls.exit")}
        description={t("exitConfirm")}
        variant="destructive"
        onConfirm={handleExit}
        loading={runningAction === "exit"}
      />
    </div>
  );
}
