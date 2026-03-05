import { useState } from "react";
import { Copy, Check } from "lucide-react";
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

interface IdentitySectionProps {
  agentKey: string;
  displayName: string;
  onDisplayNameChange: (v: string) => void;
  frontmatter: string;
  onFrontmatterChange: (v: string) => void;
  status: string;
  onStatusChange: (v: string) => void;
  isDefault: boolean;
  onIsDefaultChange: (v: boolean) => void;
}

export function IdentitySection({
  agentKey,
  displayName,
  onDisplayNameChange,
  frontmatter,
  onFrontmatterChange,
  status,
  onStatusChange,
  isDefault,
  onIsDefaultChange,
}: IdentitySectionProps) {
  const [copied, setCopied] = useState(false);

  const copyAgentKey = async () => {
    await navigator.clipboard.writeText(agentKey);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <section className="space-y-4">
      <h3 className="text-sm font-medium text-muted-foreground">Identity</h3>
      <div className="space-y-4 rounded-lg border p-4">
        <div className="space-y-2">
          <Label>Agent Key</Label>
          <div className="flex items-center gap-2">
            <Input value={agentKey} disabled className="font-mono text-sm" />
            <Button
              variant="ghost"
              size="icon"
              className="shrink-0"
              onClick={copyAgentKey}
            >
              {copied ? (
                <Check className="h-3.5 w-3.5 text-green-500" />
              ) : (
                <Copy className="h-3.5 w-3.5" />
              )}
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">
            Unique identifier, cannot be changed.
          </p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="displayName">Display Name</Label>
          <Input
            id="displayName"
            value={displayName}
            onChange={(e) => onDisplayNameChange(e.target.value)}
            placeholder="e.g. My Assistant"
          />
          <p className="text-xs text-muted-foreground">
            Friendly name shown in the UI. Leave empty to use the agent key.
          </p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="frontmatter">Expertise Summary</Label>
          <Input
            id="frontmatter"
            value={frontmatter}
            onChange={(e) => onFrontmatterChange(e.target.value)}
            placeholder="e.g. Chiêm tinh, bói toán, tử vi, thần số học"
          />
          <p className="text-xs text-muted-foreground">
            Short description of this agent's expertise. Used for delegation discovery.
          </p>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label>Status</Label>
            <Select value={status} onValueChange={onStatusChange}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="active">Active</SelectItem>
                <SelectItem value="inactive">Inactive</SelectItem>
                {status === "summon_failed" && (
                  <SelectItem value="summon_failed" disabled>
                    Summon Failed
                  </SelectItem>
                )}
              </SelectContent>
            </Select>
          </div>
          <div className="flex items-end pb-2">
            <div className="flex items-center gap-2">
              <Switch checked={isDefault} onCheckedChange={onIsDefaultChange} />
              <Label>Default Agent</Label>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
