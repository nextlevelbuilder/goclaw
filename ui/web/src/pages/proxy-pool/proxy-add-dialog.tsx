import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

interface ProxyAddDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onAdd: (data: {
    name: string;
    url: string;
    username?: string;
    password?: string;
    geo?: string;
  }) => Promise<void>;
}

export function ProxyAddDialog({ open, onOpenChange, onAdd }: ProxyAddDialogProps) {
  const { t } = useTranslation("proxy-pool");
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [geo, setGeo] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const reset = () => {
    setName("");
    setUrl("");
    setUsername("");
    setPassword("");
    setGeo("");
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || !url.trim()) return;
    setSubmitting(true);
    try {
      await onAdd({
        name: name.trim(),
        url: url.trim(),
        username: username.trim() || undefined,
        password: password.trim() || undefined,
        geo: geo.trim().toUpperCase() || undefined,
      });
      reset();
      onOpenChange(false);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("addDialog.title")}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="proxy-name">{t("addDialog.name")}</Label>
            <Input
              id="proxy-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("addDialog.namePlaceholder")}
              className="text-base md:text-sm"
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="proxy-url">{t("addDialog.url")}</Label>
            <Input
              id="proxy-url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder={t("addDialog.urlPlaceholder")}
              className="text-base md:text-sm"
              required
            />
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="proxy-username">{t("addDialog.username")}</Label>
              <Input
                id="proxy-username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder={t("addDialog.usernamePlaceholder")}
                className="text-base md:text-sm"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="proxy-password">{t("addDialog.password")}</Label>
              <Input
                id="proxy-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={t("addDialog.passwordPlaceholder")}
                className="text-base md:text-sm"
              />
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="proxy-geo">{t("addDialog.geo")}</Label>
            <Input
              id="proxy-geo"
              value={geo}
              onChange={(e) => setGeo(e.target.value)}
              placeholder={t("addDialog.geoPlaceholder")}
              className="text-base md:text-sm"
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t("addDialog.cancel")}
            </Button>
            <Button type="submit" disabled={submitting || !name.trim() || !url.trim()}>
              {t("addDialog.add")}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
