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
import { ProviderModelSelect } from "@/components/shared/provider-model-select";

interface KGExtractDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onExtract: (text: string, provider: string, model: string) => Promise<unknown>;
}

export function KGExtractDialog({ open, onOpenChange, onExtract }: KGExtractDialogProps) {
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
      <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>Extract Entities from Text</DialogTitle>
        </DialogHeader>

        <div className="flex-1 min-h-0 overflow-y-auto py-2 px-1 -mx-1 space-y-4">
          <ProviderModelSelect
            provider={provider}
            onProviderChange={setProvider}
            model={model}
            onModelChange={setModel}
            providerLabel="Extraction Provider"
            modelLabel="Extraction Model"
          />

          <div className="grid gap-1.5">
            <label className="text-xs font-medium">Text to extract from</label>
            <Textarea
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder="Paste conversation text, notes, or any content to extract entities and relations from..."
              rows={10}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={loading || !text.trim() || !provider || !model}>
            {loading ? "Extracting..." : "Extract"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
