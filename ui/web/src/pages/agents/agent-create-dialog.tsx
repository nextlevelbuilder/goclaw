import { useState, useMemo, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Combobox } from "@/components/ui/combobox";
import type { AgentData } from "@/types/agent";
import { slugify, isValidSlug } from "@/lib/slug";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useProviderModels } from "@/pages/providers/hooks/use-provider-models";
import { useProviderVerify } from "@/pages/providers/hooks/use-provider-verify";
import { AGENT_PRESETS } from "./agent-presets";

interface AgentCreateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (data: Partial<AgentData>) => Promise<unknown>;
}

export function AgentCreateDialog({ open, onOpenChange, onCreate }: AgentCreateDialogProps) {
  const { providers } = useProviders();
  const [agentKey, setAgentKey] = useState("");
  const [keyTouched, setKeyTouched] = useState(false);
  const [displayName, setDisplayName] = useState("");
  const [provider, setProvider] = useState("");
  const [model, setModel] = useState("");
  const [agentType, setAgentType] = useState<"open" | "predefined">("open");
  const [description, setDescription] = useState("");
  const [loading, setLoading] = useState(false);

  const enabledProviders = providers.filter((p) => p.enabled);

  // Look up provider ID from selected provider name for model fetching
  const selectedProviderId = useMemo(
    () => enabledProviders.find((p) => p.name === provider)?.id,
    [enabledProviders, provider],
  );
  const { models, loading: modelsLoading } = useProviderModels(selectedProviderId);
  const { verify, verifying, result: verifyResult, reset: resetVerify } = useProviderVerify();

  // Reset verification when provider or model changes
  useEffect(() => {
    resetVerify();
  }, [provider, model, resetVerify]);

  const handleVerify = async () => {
    if (!selectedProviderId || !model.trim()) return;
    await verify(selectedProviderId, model.trim());
  };

  const handleVerifyAndCreate = async () => {
    if (!selectedProviderId || !model.trim()) return;
    const res = await verify(selectedProviderId, model.trim());
    if (res?.valid) await handleCreate();
  };

  const handleCreate = async () => {
    if (!agentKey.trim()) return;
    setLoading(true);
    try {
      await onCreate({
        agent_key: agentKey.trim(),
        display_name: displayName.trim() || undefined,
        provider: provider.trim(),
        model: model.trim(),
        agent_type: agentType,
        other_config: description.trim() ? { description: description.trim() } : undefined,
      });
      onOpenChange(false);
      setAgentKey("");
      setKeyTouched(false);
      setDisplayName("");
      setProvider("");
      setModel("");
      setAgentType("open");
      setDescription("");
    } catch {
      // error handled upstream
    } finally {
      setLoading(false);
    }
  };

  const handleProviderChange = (value: string) => {
    setProvider(value);
    setModel("");
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-4xl max-h-[90vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>Create Agent</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 py-4 overflow-y-auto min-h-0">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="displayName">Display Name *</Label>
              <Input
                id="displayName"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                onBlur={() => {
                  if (!keyTouched && displayName.trim()) {
                    setAgentKey(slugify(displayName.trim()));
                  }
                }}
                placeholder="My Agent"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="agentKey">Agent Key *</Label>
              <Input
                id="agentKey"
                value={agentKey}
                onChange={(e) => {
                  setKeyTouched(true);
                  setAgentKey(e.target.value);
                }}
                onBlur={() => setAgentKey(slugify(agentKey))}
                placeholder="e.g. my-agent"
              />
              <p className="text-xs text-muted-foreground">Lowercase, numbers, hyphens</p>
            </div>
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label>Provider *</Label>
              {enabledProviders.length > 0 ? (
                <Select value={provider} onValueChange={handleProviderChange}>
                  <SelectTrigger>
                    <SelectValue placeholder="Select provider" />
                  </SelectTrigger>
                  <SelectContent>
                    {enabledProviders.map((p) => (
                      <SelectItem key={p.name} value={p.name}>
                        {p.display_name || p.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : (
                <Input
                  value={provider}
                  onChange={(e) => setProvider(e.target.value)}
                  placeholder="openrouter"
                />
              )}
            </div>
            <div className="space-y-2">
              <Label>Model *</Label>
              <div className="flex gap-2">
                <div className="flex-1">
                  <Combobox
                    value={model}
                    onChange={setModel}
                    options={models.map((m) => ({ value: m.id, label: m.name }))}
                    placeholder={modelsLoading ? "Loading models..." : "Enter or select model"}
                  />
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-9 px-3"
                  disabled={!selectedProviderId || !model.trim() || verifying}
                  onClick={handleVerify}
                >
                  {verifying ? "..." : "Check"}
                </Button>
              </div>
              {verifyResult && (
                <p className={`text-xs ${verifyResult.valid ? "text-success" : "text-destructive"}`}>
                  {verifyResult.valid ? "Model verified" : verifyResult.error || "Verification failed"}
                </p>
              )}
              {!verifyResult && provider && !modelsLoading && models.length === 0 && (
                <p className="text-xs text-muted-foreground">This provider doesn't list models — type the model ID manually.</p>
              )}
            </div>
          </div>
          <div className="space-y-2">
            <Label>Agent Type</Label>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => setAgentType("open")}
                className={`flex-1 rounded-md border px-3 py-2 text-sm font-medium transition-colors ${
                  agentType === "open"
                    ? "border-primary bg-primary text-primary-foreground"
                    : "border-input bg-background hover:bg-accent"
                }`}
              >
                Open
                <span className="block text-xs font-normal opacity-70">Per-user context</span>
              </button>
              <button
                type="button"
                onClick={() => setAgentType("predefined")}
                className={`flex-1 rounded-md border px-3 py-2 text-sm font-medium transition-colors ${
                  agentType === "predefined"
                    ? "border-primary bg-primary text-primary-foreground"
                    : "border-input bg-background hover:bg-accent"
                }`}
              >
                Predefined
                <span className="block text-xs font-normal opacity-70">Agent-level config</span>
              </button>
            </div>
          </div>

          {agentType === "predefined" && (
            <div className="space-y-3">
              <Label>Describe Your Agent</Label>
              <div className="flex flex-wrap gap-1.5">
                {AGENT_PRESETS.map((preset) => (
                  <button
                    key={preset.label}
                    type="button"
                    onClick={() => setDescription(preset.prompt)}
                    className="rounded-full border px-2.5 py-0.5 text-xs transition-colors hover:bg-accent"
                  >
                    {preset.label}
                  </button>
                ))}
              </div>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Describe your agent's personality, purpose, and behavior..."
                className="min-h-[120px]"
              />
              <p className="text-xs text-muted-foreground">
                AI will automatically generate your agent's context files from this description.
                Leave empty to start with templates.
              </p>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            Cancel
          </Button>
          {loading ? (
            <Button disabled>Creating...</Button>
          ) : !verifyResult?.valid && selectedProviderId && model.trim() ? (
            <Button onClick={handleVerifyAndCreate} disabled={verifying || !displayName.trim() || !agentKey.trim() || !isValidSlug(agentKey) || (agentType === "predefined" && !description.trim())}>
              {verifying ? "Checking..." : "Check & Create"}
            </Button>
          ) : (
            <Button onClick={handleCreate} disabled={!displayName.trim() || !agentKey.trim() || !isValidSlug(agentKey) || !provider.trim() || !model.trim() || !verifyResult?.valid || (agentType === "predefined" && !description.trim())}>
              Create
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
