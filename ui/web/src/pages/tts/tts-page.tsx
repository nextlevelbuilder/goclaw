import { useState, useEffect } from "react";
import { Volume2, RefreshCw, Save } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { PageHeader } from "@/components/shared/page-header";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { useTtsConfig, type TtsConfig, type TtsProviderConfig } from "./hooks/use-tts-config";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";

const PROVIDERS = [
  { value: "", label: "None (Disabled)" },
  { value: "openai", label: "OpenAI" },
  { value: "elevenlabs", label: "ElevenLabs" },
  { value: "edge", label: "Edge (Free)" },
  { value: "minimax", label: "MiniMax" },
];

const AUTO_MODES = [
  { value: "off", label: "Off", desc: "Agent can use TTS tool manually" },
  { value: "always", label: "Always", desc: "All replies get audio" },
  { value: "inbound", label: "Inbound", desc: "Only when user sends voice/audio" },
  { value: "tagged", label: "Tagged", desc: "Only when reply contains [[tts]] tag" },
];

const REPLY_MODES = [
  { value: "final", label: "Final only", desc: "Only final replies" },
  { value: "all", label: "All", desc: "All replies including tool/block" },
];

export function TtsPage() {
  const { tts, loading, saving, error, refresh, save } = useTtsConfig();
  const spinning = useMinLoading(loading);

  const [draft, setDraft] = useState<TtsConfig>(tts);
  const showSkeleton = useDeferredLoading(loading && !draft.provider);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(tts);
    setDirty(false);
  }, [tts]);

  const update = (patch: Partial<TtsConfig>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setDirty(true);
  };

  const updateProvider = (provider: keyof Pick<TtsConfig, "openai" | "elevenlabs" | "edge" | "minimax">, patch: Partial<TtsProviderConfig>) => {
    setDraft((prev) => ({ ...prev, [provider]: { ...prev[provider], ...patch } }));
    setDirty(true);
  };

  const handleSave = async () => {
    await save(draft);
    setDirty(false);
  };

  if (showSkeleton) {
    return (
      <div className="p-4 sm:p-6">
        <PageHeader title="Text-to-Speech" description="Configure TTS providers and auto-apply settings" />
        <div className="mt-4">
          <TableSkeleton rows={3} />
        </div>
      </div>
    );
  }

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title="Text-to-Speech"
        description="Configure TTS providers and auto-apply settings"
        actions={
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> Refresh
            </Button>
            <Button size="sm" onClick={handleSave} disabled={!dirty || saving} className="gap-1">
              <Save className="h-3.5 w-3.5" /> {saving ? "Saving..." : "Save"}
            </Button>
          </div>
        }
      />

      {/* Status */}
      <Card className="mt-4">
        <CardHeader className="pb-3">
          <div className="flex items-center gap-2">
            <Volume2 className="h-5 w-5" />
            <CardTitle className="text-base">Status</CardTitle>
            <Badge variant={draft.provider ? "default" : "secondary"}>
              {draft.provider ? "Configured" : "Disabled"}
            </Badge>
          </div>
          <CardDescription>
            {draft.provider
              ? `Primary provider: ${draft.provider}, Auto: ${draft.auto}`
              : "No TTS provider configured"}
          </CardDescription>
        </CardHeader>
      </Card>

      {/* General Settings */}
      <Card className="mt-4">
        <CardHeader className="pb-3">
          <CardTitle className="text-base">General Settings</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {/* Provider */}
          <div className="grid gap-1.5">
            <Label>Primary Provider</Label>
            <div className="flex flex-wrap gap-2">
              {PROVIDERS.map((p) => (
                <Button
                  key={p.value}
                  type="button"
                  variant={draft.provider === p.value ? "default" : "outline"}
                  size="sm"
                  onClick={() => update({ provider: p.value })}
                >
                  {p.label}
                </Button>
              ))}
            </div>
          </div>

          {/* Auto mode */}
          <div className="grid gap-1.5">
            <Label>Auto-Apply Mode</Label>
            <div className="flex flex-wrap gap-2">
              {AUTO_MODES.map((m) => (
                <Button
                  key={m.value}
                  type="button"
                  variant={draft.auto === m.value ? "default" : "outline"}
                  size="sm"
                  onClick={() => update({ auto: m.value })}
                  title={m.desc}
                >
                  {m.label}
                </Button>
              ))}
            </div>
            <p className="text-xs text-muted-foreground">
              {AUTO_MODES.find((m) => m.value === draft.auto)?.desc}
            </p>
          </div>

          {/* Reply mode */}
          <div className="grid gap-1.5">
            <Label>Reply Mode</Label>
            <div className="flex gap-2">
              {REPLY_MODES.map((m) => (
                <Button
                  key={m.value}
                  type="button"
                  variant={draft.mode === m.value ? "default" : "outline"}
                  size="sm"
                  onClick={() => update({ mode: m.value })}
                  title={m.desc}
                >
                  {m.label}
                </Button>
              ))}
            </div>
          </div>

          {/* Max length & timeout */}
          <div className="grid grid-cols-2 gap-4">
            <div className="grid gap-1.5">
              <Label htmlFor="tts-maxlen">Max Text Length</Label>
              <Input
                id="tts-maxlen"
                type="number"
                value={draft.max_length}
                onChange={(e) => update({ max_length: Number(e.target.value) })}
                min={10}
              />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="tts-timeout">Timeout (ms)</Label>
              <Input
                id="tts-timeout"
                type="number"
                value={draft.timeout_ms}
                onChange={(e) => update({ timeout_ms: Number(e.target.value) })}
                min={1000}
              />
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Provider-specific settings */}
      {draft.provider && draft.provider !== "" && (
        <Card className="mt-4">
          <CardHeader className="pb-3">
            <CardTitle className="text-base">
              {PROVIDERS.find((p) => p.value === draft.provider)?.label} Settings
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {draft.provider === "openai" && (
              <>
                <div className="grid gap-1.5">
                  <Label htmlFor="oai-key">API Key</Label>
                  <Input id="oai-key" type="password" value={draft.openai.api_key ?? ""} onChange={(e) => updateProvider("openai", { api_key: e.target.value })} placeholder="sk-..." />
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="oai-base">API Base URL</Label>
                  <Input id="oai-base" value={draft.openai.api_base ?? ""} onChange={(e) => updateProvider("openai", { api_base: e.target.value })} placeholder="https://api.openai.com/v1" />
                </div>
                <div className="grid grid-cols-2 gap-4">
                  <div className="grid gap-1.5">
                    <Label htmlFor="oai-model">Model</Label>
                    <Input id="oai-model" value={draft.openai.model ?? ""} onChange={(e) => updateProvider("openai", { model: e.target.value })} placeholder="gpt-4o-mini-tts" />
                  </div>
                  <div className="grid gap-1.5">
                    <Label htmlFor="oai-voice">Voice</Label>
                    <Input id="oai-voice" value={draft.openai.voice ?? ""} onChange={(e) => updateProvider("openai", { voice: e.target.value })} placeholder="alloy" />
                  </div>
                </div>
              </>
            )}

            {draft.provider === "elevenlabs" && (
              <>
                <div className="grid gap-1.5">
                  <Label htmlFor="el-key">API Key</Label>
                  <Input id="el-key" type="password" value={draft.elevenlabs.api_key ?? ""} onChange={(e) => updateProvider("elevenlabs", { api_key: e.target.value })} placeholder="xi-..." />
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="el-base">Base URL</Label>
                  <Input id="el-base" value={draft.elevenlabs.base_url ?? ""} onChange={(e) => updateProvider("elevenlabs", { base_url: e.target.value })} placeholder="https://api.elevenlabs.io" />
                </div>
                <div className="grid grid-cols-2 gap-4">
                  <div className="grid gap-1.5">
                    <Label htmlFor="el-voice">Voice ID</Label>
                    <Input id="el-voice" value={draft.elevenlabs.voice_id ?? ""} onChange={(e) => updateProvider("elevenlabs", { voice_id: e.target.value })} placeholder="pMsXgVXv3BLzUgSXRplE" />
                  </div>
                  <div className="grid gap-1.5">
                    <Label htmlFor="el-model">Model ID</Label>
                    <Input id="el-model" value={draft.elevenlabs.model_id ?? ""} onChange={(e) => updateProvider("elevenlabs", { model_id: e.target.value })} placeholder="eleven_multilingual_v2" />
                  </div>
                </div>
              </>
            )}

            {draft.provider === "edge" && (
              <>
                <div className="grid gap-1.5">
                  <Label htmlFor="edge-voice">Voice</Label>
                  <Input id="edge-voice" value={draft.edge.voice ?? ""} onChange={(e) => updateProvider("edge", { voice: e.target.value })} placeholder="en-US-MichelleNeural" />
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="edge-rate">Speech Rate</Label>
                  <Input id="edge-rate" value={draft.edge.rate ?? ""} onChange={(e) => updateProvider("edge", { rate: e.target.value })} placeholder="+0%" />
                </div>
                <p className="text-xs text-muted-foreground">Edge TTS is free and requires no API key. Install with: pip install edge-tts</p>
              </>
            )}

            {draft.provider === "minimax" && (
              <>
                <div className="grid gap-1.5">
                  <Label htmlFor="mm-key">API Key</Label>
                  <Input id="mm-key" type="password" value={draft.minimax.api_key ?? ""} onChange={(e) => updateProvider("minimax", { api_key: e.target.value })} />
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="mm-group">Group ID *</Label>
                  <Input id="mm-group" value={draft.minimax.group_id ?? ""} onChange={(e) => updateProvider("minimax", { group_id: e.target.value })} placeholder="Required for MiniMax" />
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="mm-base">API Base</Label>
                  <Input id="mm-base" value={draft.minimax.api_base ?? ""} onChange={(e) => updateProvider("minimax", { api_base: e.target.value })} placeholder="https://api.minimax.io/v1" />
                </div>
                <div className="grid grid-cols-2 gap-4">
                  <div className="grid gap-1.5">
                    <Label htmlFor="mm-model">Model</Label>
                    <Input id="mm-model" value={draft.minimax.model ?? ""} onChange={(e) => updateProvider("minimax", { model: e.target.value })} placeholder="speech-02-hd" />
                  </div>
                  <div className="grid gap-1.5">
                    <Label htmlFor="mm-voice">Voice ID</Label>
                    <Input id="mm-voice" value={draft.minimax.voice_id ?? ""} onChange={(e) => updateProvider("minimax", { voice_id: e.target.value })} placeholder="Wise_Woman" />
                  </div>
                </div>
              </>
            )}
          </CardContent>
        </Card>
      )}

      <Separator className="my-6" />

      {error && <p className="text-sm text-destructive">{error}</p>}

      {dirty && (
        <div className="flex justify-end">
          <Button onClick={handleSave} disabled={saving} className="gap-1">
            <Save className="h-3.5 w-3.5" /> {saving ? "Saving..." : "Save Changes"}
          </Button>
        </div>
      )}
    </div>
  );
}
