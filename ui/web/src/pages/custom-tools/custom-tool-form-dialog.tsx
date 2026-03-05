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
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import type { CustomToolData, CustomToolInput } from "./hooks/use-custom-tools";
import { slugify, isValidSlug } from "@/lib/slug";

interface CustomToolFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  tool?: CustomToolData | null;
  onSubmit: (data: CustomToolInput) => Promise<unknown>;
}

export function CustomToolFormDialog({ open, onOpenChange, tool, onSubmit }: CustomToolFormDialogProps) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [command, setCommand] = useState("");
  const [parameters, setParameters] = useState("");
  const [workingDir, setWorkingDir] = useState("");
  const [timeout, setTimeout] = useState(60);
  const [agentId, setAgentId] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (open) {
      setName(tool?.name ?? "");
      setDescription(tool?.description ?? "");
      setCommand(tool?.command ?? "");
      setParameters(tool?.parameters ? JSON.stringify(tool.parameters, null, 2) : "");
      setWorkingDir(tool?.working_dir ?? "");
      setTimeout(tool?.timeout_seconds ?? 60);
      setAgentId(tool?.agent_id ?? "");
      setEnabled(tool?.enabled ?? true);
      setError("");
    }
  }, [open, tool]);

  const handleSubmit = async () => {
    if (!name.trim() || !command.trim()) {
      setError("Name and command are required");
      return;
    }
    if (!isValidSlug(name.trim())) {
      setError("Name must be a valid slug (lowercase letters, numbers, hyphens only)");
      return;
    }

    let parsedParams: Record<string, unknown> | undefined;
    if (parameters.trim()) {
      try {
        parsedParams = JSON.parse(parameters);
      } catch {
        setError("Parameters must be valid JSON");
        return;
      }
    }

    setLoading(true);
    setError("");
    try {
      await onSubmit({
        name: name.trim(),
        description: description.trim(),
        command: command.trim(),
        parameters: parsedParams,
        working_dir: workingDir.trim() || undefined,
        timeout_seconds: timeout,
        agent_id: agentId.trim() || undefined,
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
      <DialogContent className="max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{tool ? "Edit Tool" : "Create Custom Tool"}</DialogTitle>
        </DialogHeader>

        <div className="grid gap-4 py-2 overflow-y-auto min-h-0">
          <div className="grid gap-1.5">
            <Label htmlFor="ct-name">Name *</Label>
            <Input id="ct-name" value={name} onChange={(e) => setName(slugify(e.target.value))} placeholder="my-tool" />
            <p className="text-xs text-muted-foreground">Lowercase letters, numbers, and hyphens only</p>
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="ct-desc">Description</Label>
            <Textarea id="ct-desc" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What this tool does..." rows={2} />
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="ct-cmd">Command *</Label>
            <Textarea
              id="ct-cmd"
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              placeholder={'echo "Hello {{.name}}"'}
              className="font-mono text-sm"
              rows={3}
            />
            <p className="text-xs text-muted-foreground">Shell template. Use {"{{.key}}"} for parameter placeholders.</p>
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="ct-params">Parameters (JSON Schema)</Label>
            <Textarea
              id="ct-params"
              value={parameters}
              onChange={(e) => setParameters(e.target.value)}
              placeholder={'{\n  "type": "object",\n  "properties": { ... }\n}'}
              className="font-mono text-sm"
              rows={4}
            />
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="grid gap-1.5">
              <Label htmlFor="ct-wd">Working Directory</Label>
              <Input id="ct-wd" value={workingDir} onChange={(e) => setWorkingDir(e.target.value)} placeholder="/path/to/dir" />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="ct-timeout">Timeout (seconds)</Label>
              <Input id="ct-timeout" type="number" value={timeout} onChange={(e) => setTimeout(Number(e.target.value))} min={1} />
            </div>
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="ct-agent">Agent ID (optional)</Label>
            <Input id="ct-agent" value={agentId} onChange={(e) => setAgentId(e.target.value)} placeholder="Leave blank for global scope" />
          </div>

          <div className="flex items-center gap-2">
            <Switch id="ct-enabled" checked={enabled} onCheckedChange={setEnabled} />
            <Label htmlFor="ct-enabled">Enabled</Label>
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>Cancel</Button>
          <Button onClick={handleSubmit} disabled={loading}>
            {loading ? "Saving..." : tool ? "Update" : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
