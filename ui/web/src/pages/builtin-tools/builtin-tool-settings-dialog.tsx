import { useState, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import type { BuiltinToolData } from "./hooks/use-builtin-tools";
import { MEDIA_TOOLS } from "./media-provider-params-schema";
import { MediaProviderChainForm } from "./media-provider-chain-form";
import { KGSettingsForm } from "./kg-settings-form";

const KG_TOOL = "knowledge_graph_search";

interface Props {
  tool: BuiltinToolData | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: (name: string, settings: Record<string, unknown>) => Promise<void>;
}

export function BuiltinToolSettingsDialog({ tool, open, onOpenChange, onSave }: Props) {
  const isMedia = tool ? MEDIA_TOOLS.has(tool.name) : false;
  const isKG = tool?.name === KG_TOOL;
  const wide = isMedia || isKG;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={wide ? "sm:max-w-2xl" : "sm:max-w-md"}>
        {isMedia && tool ? (
          <MediaProviderChainForm
            toolName={tool.name}
            initialSettings={tool.settings ?? {}}
            onSave={(settings) => onSave(tool.name, settings).then(() => onOpenChange(false))}
            onCancel={() => onOpenChange(false)}
          />
        ) : isKG && tool ? (
          <KGSettingsForm
            initialSettings={tool.settings ?? {}}
            onSave={(settings) => onSave(tool.name, settings).then(() => onOpenChange(false))}
            onCancel={() => onOpenChange(false)}
          />
        ) : (
          <JsonSettingsForm tool={tool} onOpenChange={onOpenChange} onSave={onSave} />
        )}
      </DialogContent>
    </Dialog>
  );
}


function JsonSettingsForm({
  tool,
  onOpenChange,
  onSave,
}: {
  tool: BuiltinToolData | null;
  onOpenChange: (open: boolean) => void;
  onSave: (name: string, settings: Record<string, unknown>) => Promise<void>;
}) {
  const [json, setJson] = useState("");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [validJson, setValidJson] = useState(true);

  useEffect(() => {
    if (tool) {
      setJson(JSON.stringify(tool.settings ?? {}, null, 2));
      setError("");
      setValidJson(true);
    }
  }, [tool]);

  const handleJsonChange = (text: string) => {
    setJson(text);
    try {
      JSON.parse(text);
      setValidJson(true);
      setError("");
    } catch {
      setValidJson(false);
    }
  };

  const handleFormat = () => {
    try {
      const parsed = JSON.parse(json);
      setJson(JSON.stringify(parsed, null, 2));
      setError("");
      setValidJson(true);
    } catch {
      setError("Cannot format: invalid JSON");
    }
  };

  const handleSave = async () => {
    if (!tool) return;
    try {
      const parsed = JSON.parse(json);
      setSaving(true);
      setError("");
      await onSave(tool.name, parsed);
      onOpenChange(false);
    } catch (e) {
      setError(e instanceof SyntaxError ? "Invalid JSON" : String(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>Settings: {tool?.display_name ?? tool?.name}</DialogTitle>
        <DialogDescription>
          Edit tool-specific settings as JSON. Changes take effect immediately after saving.
        </DialogDescription>
      </DialogHeader>
      <div className="space-y-3">
        <Textarea
          value={json}
          onChange={(e) => handleJsonChange(e.target.value)}
          rows={10}
          className={`font-mono text-sm ${!validJson ? "border-destructive" : ""}`}
        />
        <div className="flex items-center justify-between">
          <Button variant="ghost" size="sm" onClick={handleFormat} className="h-7 px-2 text-xs">
            Format JSON
          </Button>
          {!validJson && <span className="text-xs text-destructive">Invalid JSON syntax</span>}
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={() => onOpenChange(false)}>
          Cancel
        </Button>
        <Button onClick={handleSave} disabled={saving || !validJson}>
          {saving ? "Saving..." : "Save"}
        </Button>
      </DialogFooter>
    </>
  );
}
