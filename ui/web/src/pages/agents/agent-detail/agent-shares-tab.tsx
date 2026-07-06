import { useState, useCallback, useMemo } from "react";
import { Plus, Trash2, Users } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Combobox } from "@/components/ui/combobox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useHttp } from "@/hooks/use-ws";
import { useContactPicker } from "@/hooks/use-contact-picker";
import { useContactResolver } from "@/hooks/use-contact-resolver";
import { useAgentShares } from "../hooks/use-agent-shares";
import type { ChannelContact } from "@/types/contact";

interface AgentSharesTabProps {
  agentId: string;
}

const ROLE_OPTIONS = [
  { value: "user" },
  { value: "viewer" },
] as const;

function roleBadgeVariant(role: string) {
  switch (role) {
    case "owner": return "success" as const;
    case "user": return "info" as const;
    default: return "outline" as const;
  }
}

export function AgentSharesTab({ agentId }: AgentSharesTabProps) {
  const { t } = useTranslation("agents");
  const http = useHttp();
  const { shares, loading, addShare, revokeShare } = useAgentShares(agentId);
  const [newUserId, setNewUserId] = useState("");
  const [newRole, setNewRole] = useState("user");
  const [revokeTarget, setRevokeTarget] = useState<string | null>(null);

  // Contact picker for the add form
  const listContacts = useCallback(
    async (search: string): Promise<ChannelContact[]> => {
      const res = await http.get<{ contacts: ChannelContact[] }>("/v1/contacts", {
        search,
        limit: "20",
      });
      return res.contacts ?? [];
    },
    [http],
  );
  const { options, searchContacts } = useContactPicker(listContacts);

  // Resolve display names for existing shares
  const shareUserIDs = useMemo(() => shares.map((s) => s.user_id), [shares]);
  const { resolve } = useContactResolver(shareUserIDs);

  const handleUserIdChange = (val: string) => {
    setNewUserId(val);
    searchContacts(val);
  };

  const handleAddShare = async () => {
    if (!newUserId.trim()) return;
    try {
      await addShare(newUserId.trim(), newRole);
      setNewUserId("");
      setNewRole("user");
    } catch {
      // ignore
    }
  };

  return (
    <div className="max-w-2xl space-y-6">
      {/* Add share form */}
      <div className="rounded-lg border p-4">
        <h3 className="mb-3 text-sm font-medium">{t("shares.grantAccess")}</h3>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div className="flex-1 space-y-1.5">
            <Label htmlFor="shareUserId">{t("shares.userId")}</Label>
            <Combobox
              value={newUserId}
              onChange={handleUserIdChange}
              options={options}
              placeholder={t("shares.userIdPlaceholder")}
            />
          </div>
          <div className="w-full space-y-1.5 sm:w-36">
            <Label>{t("shares.role")}</Label>
            <Select value={newRole} onValueChange={setNewRole}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ROLE_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {t(`shares.role.${opt.value}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Button onClick={handleAddShare} disabled={!newUserId.trim()} className="gap-1.5">
            <Plus className="h-4 w-4" />
            {t("shares.share")}
          </Button>
        </div>
      </div>

      {/* Share list */}
      {loading && shares.length === 0 ? (
        <div className="py-8 text-center text-sm text-muted-foreground">{t("shares.loadingShares")}</div>
      ) : shares.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-8 text-center">
          <Users className="h-8 w-8 text-muted-foreground/50" />
          <p className="text-sm text-muted-foreground">{t("shares.noShares")}</p>
          <p className="text-xs text-muted-foreground">
            {t("shares.noSharesDesc")}
          </p>
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <div className="grid min-w-[300px] grid-cols-[1fr_100px_48px] items-center gap-2 border-b bg-muted/50 px-4 py-2.5 text-xs font-medium text-muted-foreground">
            <span>{t("shares.user")}</span>
            <span>{t("shares.role")}</span>
            <span />
          </div>
          {shares.map((share) => {
            const contact = resolve(share.user_id);
            const contactByGrant = share.granted_by ? resolve(share.granted_by) : null;
            return (
              <div
                key={share.user_id}
                className="grid min-w-[300px] grid-cols-[1fr_100px_48px] items-center gap-2 border-b px-4 py-3 last:border-0"
              >
                <div>
                  <span className="text-sm font-medium">
                    {contact?.display_name ?? share.user_id}
                  </span>
                  {contact?.display_name && (
                    <span className="ml-1.5 text-xs text-muted-foreground font-mono">
                      {share.user_id}
                    </span>
                  )}
                  {contact?.username && (
                    <span className="ml-1.5 text-xs text-muted-foreground">
                      @{contact.username}
                    </span>
                  )}
                  {share.granted_by && (
                    <span className="ml-2 text-xs text-muted-foreground">
                      by {contactByGrant?.display_name ?? share.granted_by}
                    </span>
                  )}
                </div>
                <Badge variant={roleBadgeVariant(share.role)}>{share.role}</Badge>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8"
                  onClick={() => setRevokeTarget(share.user_id)}
                >
                  <Trash2 className="h-3.5 w-3.5 text-destructive" />
                </Button>
              </div>
            );
          })}
        </div>
      )}

      <ConfirmDialog
        open={!!revokeTarget}
        onOpenChange={() => setRevokeTarget(null)}
        title={t("shares.revokeTitle")}
        description={t("shares.revokeDesc", { userId: revokeTarget })}
        confirmLabel={t("shares.revoke")}
        variant="destructive"
        onConfirm={async () => {
          if (revokeTarget) {
            try {
              await revokeShare(revokeTarget);
            } catch {
              // ignore
            }
            setRevokeTarget(null);
          }
        }}
      />
    </div>
  );
}
