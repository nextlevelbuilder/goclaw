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
import { WebSearchChainForm } from "./web-search-chain-form";
import { SttProviderForm } from "./stt-provider-form";

const KG_TOOL = "knowledge_graph_search";
const WEB_FETCH_TOOL = "web_fetch";
const WEB_SEARCH_TOOL = "web_search";
const STT_TOOL = "stt";

interface Props {
  tool: BuiltinToolData | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: (name: string, settings: Record<string, unknown>) => Promise<void>;
  /**
   * When true the dialog is editing a tenant override. Initial form values
   * are drawn from `tool.tenant_settings ?? tool.settings` (fall back to the
   * global default when no override exists yet) and the parent page is
   * expected to route the save through the tenant-config endpoint. When
   * false the dialog edits the global `tool.settings` directly — parent
   * must enforce master-scope (the backend rejects non-master writes).
   */
  tenantScope?: boolean;
  /**
   * Optional reset handler. When provided, dialogs rendered in tenant scope
   * with an existing override show a "Reset to default" button that calls
   * this with the tool name — parent maps it to PUT tenant-config with
   * settings:null, clearing the override while preserving tenant_enabled.
   */
  onResetToDefault?: (name: string) => Promise<void>;
}

export function BuiltinToolSettingsDialog({
  tool,
  open,
  onOpenChange,
  onSave,
  tenantScope = false,
  onResetToDefault,
}: Props) {
  const { t } = useTranslation("tools");
  const isMedia = tool ? MEDIA_TOOLS.has(tool.name) : false;
  const isKG = tool?.name === KG_TOOL;
  const isWebFetch = tool?.name === WEB_FETCH_TOOL;
  const isWebSearch = tool?.name === WEB_SEARCH_TOOL;
  const isStt = tool?.name === STT_TOOL;
  const wide = isMedia || isKG || isWebFetch || isWebSearch;

  // Tenant-scope overlay: prefer the tenant override when present; fall back
  // to the global default so the form opens pre-populated with something
  // sensible when the admin is creating a new override.
  const initialSettings: Record<string, unknown> =
    (tenantScope ? (tool?.tenant_settings ?? tool?.settings) : tool?.settings) ?? {};
  const hasTenantOverride = tenantScope && tool?.tenant_settings != null;

  const modeBadge = tenantScope ? (
    <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
      <span className="rounded bg-amber-100 px-1.5 py-0.5 font-medium text-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
        {t("builtin.tenantOverrideBadge")}
      </span>
      <span>{hasTenantOverride ? t("builtin.tenantOverrideHint") : t("builtin.tenantOverrideNewHint")}</span>
    </div>
  ) : null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={wide ? "sm:max-w-2xl" : "sm:max-w-md"}>
        {modeBadge}
        {isWebSearch && tool ? (
          <WebSearchChainForm
            initialSettings={initialSettings}
            secretsSet={tool.secrets_set}
            onSave={(settings) => onSave(tool.name, settings).then(() => onOpenChange(false))}
            onCancel={() => onOpenChange(false)}
          />
        ) : isStt && tool ? (
          <SttProviderForm
            initialSettings={initialSettings}
            onSave={(settings) => onSave(tool.name, settings).then(() => onOpenChange(false))}
            onCancel={() => onOpenChange(false)}
          />
        ) : isWebFetch && tool ? (
          <WebFetchExtractorChainForm
            initialSettings={initialSettings}
            onSave={(settings) => onSave(tool.name, settings).then(() => onOpenChange(false))}
            onCancel={() => onOpenChange(false)}
          />
        ) : isMedia && tool ? (
          <MediaProviderChainForm
            toolName={tool.name}
            initialSettings={initialSettings}
            onSave={(settings) => onSave(tool.name, settings).then(() => onOpenChange(false))}
            onCancel={() => onOpenChange(false)}
          />
        ) : isKG && tool ? (
          <KGSettingsForm
            initialSettings={initialSettings}
            onSave={(settings) => onSave(tool.name, settings).then(() => onOpenChange(false))}
            onCancel={() => onOpenChange(false)}
          />
        ) : (
          <JsonSettingsForm
            tool={tool}
            initialSettings={initialSettings}
            onOpenChange={onOpenChange}
            onSave={onSave}
          />
        )}
        {hasTenantOverride && onResetToDefault && tool ? (
          <div className="mt-2 border-t pt-2 text-xs">
            <button
              type="button"
              onClick={() => onResetToDefault(tool.name).then(() => onOpenChange(false))}
              className="text-muted-foreground hover:text-foreground underline underline-offset-2"
            >
              {t("builtin.resetToGlobalDefault")}
            </button>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}


function JsonSettingsForm({
  tool,
  initialSettings,
  onOpenChange,
  onSave,
}: {
  tool: BuiltinToolData | null;
  initialSettings: Record<string, unknown>;
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
      // initialSettings is pre-resolved by the parent to account for tenant
      // scope (tenant_settings ?? settings). Re-hydrate every time the tool
      // or the resolved initial changes — handles switch-between-tools and
      // tenant-override-cleared flows.
      setJson(JSON.stringify(initialSettings, null, 2));
      setError("");
      setValidJson(true);
    }
  }, [tool, initialSettings]);

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
