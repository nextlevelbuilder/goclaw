import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { DialogFooter } from "@/components/ui/dialog";
import type { WizardAuthStepProps } from "../channel-wizard-registry";
import { useZaloOAConnect } from "./use-zalo-oa-connect";
import { ZaloOAConnectBody } from "./zalo-oa-connect-body";

// Paste-code consent step rendered inside the create wizard dialog after
// the channel_instance row has been persisted. Mounts active → hook fetches
// consent URL immediately so the user sees the Authorize button without
// an extra click.
export function ZaloOAAuthStep({ instanceId, onComplete, onSkip }: WizardAuthStepProps) {
  const { t } = useTranslation("channels");
  const flow = useZaloOAConnect(instanceId, true /* always active in wizard */, onComplete);

  const canSubmit = flow.code.trim() !== "" && flow.state !== "" && !flow.submitting && !flow.done;

  return (
    <>
      <ZaloOAConnectBody flow={flow} />
      <DialogFooter>
        <Button variant="outline" onClick={onSkip} disabled={flow.submitting}>
          {t("zaloOa.cancel")}
        </Button>
        <Button onClick={flow.handleSubmit} disabled={!canSubmit}>
          {flow.submitting ? t("zaloOa.connecting") : t("zaloOa.connect")}
        </Button>
      </DialogFooter>
    </>
  );
}
