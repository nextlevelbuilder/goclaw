import { useState, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import type { MCPServerData, MCPServerInput } from "./hooks/use-mcp";
import { slugify, isValidSlug } from "@/lib/slug";

interface MCPFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  server?: MCPServerData | null;
  onSubmit: (data: MCPServerInput) => Promise<unknown>;
}

const TRANSPORTS = [
  { value: "stdio", label: "stdio" },
  { value: "sse", label: "SSE" },
  { value: "streamable-http", label: "Streamable HTTP" },
];

export function MCPFormDialog({ open, onOpenChange, server, onSubmit }: MCPFormDialogProps) {
  const [name, setName] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [transport, setTransport] = useState("stdio");
  const [command, setCommand] = useState("");
  const [args, setArgs] = useState("");
  const [url, setUrl] = useState("");
  const [headers, setHeaders] = useState("");
  const [toolPrefix, setToolPrefix] = useState("");
  const [timeout, setTimeout] = useState(60);
  const [enabled, setEnabled] = useState(true);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (open) {
      setName(server?.name ?? "");
      setDisplayName(server?.display_name ?? "");
      setTransport(server?.transport ?? "stdio");
      setCommand(server?.command ?? "");
      setArgs(Array.isArray(server?.args) ? server.args.join(", ") : "");
      setUrl(server?.url ?? "");
      setHeaders(server?.headers ? JSON.stringify(server.headers, null, 2) : "");
      setToolPrefix(server?.tool_prefix ?? "");
      setTimeout(server?.timeout_sec ?? 60);
      setEnabled(server?.enabled ?? true);
      setError("");
    }
  }, [open, server]);

  const isStdio = transport === "stdio";

  const handleSubmit = async () => {
    if (!name.trim() || !transport) {
      setError("Name and transport are required");
      return;
    }
    if (!isValidSlug(name.trim())) {
      setError("Name must be a valid slug (lowercase letters, numbers, hyphens only)");
      return;
    }
    if (isStdio && !command.trim()) {
      setError("Command is required for stdio transport");
      return;
    }
    if (!isStdio && !url.trim()) {
      setError("URL is required for SSE/HTTP transport");
      return;
    }

    let parsedHeaders: Record<string, string> | undefined;
    if (!isStdio && headers.trim()) {
      try {
        parsedHeaders = JSON.parse(headers);
      } catch {
        setError("Headers must be valid JSON object");
        return;
      }
    }

    const parsedArgs = isStdio && args.trim()
      ? args.split(",").map((a) => a.trim()).filter(Boolean)
      : undefined;

    setLoading(true);
    setError("");
    try {
      await onSubmit({
        name: name.trim(),
        display_name: displayName.trim() || undefined,
        transport,
        command: isStdio ? command.trim() : undefined,
        args: parsedArgs,
        url: !isStdio ? url.trim() : undefined,
        headers: parsedHeaders,
        tool_prefix: toolPrefix.trim() || undefined,
        timeout_sec: timeout,
        enabled,
      });
      onOpenChange(false);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to save");
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !loading && onOpenChange(v)}>
      <DialogContent className="max-h-[85vh] max-w-lg flex flex-col">
        <DialogHeader>
          <DialogTitle>{server ? "Edit MCP Server" : "Add MCP Server"}</DialogTitle>
        </DialogHeader>

        <div className="grid gap-4 py-2 overflow-y-auto min-h-0">
          <div className="grid gap-1.5">
            <Label htmlFor="mcp-name">Name *</Label>
            <Input id="mcp-name" value={name} onChange={(e) => setName(slugify(e.target.value))} placeholder="my-mcp-server" />
            <p className="text-xs text-muted-foreground">Lowercase letters, numbers, and hyphens only</p>
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="mcp-display">Display Name</Label>
            <Input id="mcp-display" value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder="My MCP Server" />
          </div>

          <div className="grid gap-1.5">
            <Label>Transport *</Label>
            <div className="flex gap-2">
              {TRANSPORTS.map((t) => (
                <Button
                  key={t.value}
                  type="button"
                  variant={transport === t.value ? "default" : "outline"}
                  size="sm"
                  onClick={() => setTransport(t.value)}
                >
                  {t.label}
                </Button>
              ))}
            </div>
          </div>

          {isStdio ? (
            <>
              <div className="grid gap-1.5">
                <Label htmlFor="mcp-cmd">Command *</Label>
                <Input id="mcp-cmd" value={command} onChange={(e) => setCommand(e.target.value)} placeholder="npx -y @modelcontextprotocol/server-everything" className="font-mono text-sm" />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="mcp-args">Args (comma-separated)</Label>
                <Input id="mcp-args" value={args} onChange={(e) => setArgs(e.target.value)} placeholder="--flag1, --flag2" className="font-mono text-sm" />
              </div>
            </>
          ) : (
            <>
              <div className="grid gap-1.5">
                <Label htmlFor="mcp-url">URL *</Label>
                <Input id="mcp-url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="http://localhost:3001/sse" className="font-mono text-sm" />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="mcp-headers">Headers (JSON)</Label>
                <Input id="mcp-headers" value={headers} onChange={(e) => setHeaders(e.target.value)} placeholder='{"Authorization": "Bearer ..."}' className="font-mono text-sm" />
              </div>
            </>
          )}

          <div className="grid grid-cols-2 gap-4">
            <div className="grid gap-1.5">
              <Label htmlFor="mcp-prefix">Tool Prefix</Label>
              <Input id="mcp-prefix" value={toolPrefix} onChange={(e) => setToolPrefix(e.target.value)} placeholder="mcp_" />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="mcp-timeout">Timeout (seconds)</Label>
              <Input id="mcp-timeout" type="number" value={timeout} onChange={(e) => setTimeout(Number(e.target.value))} min={1} />
            </div>
          </div>

          <div className="flex items-center gap-2">
            <Switch id="mcp-enabled" checked={enabled} onCheckedChange={setEnabled} />
            <Label htmlFor="mcp-enabled">Enabled</Label>
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>Cancel</Button>
          <Button onClick={handleSubmit} disabled={loading}>
            {loading ? "Saving..." : server ? "Update" : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
