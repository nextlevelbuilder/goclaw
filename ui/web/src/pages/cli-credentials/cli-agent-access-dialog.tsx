import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Bot, ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { CLIAgentCredentialsContent } from "./cli-agent-credentials-content";
import { CliCredentialGrantsContent } from "./cli-credential-grants-content";
import type { SecureCLIBinary } from "./hooks/use-cli-credentials";

export type AgentAccessTab = "credentials" | "grants";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  binary: SecureCLIBinary;
  initialTab?: AgentAccessTab;
}

export function CLIAgentAccessDialog({ open, onOpenChange, binary, initialTab = "credentials" }: Props) {
  const { t } = useTranslation("cli-credentials");
  const [tab, setTab] = useState<AgentAccessTab>(initialTab);

  useEffect(() => {
    if (open) setTab(initialTab);
  }, [open, initialTab, binary.id]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ShieldCheck className="h-4 w-4" />
            {t("agentAccess.title")}
          </DialogTitle>
          <DialogDescription>{t("agentAccess.description", { name: binary.binary_name })}</DialogDescription>
        </DialogHeader>

        <div className="grid grid-cols-2 gap-2">
          <Button
            type="button"
            variant={tab === "credentials" ? "default" : "outline"}
            onClick={() => setTab("credentials")}
            className="gap-2"
          >
            <Bot className="h-3.5 w-3.5" />
            {t("agentAccess.credentialsTab")}
          </Button>
          <Button
            type="button"
            variant={tab === "grants" ? "default" : "outline"}
            onClick={() => setTab("grants")}
            className="gap-2"
          >
            <ShieldCheck className="h-3.5 w-3.5" />
            {t("agentAccess.grantsTab")}
          </Button>
        </div>

        {tab === "credentials" ? (
          <CLIAgentCredentialsContent binary={binary} onClose={() => onOpenChange(false)} />
        ) : (
          <CliCredentialGrantsContent binary={binary} />
        )}
      </DialogContent>
    </Dialog>
  );
}
