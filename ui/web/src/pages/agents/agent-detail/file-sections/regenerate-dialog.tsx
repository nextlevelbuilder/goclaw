import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Loader2, Sparkles, CheckCircle2, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { useWsEvent } from "@/hooks/use-ws-event";

type Phase = "idle" | "waiting" | "completed" | "failed";

interface RegenerateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agentId: string;
  onRegenerate: (prompt: string) => Promise<void>;
  onCompleted?: () => void;
}

export function RegenerateDialog({
  open,
  onOpenChange,
  agentId,
  onRegenerate,
  onCompleted,
}: RegenerateDialogProps) {
  const { t } = useTranslation("agents");
  const [prompt, setPrompt] = useState("");
  const [phase, setPhase] = useState<Phase>("idle");
  const [errorMsg, setErrorMsg] = useState("");
  const closeTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  // Cleanup auto-close timer on unmount
  useEffect(() => () => { clearTimeout(closeTimerRef.current); }, []);

  // Reset state when dialog opens
  useEffect(() => {
    if (open) {
      setPhase("idle");
      setErrorMsg("");
    }
  }, [open]);

  // Listen for agent.summoning events to track regeneration progress
  const handleSummoningEvent = useCallback(
    (payload: unknown) => {
      const data = payload as Record<string, string>;
      if (data.agent_id !== agentId) return;

      if (data.type === "completed") {
        setPhase("completed");
        onCompleted?.();
        // Auto-close after short delay
        closeTimerRef.current = setTimeout(() => {
          onOpenChange(false);
          setPrompt("");
        }, 800);
      }
      if (data.type === "failed") {
        setPhase("failed");
        setErrorMsg(data.error || t("fileEditor.regenerateFailed"));
      }
    },
    [agentId, onCompleted, onOpenChange, t],
  );

  useWsEvent("agent.summoning", handleSummoningEvent);

  const handleSubmit = async () => {
    if (!prompt.trim()) return;
    setPhase("waiting");
    setErrorMsg("");
    try {
      await onRegenerate(prompt.trim());
      // Stay open — waiting for WS event
    } catch {
      setPhase("failed");
      setErrorMsg(t("fileEditor.regenerateFailed"));
    }
  };

  const busy = phase === "waiting";

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!busy) onOpenChange(v); }}>
      <DialogContent className="sm:max-w-lg" onInteractOutside={(e) => { if (busy) e.preventDefault(); }}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Sparkles className="h-4 w-4" />
            {t("fileEditor.editWithAi")}
          </DialogTitle>
        </DialogHeader>

        {phase === "idle" || phase === "failed" ? (
          <>
            <div className="space-y-3 py-2">
              <p className="text-sm text-muted-foreground">
                {t("fileEditor.editAiDescription")}
              </p>
              <Textarea
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                placeholder={t("fileEditor.editAiPlaceholder")}
                className="min-h-[100px] max-h-[300px] resize-none"
              />
              {phase === "failed" && errorMsg && (
                <div className="flex items-center gap-2 text-sm text-destructive">
                  <XCircle className="h-4 w-4 shrink-0" />
                  <span>{errorMsg}</span>
                </div>
              )}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                {t("fileEditor.cancel")}
              </Button>
              <Button
                onClick={handleSubmit}
                disabled={!prompt.trim()}
                className="gap-1.5"
              >
                <Sparkles className="h-3.5 w-3.5" />
                {phase === "failed" ? t("fileEditor.retry") : t("fileEditor.regenerate")}
              </Button>
            </DialogFooter>
          </>
        ) : phase === "waiting" ? (
          <div className="flex flex-col items-center gap-3 py-8">
            <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
            <p className="text-sm text-muted-foreground">{t("fileEditor.regenerating")}</p>
          </div>
        ) : (
          <div className="flex flex-col items-center gap-3 py-8">
            <CheckCircle2 className="h-8 w-8 text-emerald-500" />
            <p className="text-sm font-medium text-emerald-600 dark:text-emerald-400">
              {t("fileEditor.regenerateCompleted")}
            </p>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
