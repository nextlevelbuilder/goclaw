import { useState, useEffect } from "react";
import { Save } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { InfoLabel } from "@/components/shared/info-label";
import { TagInput } from "@/components/shared/tag-input";

interface GatewayData {
  host?: string;
  port?: number;
  token?: string;
  owner_ids?: string[];
  allowed_origins?: string[];
  max_message_chars?: number;
  rate_limit_rpm?: number;
  injection_action?: string;
  inbound_debounce_ms?: number;
}

const DEFAULT: GatewayData = {};

function isSecret(val: unknown): boolean {
  return typeof val === "string" && val.includes("***");
}

interface Props {
  data: GatewayData | undefined;
  onSave: (value: GatewayData) => Promise<void>;
  saving: boolean;
}

export function GatewaySection({ data, onSave, saving }: Props) {
  const [draft, setDraft] = useState<GatewayData>(data ?? DEFAULT);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? DEFAULT);
    setDirty(false);
  }, [data]);

  const update = (patch: Partial<GatewayData>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setDirty(true);
  };

  const handleSave = () => {
    // Don't send masked secret fields back
    const toSave = { ...draft };
    if (isSecret(toSave.token)) {
      delete toSave.token;
    }
    onSave(toSave);
  };

  if (!data) return null;

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">Gateway</CardTitle>
        <CardDescription>WebSocket & HTTP server settings, security</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div className="grid gap-1.5">
            <InfoLabel tip="IP address to bind the server. Use 0.0.0.0 to accept connections from any interface.">Host</InfoLabel>
            <Input
              value={draft.host ?? ""}
              onChange={(e) => update({ host: e.target.value })}
              placeholder="0.0.0.0"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip="TCP port for the WebSocket and HTTP server.">Port</InfoLabel>
            <Input
              type="number"
              value={draft.port ?? ""}
              onChange={(e) => update({ port: Number(e.target.value) })}
              placeholder="18790"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip="Bearer token for authenticating WebSocket and API connections. Managed via GOCLAW_GATEWAY_TOKEN env var.">Token</InfoLabel>
            <Input
              type="password"
              value={draft.token ?? ""}
              disabled={isSecret(draft.token)}
              readOnly={isSecret(draft.token)}
              onChange={(e) => update({ token: e.target.value })}
            />
            {isSecret(draft.token) && (
              <p className="text-xs text-muted-foreground">Managed via GOCLAW_GATEWAY_TOKEN</p>
            )}
          </div>
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="grid gap-1.5">
            <InfoLabel tip="Sender IDs with admin privileges. These users bypass rate limits and can manage configuration.">Owner IDs</InfoLabel>
            <TagInput
              value={draft.owner_ids ?? []}
              onChange={(v) => update({ owner_ids: v })}
              placeholder="Type ID and press Enter..."
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip="CORS allowed origins for the HTTP API. Empty means all origins are allowed.">Allowed Origins</InfoLabel>
            <TagInput
              value={draft.allowed_origins ?? []}
              onChange={(v) => update({ allowed_origins: v })}
              placeholder="Type origin and press Enter..."
            />
          </div>
        </div>

        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <div className="grid gap-1.5">
            <InfoLabel tip="Maximum characters per inbound message. Messages exceeding this are rejected.">Max Message Chars</InfoLabel>
            <Input
              type="number"
              value={draft.max_message_chars ?? ""}
              onChange={(e) => update({ max_message_chars: Number(e.target.value) })}
              placeholder="32000"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip="Requests per minute per sender. 0 = no limit. Prevents abuse from individual users.">Rate Limit (RPM)</InfoLabel>
            <Input
              type="number"
              value={draft.rate_limit_rpm ?? ""}
              onChange={(e) => update({ rate_limit_rpm: Number(e.target.value) })}
              placeholder="20 (0 = disabled)"
              min={0}
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip="Delay in ms before processing messages. Groups rapid messages into one request. -1 = disabled.">Inbound Debounce (ms)</InfoLabel>
            <Input
              type="number"
              value={draft.inbound_debounce_ms ?? ""}
              onChange={(e) => update({ inbound_debounce_ms: Number(e.target.value) })}
              placeholder="1000 (-1 = disabled)"
              min={-1}
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip="Action when prompt injection is detected. Off = disabled, Log = log only, Warn = add warning to prompt, Block = reject.">Injection Action</InfoLabel>
            <Select value={draft.injection_action ?? "warn"} onValueChange={(v) => update({ injection_action: v })}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="off">Off</SelectItem>
                <SelectItem value="log">Log</SelectItem>
                <SelectItem value="warn">Warn</SelectItem>
                <SelectItem value="block">Block</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        {dirty && (
          <div className="flex justify-end pt-2">
            <Button size="sm" onClick={handleSave} disabled={saving} className="gap-1.5">
              <Save className="h-3.5 w-3.5" /> {saving ? "Saving..." : "Save"}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
