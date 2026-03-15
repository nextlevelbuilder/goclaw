import { useCallback, useEffect, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { useWsEvent } from "@/hooks/use-ws-event";

interface SummoningModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agentId: string;
  agentName: string;
  onCompleted: () => void;
  onResummon: (agentId: string) => Promise<void>;
  hideClose?: boolean;
  onContinue?: () => void;
}

const SUMMONING_FILES = [
  { name: "SOUL.md", required: true },
  { name: "IDENTITY.md", required: true },
  { name: "USER_PREDEFINED.md", required: false },
];

export function SummoningModal({
  open,
  onOpenChange,
  agentId,
  agentName,
  onCompleted,
  onResummon,
  hideClose = false,
  onContinue,
}: SummoningModalProps) {
  const { t } = useTranslation("agents");
  const [generatedFiles, setGeneratedFiles] = useState<string[]>([]);
  const [status, setStatus] = useState<"summoning" | "completed" | "failed">("summoning");
  const [errorMsg, setErrorMsg] = useState("");
  const [retrying, setRetrying] = useState(false);

  // Reset state when modal opens
  useEffect(() => {
    if (open) {
      setGeneratedFiles([]);
      setStatus("summoning");
      setErrorMsg("");
    }
  }, [open]);


  const handleSummoningEvent = useCallback(
    (payload: unknown) => {
      const data = payload as Record<string, string>;
      if (data.agent_id !== agentId) return;

      if (data.type === "file_generated" && data.file) {
        const fileName = data.file;
        setGeneratedFiles((prev) =>
          prev.includes(fileName) ? prev : [...prev, fileName],
        );
      }
      if (data.type === "completed") {
        // Mark required files as done (safety net); optional files only if actually generated
        const required = SUMMONING_FILES.filter((f) => f.required).map((f) => f.name);
        setGeneratedFiles((prev) => [...new Set([...prev, ...required])]);
        setStatus("completed");
        onCompleted();
      }
      if (data.type === "failed") {
        setStatus("failed");
        setErrorMsg(data.error || t("summoning.failed"));
        onCompleted();
      }
    },
    [agentId, onCompleted, t],
  );

  useWsEvent("agent.summoning", handleSummoningEvent);

  const handleRetry = async () => {
    setRetrying(true);
    try {
      await onResummon(agentId);
      setGeneratedFiles([]);
      setStatus("summoning");
      setErrorMsg("");
    } catch {
      // stay in failed state
    } finally {
      setRetrying(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md" showCloseButton={!hideClose} overlayTransparent onInteractOutside={(e) => { if (hideClose || status === "summoning") e.preventDefault(); }}>
        <DialogHeader>
          <DialogTitle className="text-center">
            {status === "completed"
              ? t("summoning.completed")
              : status === "failed"
                ? t("summoning.failed")
                : t("summoning.title")}
          </DialogTitle>
        </DialogHeader>

        <div className="flex flex-col items-center gap-6 py-6">
          {/* Animated orb */}
          <div className="relative flex h-24 w-24 items-center justify-center">
            {status === "summoning" && (
              <>
                <motion.div
                  className="absolute inset-0 rounded-full bg-violet-500/20"
                  animate={{ scale: [1, 1.3, 1], opacity: [0.3, 0.1, 0.3] }}
                  transition={{ duration: 2, repeat: Infinity, ease: "easeInOut" }}
                />
                <motion.div
                  className="absolute inset-2 rounded-full bg-violet-500/30"
                  animate={{ scale: [1, 1.15, 1], opacity: [0.5, 0.2, 0.5] }}
                  transition={{ duration: 1.5, repeat: Infinity, ease: "easeInOut", delay: 0.3 }}
                />
              </>
            )}
            <motion.div
              className={`relative z-10 flex h-16 w-16 items-center justify-center rounded-full text-3xl ${
                status === "completed"
                  ? "bg-emerald-100 dark:bg-emerald-900/30"
                  : status === "failed"
                    ? "bg-red-100 dark:bg-red-900/30"
                    : "bg-violet-100 dark:bg-violet-900/30"
              }`}
              animate={
                status === "summoning"
                  ? { rotate: [0, 5, -5, 0] }
                  : status === "completed"
                    ? { scale: [1, 1.2, 1] }
                    : {}
              }
              transition={
                status === "summoning"
                  ? { duration: 3, repeat: Infinity, ease: "easeInOut" }
                  : { duration: 0.5 }
              }
            >
              {status === "completed" ? "\u2728" : status === "failed" ? "\u{1F4A8}" : "\u{1FA84}"}
            </motion.div>
          </div>

          {/* Agent name */}
          <p className="text-sm text-foreground">
            {status === "completed" ? (
              <span className="font-medium text-emerald-600 dark:text-emerald-400">
                {t("summoning.agentReady", { name: agentName })}
              </span>
            ) : status === "failed" ? (
              <span className="font-medium text-red-600 dark:text-red-400">
                {errorMsg || t("summoning.failed")}
              </span>
            ) : (
              <>{t("summoning.weavingSoul")} <span className="font-semibold text-foreground">{agentName}</span>...</>
            )}
          </p>

          {/* File progress */}
          <div className="w-full space-y-2">
            <AnimatePresence>
              {SUMMONING_FILES.map((file, i) => {
                const done = generatedFiles.includes(file.name);
                return (
                  <motion.div
                    key={file.name}
                    initial={{ opacity: 1 }}
                    animate={{ opacity: 1 }}
                    transition={{ duration: 0.3 }}
                    className="flex items-center gap-3 rounded-md px-3 py-1.5"
                  >
                    <motion.div
                      className={`flex h-5 w-5 items-center justify-center rounded-full text-xs ${
                        done
                          ? "bg-violet-100 text-violet-600 dark:bg-violet-900/40 dark:text-violet-400"
                          : "bg-muted text-muted-foreground"
                      }`}
                      animate={done ? { scale: [0.8, 1.2, 1] } : {}}
                      transition={{ duration: 0.3 }}
                    >
                      {done ? "\u2713" : i + 1}
                    </motion.div>
                    <div className="flex-1">
                      <span className={`text-sm text-foreground ${done ? "font-medium" : ""}`}>
                        {file.name}
                      </span>
                      <span className="ml-2 text-xs text-muted-foreground">{t(`summoning.fileLabel${file.name.replace(".md", "")}`)}</span>
                    </div>
                    {done && (
                      <motion.span
                        initial={{ opacity: 0, scale: 0.5 }}
                        animate={{ opacity: 1, scale: 1 }}
                        className="text-xs text-violet-600 dark:text-violet-400"
                      >
                        {t("summoning.done")}
                      </motion.span>
                    )}
                  </motion.div>
                );
              })}
            </AnimatePresence>
          </div>

          {status === "summoning" && (
            <p className="text-center text-xs text-muted-foreground">
              {t("summoning.wait")}
            </p>
          )}

          {status === "completed" && onContinue && (
            <Button size="sm" onClick={onContinue}>
              {t("summoning.continue")}
            </Button>
          )}

          {status === "failed" && (
            <Button variant="outline" size="sm" onClick={handleRetry} disabled={retrying}>
              {retrying ? t("summoning.retrying") : t("summoning.retry")}
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
