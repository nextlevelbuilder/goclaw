import { useState, useEffect, useCallback } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Loader2, CheckCircle2, AlertTriangle, RefreshCw } from "lucide-react";
import type { ProviderData, ProviderInput } from "./hooks/use-providers";
import { slugify, isValidSlug } from "@/lib/slug";
import { useHttp } from "@/hooks/use-ws";

interface CLIAuthStatus {
  logged_in: boolean;
  email?: string;
  subscription_type?: string;
  error?: string;
}

interface ProviderFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  provider: ProviderData | null; // null = create mode
  onSubmit: (data: ProviderInput) => Promise<unknown>;
  existingProviders?: ProviderData[];
}

const PROVIDER_TYPES = [
  { value: "anthropic_native", label: "Anthropic (Native)", apiBase: "", placeholder: "https://api.anthropic.com" },
  { value: "openai_compat", label: "OpenAI Compatible", apiBase: "", placeholder: "https://api.openai.com/v1" },
  { value: "gemini_native", label: "Google Gemini", apiBase: "https://generativelanguage.googleapis.com/v1beta/openai", placeholder: "" },
  { value: "openrouter", label: "OpenRouter", apiBase: "https://openrouter.ai/api/v1", placeholder: "" },
  { value: "groq", label: "Groq", apiBase: "https://api.groq.com/openai/v1", placeholder: "" },
  { value: "deepseek", label: "DeepSeek", apiBase: "https://api.deepseek.com/v1", placeholder: "" },
  { value: "mistral", label: "Mistral AI", apiBase: "https://api.mistral.ai/v1", placeholder: "" },
  { value: "xai", label: "xAI (Grok)", apiBase: "https://api.x.ai/v1", placeholder: "" },
  { value: "minimax_native", label: "MiniMax (Native)", apiBase: "https://api.minimax.io/v1", placeholder: "" },
  { value: "cohere", label: "Cohere", apiBase: "https://api.cohere.ai/compatibility/v1", placeholder: "" },
  { value: "perplexity", label: "Perplexity", apiBase: "https://api.perplexity.ai", placeholder: "" },
  { value: "dashscope", label: "DashScope (Qwen)", apiBase: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", placeholder: "" },
  { value: "bailian", label: "Bailian Coding", apiBase: "https://coding-intl.dashscope.aliyuncs.com/v1", placeholder: "" },
  { value: "claude_cli", label: "Claude CLI (Local)", apiBase: "", placeholder: "" },
];

export function ProviderFormDialog({ open, onOpenChange, provider, onSubmit, existingProviders = [] }: ProviderFormDialogProps) {
  const isEdit = !!provider;
  const [name, setName] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [providerType, setProviderType] = useState("openai_compat");
  const [apiBase, setApiBase] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [cliAuth, setCliAuth] = useState<CLIAuthStatus | null>(null);
  const [cliAuthLoading, setCliAuthLoading] = useState(false);
  const http = useHttp();

  // Only one Claude CLI provider allowed per instance
  const hasClaudeCLI = !isEdit && existingProviders.some((p) => p.provider_type === "claude_cli");

  useEffect(() => {
    if (open) {
      setError("");
      if (provider) {
        setName(provider.name);
        setDisplayName(provider.display_name || "");
        setProviderType(provider.provider_type);
        setApiBase(provider.api_base || "");
        setApiKey(provider.api_key || "");
        setEnabled(provider.enabled);
      } else {
        setName("");
        setDisplayName("");
        setProviderType("openai_compat");
        setApiBase("");
        setApiKey("");
        setEnabled(true);
      }
    }
  }, [open, provider]);

  // Check Claude CLI auth status
  const checkCliAuth = useCallback(() => {
    setCliAuthLoading(true);
    http
      .get<CLIAuthStatus>("/v1/providers/claude-cli/auth-status")
      .then(setCliAuth)
      .catch(() => setCliAuth({ logged_in: false, error: "Failed to check auth status" }))
      .finally(() => setCliAuthLoading(false));
  }, [http]);

  useEffect(() => {
    if (providerType === "claude_cli" && open) {
      checkCliAuth();
    } else {
      setCliAuth(null);
    }
  }, [providerType, open, checkCliAuth]);

  const handleSubmit = async () => {
    if (!name.trim() || !providerType) return;
    setLoading(true);
    try {
      const data: ProviderInput = {
        name: name.trim(),
        display_name: displayName.trim() || undefined,
        provider_type: providerType,
        api_base: apiBase.trim() || undefined,
        enabled,
      };

      // Only include api_key if it's a real value (not the mask)
      if (apiKey && apiKey !== "***") {
        data.api_key = apiKey;
      }

      await onSubmit(data);
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save provider");
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{isEdit ? "Edit Provider" : "Add Provider"}</DialogTitle>
          <DialogDescription>Configure an LLM provider connection.</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-4 overflow-y-auto min-h-0">
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="name">Name *</Label>
              <Input
                id="name"
                value={name}
                onChange={(e) => setName(slugify(e.target.value))}
                placeholder="e.g. openrouter"
                readOnly={isEdit}
                className={isEdit ? "opacity-70 cursor-not-allowed" : ""}
              />
              <p className="text-xs text-muted-foreground">Lowercase, numbers, hyphens</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="displayName">Display Name</Label>
              <Input
                id="displayName"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                placeholder="OpenRouter"
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label>Provider Type *</Label>
            <Select
              value={providerType}
              onValueChange={(v) => {
                setProviderType(v);
                if (!isEdit) {
                  const preset = PROVIDER_TYPES.find((t) => t.value === v);
                  setApiBase(preset?.apiBase || "");
                }
              }}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PROVIDER_TYPES.map((t) => (
                  <SelectItem
                    key={t.value}
                    value={t.value}
                    disabled={t.value === "claude_cli" && hasClaudeCLI}
                  >
                    {t.label}
                    {t.value === "claude_cli" && hasClaudeCLI && (
                      <span className="ml-1 text-xs opacity-60">(already added)</span>
                    )}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {providerType !== "claude_cli" && (
            <>
              <div className="space-y-2">
                <Label htmlFor="apiBase">API Base URL</Label>
                <Input
                  id="apiBase"
                  value={apiBase}
                  onChange={(e) => setApiBase(e.target.value)}
                  placeholder={PROVIDER_TYPES.find((t) => t.value === providerType)?.placeholder || PROVIDER_TYPES.find((t) => t.value === providerType)?.apiBase || "https://api.example.com/v1"}
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="apiKey">API Key</Label>
                <Input
                  id="apiKey"
                  type="password"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  placeholder={isEdit ? "Leave as-is or enter new key" : "sk-..."}
                />
                {isEdit && apiKey === "***" && (
                  <p className="text-xs text-muted-foreground">
                    API key is set. Clear and type a new value to change it.
                  </p>
                )}
              </div>
            </>
          )}
          {providerType === "claude_cli" && (
            <div className="space-y-3">
              <p className="text-sm text-muted-foreground">
                Claude CLI uses your local <code className="rounded bg-muted px-1 py-0.5">claude</code> binary. No API key needed.
              </p>
              {cliAuthLoading ? (
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  Checking authentication...
                </div>
              ) : cliAuth?.logged_in ? (
                <div className="space-y-2">
                  <div className="flex items-center justify-between rounded-md border border-green-200 bg-green-50 px-3 py-2 dark:border-green-800 dark:bg-green-950">
                    <div className="flex items-center gap-2">
                      <CheckCircle2 className="h-4 w-4 text-green-600 dark:text-green-400" />
                      <p className="text-sm text-green-700 dark:text-green-300">
                        Authenticated as <strong>{cliAuth.email}</strong>
                        {cliAuth.subscription_type && (
                          <span className="ml-1 text-xs opacity-75">({cliAuth.subscription_type})</span>
                        )}
                      </p>
                    </div>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="h-7 px-2 text-green-700 hover:text-green-800 dark:text-green-400 dark:hover:text-green-300"
                      onClick={checkCliAuth}
                    >
                      <RefreshCw className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                  <details className="text-xs text-muted-foreground">
                    <summary className="cursor-pointer hover:text-foreground">Switch account?</summary>
                    <div className="mt-1.5 space-y-1 rounded-md border bg-muted/50 px-3 py-2">
                      <p>Run on the server terminal:</p>
                      <code className="block rounded bg-muted px-2 py-1 font-mono">claude auth logout && claude auth login</code>
                      <p>Then click <RefreshCw className="inline h-3 w-3" /> to re-check.</p>
                    </div>
                  </details>
                </div>
              ) : cliAuth ? (
                <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 dark:border-amber-800 dark:bg-amber-950">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <AlertTriangle className="h-4 w-4 text-amber-600 dark:text-amber-400" />
                      <p className="text-sm font-medium text-amber-700 dark:text-amber-300">Not authenticated</p>
                    </div>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="h-7 px-2 text-amber-700 hover:text-amber-800 dark:text-amber-400 dark:hover:text-amber-300"
                      onClick={checkCliAuth}
                    >
                      <RefreshCw className="h-3.5 w-3.5 mr-1" />
                      <span className="text-xs">Re-check</span>
                    </Button>
                  </div>
                  <p className="mt-1 text-sm text-amber-600 dark:text-amber-400">
                    Run on the server terminal:
                  </p>
                  <code className="mt-1 block rounded bg-amber-100 px-2 py-1 text-xs font-mono dark:bg-amber-900 dark:text-amber-300">
                    claude auth login
                  </code>
                  {cliAuth.error && (
                    <p className="mt-1 text-xs text-amber-500">{cliAuth.error}</p>
                  )}
                </div>
              ) : null}
            </div>
          )}

          <div className="flex items-center justify-between">
            <Label htmlFor="enabled">Enabled</Label>
            <Switch id="enabled" checked={enabled} onCheckedChange={setEnabled} />
          </div>
          {error && (
            <p className="text-sm text-destructive">{error}</p>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            Cancel
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={!name.trim() || !isValidSlug(name) || !providerType || loading}
          >
            {loading ? (isEdit ? "Saving..." : "Creating...") : isEdit ? "Save" : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
