import { useTranslation } from "react-i18next";
import { Bot } from "lucide-react";
import {
  Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { CLIAgentCredentialsContent } from "./cli-agent-credentials-content";
import type { SecureCLIBinary } from "./hooks/use-cli-credentials";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  binary: SecureCLIBinary;
}

export function CLIAgentCredentialsDialog({ open, onOpenChange, binary }: Props) {
  const { t } = useTranslation("cli-credentials");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Bot className="h-4 w-4" />{t("agentCredentials.title")}</DialogTitle>
          <DialogDescription>{t("agentCredentials.description", { name: binary.binary_name })}</DialogDescription>
        </DialogHeader>
        <CLIAgentCredentialsContent binary={binary} onClose={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}
