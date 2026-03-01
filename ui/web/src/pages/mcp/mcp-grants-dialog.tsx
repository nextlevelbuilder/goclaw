import { useState, useEffect } from "react";
import { Trash2, Plus } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import type { MCPServerData, MCPAgentGrant } from "./hooks/use-mcp";

interface MCPGrantsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  server: MCPServerData;
  onGrant: (agentId: string, toolAllow?: string[], toolDeny?: string[]) => Promise<void>;
  onRevoke: (agentId: string) => Promise<void>;
  onLoadGrants: (agentId: string) => Promise<MCPAgentGrant[]>;
}

export function MCPGrantsDialog({
  open,
  onOpenChange,
  server,
  onGrant,
  onRevoke,
}: MCPGrantsDialogProps) {
  const [agentId, setAgentId] = useState("");
  const [toolAllow, setToolAllow] = useState("");
  const [toolDeny, setToolDeny] = useState("");
  const [grants, setGrants] = useState<MCPAgentGrant[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (open) {
      setAgentId("");
      setToolAllow("");
      setToolDeny("");
      setGrants([]);
      setError("");
    }
  }, [open]);

  const handleGrant = async () => {
    if (!agentId.trim()) {
      setError("Agent ID is required");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const allow = toolAllow.trim() ? toolAllow.split(",").map((s) => s.trim()).filter(Boolean) : undefined;
      const deny = toolDeny.trim() ? toolDeny.split(",").map((s) => s.trim()).filter(Boolean) : undefined;
      await onGrant(agentId.trim(), allow, deny);
      setGrants((prev) => [
        ...prev,
        {
          id: crypto.randomUUID(),
          server_id: server.id,
          agent_id: agentId.trim(),
          enabled: true,
          tool_allow: allow ?? null,
          tool_deny: deny ?? null,
          granted_by: "",
          created_at: new Date().toISOString(),
        },
      ]);
      setAgentId("");
      setToolAllow("");
      setToolDeny("");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to grant");
    } finally {
      setLoading(false);
    }
  };

  const handleRevoke = async (grant: MCPAgentGrant) => {
    setLoading(true);
    try {
      await onRevoke(grant.agent_id);
      setGrants((prev) => prev.filter((g) => g.agent_id !== grant.agent_id));
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to revoke");
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Agent Grants - {server.display_name || server.name}</DialogTitle>
        </DialogHeader>

        {/* Existing grants */}
        {grants.length > 0 && (
          <div className="space-y-2">
            <Label>Current Grants</Label>
            <div className="rounded-md border">
              {grants.map((grant) => (
                <div key={grant.id} className="flex items-center justify-between border-b px-3 py-2 last:border-0">
                  <div className="flex items-center gap-2">
                    <Badge variant="outline" className="font-mono text-xs">{grant.agent_id}</Badge>
                    {Array.isArray(grant.tool_allow) && grant.tool_allow.length > 0 && (
                      <span className="text-xs text-muted-foreground">allow: {grant.tool_allow.join(", ")}</span>
                    )}
                    {Array.isArray(grant.tool_deny) && grant.tool_deny.length > 0 && (
                      <span className="text-xs text-muted-foreground">deny: {grant.tool_deny.join(", ")}</span>
                    )}
                  </div>
                  <Button variant="ghost" size="sm" onClick={() => handleRevoke(grant)} disabled={loading}>
                    <Trash2 className="h-3.5 w-3.5 text-destructive" />
                  </Button>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Add grant form */}
        <div className="space-y-3 rounded-md border p-3">
          <Label className="text-sm font-medium">Add Agent Grant</Label>
          <div className="grid gap-2">
            <Input
              value={agentId}
              onChange={(e) => setAgentId(e.target.value)}
              placeholder="Agent ID (UUID)"
              className="font-mono text-sm"
            />
            <Input
              value={toolAllow}
              onChange={(e) => setToolAllow(e.target.value)}
              placeholder="Tool allow list (comma-separated, optional)"
              className="text-sm"
            />
            <Input
              value={toolDeny}
              onChange={(e) => setToolDeny(e.target.value)}
              placeholder="Tool deny list (comma-separated, optional)"
              className="text-sm"
            />
          </div>
          <Button size="sm" onClick={handleGrant} disabled={loading || !agentId.trim()} className="gap-1">
            <Plus className="h-3.5 w-3.5" /> Grant
          </Button>
        </div>

        {error && <p className="text-sm text-destructive">{error}</p>}
      </DialogContent>
    </Dialog>
  );
}
