import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { CreateWorkstationParams } from "./hooks/use-workstations";

interface WorkstationCreateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (params: CreateWorkstationParams) => Promise<void>;
}

type BackendType = "ssh" | "docker";

export function WorkstationCreateDialog({
  open,
  onOpenChange,
  onCreate,
}: WorkstationCreateDialogProps) {
  const { t } = useTranslation("workstations");

  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [backend, setBackend] = useState<BackendType>("ssh");
  // SSH fields
  const [host, setHost] = useState("");
  const [port, setPort] = useState("22");
  const [user, setUser] = useState("");
  const [identityFile, setIdentityFile] = useState("");
  // Docker fields
  const [container, setContainer] = useState("");
  const [dockerHost, setDockerHost] = useState("");

  const [submitting, setSubmitting] = useState(false);
  const [fieldError, setFieldError] = useState<string | null>(null);

  function resetForm() {
    setName("");
    setKey("");
    setBackend("ssh");
    setHost("");
    setPort("22");
    setUser("");
    setIdentityFile("");
    setContainer("");
    setDockerHost("");
    setFieldError(null);
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim() || !key.trim()) return;

    // Build backend metadata
    let metadata: Record<string, unknown>;
    if (backend === "ssh") {
      if (!host.trim() || !user.trim()) {
        setFieldError("Host and SSH user are required for SSH backend.");
        return;
      }
      metadata = {
        host: host.trim(),
        port: parseInt(port, 10) || 22,
        user: user.trim(),
        ...(identityFile.trim() ? { identity_file: identityFile.trim() } : {}),
      };
    } else {
      if (!container.trim()) {
        setFieldError("Container name is required for Docker backend.");
        return;
      }
      metadata = {
        container: container.trim(),
        ...(dockerHost.trim() ? { docker_host: dockerHost.trim() } : {}),
      };
    }

    setFieldError(null);
    setSubmitting(true);
    try {
      await onCreate({ workstation_key: key.trim(), name: name.trim(), backend_type: backend, metadata });
      resetForm();
      onOpenChange(false);
    } catch (err) {
      setFieldError(err instanceof Error ? err.message : "Failed to create workstation.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!submitting) { resetForm(); onOpenChange(v); } }}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>{t("createDialog.title")}</DialogTitle>
            <DialogDescription>{t("createDialog.description")}</DialogDescription>
          </DialogHeader>

          <div className="mt-4 space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="ws-name">{t("createDialog.nameLabel")}</Label>
              <Input
                id="ws-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t("createDialog.namePlaceholder")}
                required
                className="text-base md:text-sm"
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="ws-key">{t("createDialog.keyLabel")}</Label>
              <Input
                id="ws-key"
                value={key}
                onChange={(e) => setKey(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))}
                placeholder={t("createDialog.keyPlaceholder")}
                required
                className="text-base md:text-sm"
              />
              <p className="text-xs text-muted-foreground">{t("createDialog.keyHint")}</p>
            </div>

            <div className="space-y-1.5">
              <Label>{t("createDialog.backendLabel")}</Label>
              <Select value={backend} onValueChange={(v) => setBackend(v as BackendType)}>
                <SelectTrigger className="text-base md:text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="ssh">{t("createDialog.sshOption")}</SelectItem>
                  <SelectItem value="docker">{t("createDialog.dockerOption")}</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {backend === "ssh" && (
              <>
                <div className="grid grid-cols-3 gap-3">
                  <div className="col-span-2 space-y-1.5">
                    <Label htmlFor="ws-host">{t("createDialog.hostLabel")}</Label>
                    <Input
                      id="ws-host"
                      value={host}
                      onChange={(e) => setHost(e.target.value)}
                      placeholder={t("createDialog.hostPlaceholder")}
                      className="text-base md:text-sm"
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="ws-port">{t("createDialog.portLabel")}</Label>
                    <Input
                      id="ws-port"
                      type="number"
                      min={1}
                      max={65535}
                      value={port}
                      onChange={(e) => setPort(e.target.value)}
                      className="text-base md:text-sm"
                    />
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="ws-user">{t("createDialog.userLabel")}</Label>
                  <Input
                    id="ws-user"
                    value={user}
                    onChange={(e) => setUser(e.target.value)}
                    placeholder={t("createDialog.userPlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="ws-identity">{t("createDialog.identityFileLabel")}</Label>
                  <Input
                    id="ws-identity"
                    value={identityFile}
                    onChange={(e) => setIdentityFile(e.target.value)}
                    placeholder={t("createDialog.identityFilePlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
              </>
            )}

            {backend === "docker" && (
              <>
                <div className="space-y-1.5">
                  <Label htmlFor="ws-container">{t("createDialog.containerLabel")}</Label>
                  <Input
                    id="ws-container"
                    value={container}
                    onChange={(e) => setContainer(e.target.value)}
                    placeholder={t("createDialog.containerPlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="ws-docker-host">{t("createDialog.dockerHostLabel")}</Label>
                  <Input
                    id="ws-docker-host"
                    value={dockerHost}
                    onChange={(e) => setDockerHost(e.target.value)}
                    placeholder={t("createDialog.dockerHostPlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
              </>
            )}

            {fieldError && (
              <p className="text-sm text-destructive">{fieldError}</p>
            )}
          </div>

          <DialogFooter className="mt-6">
            <Button type="button" variant="outline" onClick={() => { resetForm(); onOpenChange(false); }} disabled={submitting}>
              {t("createDialog.cancel")}
            </Button>
            <Button type="submit" disabled={submitting || !name.trim() || !key.trim()}>
              {t("createDialog.create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
