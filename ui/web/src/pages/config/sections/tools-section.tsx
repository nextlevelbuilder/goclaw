import { useState, useEffect } from "react";
import { Save } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { InfoLabel } from "@/components/shared/info-label";
import { ToolNameSelect } from "@/components/shared/tool-name-select";

/* eslint-disable @typescript-eslint/no-explicit-any */
type ToolsData = Record<string, any>;

const DEFAULT: ToolsData = {};

interface Props {
  data: ToolsData | undefined;
  onSave: (value: ToolsData) => Promise<void>;
  saving: boolean;
}

export function ToolsSection({ data, onSave, saving }: Props) {
  const [draft, setDraft] = useState<ToolsData>(data ?? DEFAULT);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? DEFAULT);
    setDirty(false);
  }, [data]);

  const update = (patch: Partial<ToolsData>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setDirty(true);
  };

  const updateNested = (section: string, patch: Record<string, any>) => {
    setDraft((prev) => ({
      ...prev,
      [section]: { ...(prev[section] ?? {}), ...patch },
    }));
    setDirty(true);
  };

  if (!data) return null;

  const exec = draft.execApproval ?? {};
  const webFetch = draft.web_fetch ?? {};
  const web = draft.web ?? {};
  const brave = web.brave ?? {};
  const ddg = web.duckduckgo ?? {};
  const browser = draft.browser ?? {};

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">Tools</CardTitle>
        <CardDescription>Tool policies, exec approval, web search, browser</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Profile & Lists */}
        <div className="grid grid-cols-2 gap-4">
          <div className="grid gap-1.5">
            <InfoLabel tip="Tool profile preset. Minimal = basic tools, Coding = filesystem + exec, Messaging = channels, Full = all tools enabled.">Profile</InfoLabel>
            <Select value={draft.profile ?? ""} onValueChange={(v) => update({ profile: v })}>
              <SelectTrigger>
                <SelectValue placeholder="Select profile" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="minimal">Minimal</SelectItem>
                <SelectItem value="coding">Coding</SelectItem>
                <SelectItem value="messaging">Messaging</SelectItem>
                <SelectItem value="full">Full</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip="Maximum tool executions per hour across all tools. 0 = no limit.">Rate Limit (per hour)</InfoLabel>
            <Input
              type="number"
              value={draft.rate_limit_per_hour ?? ""}
              onChange={(e) => update({ rate_limit_per_hour: Number(e.target.value) })}
              placeholder="0 = disabled"
              min={0}
            />
          </div>
        </div>

        <div className="space-y-4">
          <div className="grid gap-1.5">
            <InfoLabel tip="Explicit whitelist of tool names. Only these tools will be available (overrides profile).">Allow</InfoLabel>
            <ToolNameSelect
              value={draft.allow ?? []}
              onChange={(v) => { update({ allow: v }); }}
              placeholder="Select tools to allow..."
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip="Blacklist of tool names. These tools are disabled regardless of profile or allow list.">Deny</InfoLabel>
            <ToolNameSelect
              value={draft.deny ?? []}
              onChange={(v) => { update({ deny: v }); }}
              placeholder="Select tools to deny..."
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip="Additional tools to enable on top of the profile. Additive to the profile's default set.">Also Allow</InfoLabel>
            <ToolNameSelect
              value={draft.alsoAllow ?? []}
              onChange={(v) => { update({ alsoAllow: v }); }}
              placeholder="Select additional tools..."
            />
          </div>
        </div>

        <div className="flex items-center justify-between">
          <div>
            <InfoLabel tip="Automatically redact API keys, tokens, and other credentials from tool output before sending to the LLM.">Scrub Credentials</InfoLabel>
          </div>
          <Switch
            checked={draft.scrub_credentials !== false}
            onCheckedChange={(v) => update({ scrub_credentials: v })}
          />
        </div>

        <Separator />

        {/* Exec Approval */}
        <div>
          <h4 className="mb-3 text-sm font-medium">Exec Approval</h4>
          <div className="grid grid-cols-2 gap-4">
            <div className="grid gap-1.5">
              <InfoLabel tip="Security level for shell command execution. Deny = no exec, Allowlist = only matching patterns, Full = allow all commands.">Security</InfoLabel>
              <Select value={exec.security ?? "full"} onValueChange={(v) => updateNested("execApproval", { security: v })}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="deny">Deny All</SelectItem>
                  <SelectItem value="allowlist">Allowlist</SelectItem>
                  <SelectItem value="full">Full (Allow All)</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-1.5">
              <InfoLabel tip="When to ask the user for approval before executing a command. Off = never ask, On Miss = ask if not in allowlist, Always = always ask.">Ask Mode</InfoLabel>
              <Select value={exec.ask ?? "off"} onValueChange={(v) => updateNested("execApproval", { ask: v })}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="off">Off</SelectItem>
                  <SelectItem value="on-miss">On Miss</SelectItem>
                  <SelectItem value="always">Always</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          {exec.security === "allowlist" && (
            <div className="mt-3 grid gap-1.5">
              <Label>Allowlist (glob patterns)</Label>
              <Textarea
                value={(exec.allowlist ?? []).join("\n")}
                onChange={(e) =>
                  updateNested("execApproval", {
                    allowlist: e.target.value.split("\n").filter(Boolean),
                  })
                }
                className="min-h-[80px] font-mono text-xs"
                placeholder="git *&#10;npm *&#10;ls *"
              />
            </div>
          )}
        </div>

        <Separator />

        {/* Web Search */}
        <div>
          <h4 className="mb-3 text-sm font-medium">Web Search</h4>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label>DuckDuckGo</Label>
                <Switch
                  checked={ddg.enabled !== false}
                  onCheckedChange={(v) => updateNested("web", { duckduckgo: { ...ddg, enabled: v } })}
                />
              </div>
              <div className="grid gap-1.5">
                <Label className="text-xs text-muted-foreground">Max Results</Label>
                <Input
                  type="number"
                  value={ddg.max_results ?? ""}
                  onChange={(e) => updateNested("web", { duckduckgo: { ...ddg, max_results: Number(e.target.value) } })}
                  placeholder="5"
                  min={1}
                />
              </div>
            </div>
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label>Brave Search</Label>
                <Switch
                  checked={brave.enabled ?? false}
                  onCheckedChange={(v) => updateNested("web", { brave: { ...brave, enabled: v } })}
                />
              </div>
              <div className="grid gap-1.5">
                <Label className="text-xs text-muted-foreground">Max Results</Label>
                <Input
                  type="number"
                  value={brave.max_results ?? ""}
                  onChange={(e) => updateNested("web", { brave: { ...brave, max_results: Number(e.target.value) } })}
                  placeholder="5"
                  min={1}
                />
              </div>
            </div>
          </div>
        </div>

        <Separator />

        {/* Web Fetch */}
        <div>
          <h4 className="mb-3 text-sm font-medium">Web Fetch</h4>
          <div className="grid gap-3">
            <div className="grid gap-1.5 max-w-xs">
              <InfoLabel tip="Domain policy for URL fetching. Allow All = fetch any domain. Allowlist = only fetch from specified domains.">Policy</InfoLabel>
              <Select
                value={webFetch.policy ?? "allow_all"}
                onValueChange={(v) => updateNested("web_fetch", { ...webFetch, policy: v })}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="allow_all">Allow All</SelectItem>
                  <SelectItem value="allowlist">Allowlist</SelectItem>
                </SelectContent>
              </Select>
            </div>
            {webFetch.policy === "allowlist" && (
              <div className="grid gap-1.5">
                <Label>Allowed Domains (one per line, supports *.example.com)</Label>
                <Textarea
                  value={(webFetch.allowed_domains ?? []).join("\n")}
                  onChange={(e) =>
                    updateNested("web_fetch", {
                      ...webFetch,
                      allowed_domains: e.target.value.split("\n").filter(Boolean),
                    })
                  }
                  className="min-h-[80px] font-mono text-xs"
                  placeholder={"github.com\n*.wikipedia.org\ndocs.google.com"}
                />
              </div>
            )}
            <div className="grid gap-1.5">
              <InfoLabel tip="Domains that are always blocked regardless of policy mode. Useful for preventing info-leak sites (IP lookup, etc.).">Blocked Domains (one per line, supports *.example.com)</InfoLabel>
              <Textarea
                value={(webFetch.blocked_domains ?? []).join("\n")}
                onChange={(e) =>
                  updateNested("web_fetch", {
                    ...webFetch,
                    blocked_domains: e.target.value.split("\n").filter(Boolean),
                  })
                }
                className="min-h-[80px] font-mono text-xs"
                placeholder={"ifconfig.co\nipinfo.io\n*.whatismyip.com"}
              />
            </div>
          </div>
        </div>

        <Separator />

        {/* Browser */}
        <div>
          <h4 className="mb-3 text-sm font-medium">Browser</h4>
          <div className="flex gap-6">
            <div className="flex items-center gap-2">
              <Label>Enabled</Label>
              <Switch
                checked={browser.enabled !== false}
                onCheckedChange={(v) => updateNested("browser", { enabled: v })}
              />
            </div>
            <div className="flex items-center gap-2">
              <Label>Headless</Label>
              <Switch
                checked={browser.headless !== false}
                onCheckedChange={(v) => updateNested("browser", { headless: v })}
              />
            </div>
          </div>
        </div>

        {dirty && (
          <div className="flex justify-end pt-2">
            <Button size="sm" onClick={() => onSave(draft)} disabled={saving} className="gap-1.5">
              <Save className="h-3.5 w-3.5" /> {saving ? "Saving..." : "Save"}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
