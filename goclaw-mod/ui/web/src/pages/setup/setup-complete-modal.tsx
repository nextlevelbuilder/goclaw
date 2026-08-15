import { motion } from "framer-motion";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";

interface SetupCompleteModalProps {
  open: boolean;
  onGoToDashboard: () => void;
}

export function SetupCompleteModal({ open, onGoToDashboard }: SetupCompleteModalProps) {
  const { t } = useTranslation("setup");
  return (
    <Dialog open={open} onOpenChange={() => {/* blocked */}}>
      <DialogContent className="sm:max-w-md" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle className="text-center">{t("complete.title")}</DialogTitle>
        </DialogHeader>

        <div className="flex flex-col items-center gap-6 py-6">
          {/* Animated checkmark */}
          <motion.div
            className="flex h-20 w-20 items-center justify-center rounded-full bg-emerald-100 text-4xl dark:bg-emerald-900/30"
            initial={{ scale: 0.5, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            transition={{ type: "spring", stiffness: 200, damping: 15 }}
          >
            <motion.span
              initial={{ scale: 0 }}
              animate={{ scale: 1 }}
              transition={{ delay: 0.2, type: "spring", stiffness: 300, damping: 20 }}
            >
              {"\u2705"}
            </motion.span>
          </motion.div>

          <div className="space-y-2 text-center">
            <p className="text-sm font-medium text-foreground">
              {t("complete.ready")}
            </p>
            <p className="text-xs text-muted-foreground">
              {t("complete.description")}
            </p>
          </div>

          <Button onClick={onGoToDashboard} className="w-full sm:w-auto px-8">
            {t("complete.goToDashboard")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
