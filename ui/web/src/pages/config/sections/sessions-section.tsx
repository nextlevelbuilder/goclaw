import { useState, useEffect } from "react";
import { Save } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { InfoLabel } from "@/components/shared/info-label";

interface SessionsData {
  storage?: string;
  scope?: string;
  dm_scope?: string;
  main_key?: string;
}

const DEFAULT: SessionsData = {};

interface Props {
  data: SessionsData | undefined;
  onSave: (value: SessionsData) => Promise<void>;
  saving: boolean;
}

export function SessionsSection({ data, onSave, saving }: Props) {
  const [draft, setDraft] = useState<SessionsData>(data ?? DEFAULT);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? DEFAULT);
    setDirty(false);
  }, [data]);

  const update = (patch: Partial<SessionsData>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setDirty(true);
  };

  if (!data) return null;

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">Sessions</CardTitle>
        <CardDescription>Session scoping settings. Sessions are stored in PostgreSQL.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div className="grid gap-1.5">
            <InfoLabel tip="Session isolation level. Per Sender = each user gets their own session. Global = all users share one session.">Scope</InfoLabel>
            <Select value={draft.scope ?? "per-sender"} onValueChange={(v) => update({ scope: v })}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="per-sender">Per Sender</SelectItem>
                <SelectItem value="global">Global</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip="How DM sessions are scoped. Controls session isolation for direct messages across different channels and accounts.">DM Scope</InfoLabel>
            <Select value={draft.dm_scope ?? "per-channel-peer"} onValueChange={(v) => update({ dm_scope: v })}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="main">Main</SelectItem>
                <SelectItem value="per-peer">Per Peer</SelectItem>
                <SelectItem value="per-channel-peer">Per Channel Peer</SelectItem>
                <SelectItem value="per-account-channel-peer">Per Account Channel Peer</SelectItem>
              </SelectContent>
            </Select>
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
