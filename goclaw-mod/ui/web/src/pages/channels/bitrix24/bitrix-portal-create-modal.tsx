import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { BitrixPortalFormStep } from "./bitrix-portal-form-step";
import { BitrixPortalAuthorizeStep } from "./bitrix-portal-authorize-step";
import { useBitrixPortalGetInstallURL } from "./use-bitrix-portals";

type Step = "form" | "authorize";

interface BitrixPortalCreateModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Fires when a portal completes install (whether just created or resumed).
   *  Parent typically closes the modal and auto-selects this portal in the
   *  parent form. */
  onCreated: (portalName: string) => void;
  /** When set, the modal opens directly at the authorize step for this
   *  existing portal (resume flow). Install URL is fetched server-side via
   *  bitrix.portals.get_install_url. Leave undefined for the normal create
   *  flow. */
  resumePortalName?: string;
}

// 2-step modal that owns the full Bitrix24 portal creation lifecycle:
//   step "form"      → BitrixPortalFormStep (POST bitrix.portals.create)
//   step "authorize" → BitrixPortalAuthorizeStep (open install URL + poll)
//
// State (createdName, installUrl) is held here so the orchestration stays
// declarative — child components don't know about each other. Reset on close
// so reopening starts fresh; pending portals from a cancelled flow are
// resumable via the dropdown.
export function BitrixPortalCreateModal({
  open,
  onOpenChange,
  onCreated,
  resumePortalName,
}: BitrixPortalCreateModalProps) {
  const { t } = useTranslation("channels");
  const [step, setStep] = useState<Step>("form");
  const [createdName, setCreatedName] = useState("");
  const [installUrl, setInstallUrl] = useState("");
  const [warning, setWarning] = useState<string | undefined>(undefined);
  const [resumeError, setResumeError] = useState("");
  const getInstallURL = useBitrixPortalGetInstallURL();

  // Reset to step 1 whenever the modal closes — operators expect a fresh
  // start on next open, not stale form values.
  useEffect(() => {
    if (!open) {
      setStep("form");
      setCreatedName("");
      setInstallUrl("");
      setWarning(undefined);
      setResumeError("");
    }
  }, [open]);

  // Resume flow: when the parent opens the modal with resumePortalName set,
  // skip step 1 and fetch the install URL for that existing portal.
  useEffect(() => {
    if (!open || !resumePortalName) return;
    setCreatedName(resumePortalName);
    setStep("authorize");
    setResumeError("");
    getInstallURL
      .mutateAsync(resumePortalName)
      .then((res) => setInstallUrl(res.install_url))
      .catch((err: { code?: string; message?: string }) => {
        setInstallUrl("");
        setResumeError(
          err?.code === "FAILED_PRECONDITION"
            ? t("bitrix24.create.authorize.urlUnknown", {
                defaultValue:
                  "Install URL is unavailable. Open the goclaw UI via your public URL and retry.",
              })
            : err?.message ?? t("common.unknownError", { defaultValue: "Unknown error" }),
        );
      });
    // Intentionally not depending on getInstallURL/t — they're stable enough
    // and including them would re-fire on every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, resumePortalName]);

  const title =
    step === "form"
      ? t("bitrix24.create.title", { defaultValue: "Connect Bitrix24 Portal" })
      : t("bitrix24.create.authorize.title", {
          defaultValue: "Authorize \"{{name}}\"",
          name: createdName,
        });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>

        {step === "form" ? (
          <BitrixPortalFormStep
            onSuccess={(name, url, warn) => {
              setCreatedName(name);
              setInstallUrl(url);
              setWarning(warn);
              setStep("authorize");
            }}
            onCancel={() => onOpenChange(false)}
          />
        ) : (
          <BitrixPortalAuthorizeStep
            portalName={createdName}
            installUrl={installUrl}
            warning={warning ?? resumeError}
            onAuthorized={() => onCreated(createdName)}
            onCancel={() => onOpenChange(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}
