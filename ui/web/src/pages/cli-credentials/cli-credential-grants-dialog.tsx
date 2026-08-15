import { useTranslation } from "react-i18next";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { CliCredentialGrantsContent } from "./cli-credential-grants-content";
import type { SecureCLIBinary } from "./hooks/use-cli-credentials";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  binary: SecureCLIBinary;
}

/** Dialog for managing per-agent grants on a CLI credential. */
export function CliCredentialGrantsDialog({ open, onOpenChange, binary }: Props) {
  const { t } = useTranslation("cli-credentials");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("grants.title", { name: binary.binary_name })}</DialogTitle>
        </DialogHeader>
        <CliCredentialGrantsContent binary={binary} />
      </DialogContent>
    </Dialog>
  );
}
