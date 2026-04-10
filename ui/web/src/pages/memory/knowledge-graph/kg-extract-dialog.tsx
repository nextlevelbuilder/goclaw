import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { useTranslation } from "react-i18next";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";

interface KGExtractDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onExtract: (text: string, provider: string, model: string) => Promise<unknown>;
}

export function KGExtractDialog({ open, onOpenChange, onExtract }: KGExtractDialogProps) {
  const { t } = useTranslation("memory");
  const [text, setText] = useState("");
  const [provider, setProvider] = useState("");
  const [model, setModel] = useState("");
  const [loading, setLoading] = useState(false);

  const handleSubmit = async () => {
    if (!text.trim() || !provider || !model) return;
    setLoading(true);
    try {
      await onExtract(text.trim(), provider, model);
      setText("");
      onOpenChange(false);
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !loading && onOpenChange(v)}>
      <DialogContent aria-describedby={undefined} className="max-w-2xl max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{t("kg.extractDialog.title")}</DialogTitle>
        </DialogHeader>

        <div className="flex-1 min-h-0 overflow-y-auto py-2 -mx-4 px-4 sm:-mx-6 sm:px-6 space-y-4">
          <ProviderModelSelect
            provider={provider}
            onProviderChange={setProvider}
            model={model}
            onModelChange={setModel}
            providerLabel={t("kg.extractDialog.providerLabel")}
            modelLabel={t("kg.extractDialog.modelLabel")}
          />

          <div className="grid gap-1.5">
            <label className="text-xs font-medium">{t("kg.extractDialog.textLabel")}</label>
            <Textarea
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder={t("kg.extractDialog.textPlaceholder")}
              rows={10}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            {t("kg.extractDialog.cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={loading || !text.trim() || !provider || !model}>
            {loading ? t("kg.extractDialog.extracting") : t("kg.extractDialog.extract")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
