import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Loader2 } from "lucide-react";
import { Textarea } from "@/components/ui/textarea";
import type { BuiltinToolData } from "./hooks/use-builtin-tools";
import { MEDIA_TOOLS } from "./media-provider-params-schema";
import { MediaProviderChainForm } from "./media-provider-chain-form";
import { KGSettingsForm } from "./kg-settings-form";
import { WebFetchExtractorChainForm } from "./web-fetch-extractor-chain-form";

const KG_TOOL = "knowledge_graph_search";
const WEB_FETCH_TOOL = "web_fetch";

interface Props {
  tool: BuiltinToolData | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: (name: string, settings: Record<string, unknown>) => Promise<void>;
}

export function BuiltinToolSettingsDialog({ tool, open, onOpenChange, onSave }: Props) {
  const isMedia = tool ? MEDIA_TOOLS.has(tool.name) : false;
  const isKG = tool?.name === KG_TOOL;
  const isWebFetch = tool?.name === WEB_FETCH_TOOL;
  const wide = isMedia || isKG || isWebFetch;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={wide ? "sm:max-w-2xl" : "sm:max-w-md"}>
        {isWebFetch && tool ? (
          <WebFetchExtractorChainForm
            initialSettings={tool.settings ?? {}}
            onSave={(settings) => onSave(tool.name, settings).then(() => onOpenChange(false))}
            onCancel={() => onOpenChange(false)}
          />
        ) : isMedia && tool ? (
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
  const { t } = useTranslation("tools");
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
      setError(t("builtin.jsonDialog.cannotFormat"));
    }
  };

  const handleSave = async () => {
    if (!tool) return;
    let parsed: Record<string, unknown>;
    try {
      parsed = JSON.parse(json);
    } catch {
      setError(t("builtin.jsonDialog.invalidJson"));
      return;
    }
    setSaving(true);
    setError("");
    try {
      await onSave(tool.name, parsed);
      onOpenChange(false);
    } catch {
      // toast shown by hook — keep dialog open
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t("builtin.jsonDialog.title", { name: tool?.display_name ?? tool?.name })}</DialogTitle>
        <DialogDescription>
          {t("builtin.jsonDialog.description")}
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
            {t("builtin.jsonDialog.formatJson")}
          </Button>
          {!validJson && <span className="text-xs text-destructive">{t("builtin.jsonDialog.invalidJsonSyntax")}</span>}
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={() => onOpenChange(false)}>
          {t("builtin.jsonDialog.cancel")}
        </Button>
        <Button onClick={handleSave} disabled={saving || !validJson}>
          {saving && <Loader2 className="h-4 w-4 animate-spin" />}
          {saving ? t("builtin.jsonDialog.saving") : t("builtin.jsonDialog.save")}
        </Button>
      </DialogFooter>
    </>
  );
}
