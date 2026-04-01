import { useState, useEffect } from "react";
import { Share, Plus, Trash2, Loader2, RefreshCw, Info } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { UserPickerCombobox } from "@/components/shared/user-picker-combobox";
import { useAgentShares } from "../hooks/use-agent-shares";

interface AgentSharesTabProps {
  agentId: string;
  isOwner: boolean;
}

export function AgentSharesTab({ agentId, isOwner }: AgentSharesTabProps) {
  const { t } = useTranslation("agents");
  const { shares, loading, load, share, revoke } = useAgentShares(agentId);
  const [userId, setUserId] = useState("");
  const [role, setRole] = useState("user");
  const [adding, setAdding] = useState(false);
  const [revokingId, setRevokingId] = useState<string | null>(null);

  useEffect(() => { if (isOwner) load(); }, [load, isOwner]);

  const handleRevoke = async (uid: string) => {
    setRevokingId(uid);
    try { await revoke(uid); } finally { setRevokingId(null); }
  };

  const handleAdd = async () => {
    if (!userId.trim()) return;
    setAdding(true);
    try {
      await share(userId.trim(), role);
      setUserId("");
    } finally { setAdding(false); }
  };

  if (!isOwner) {
    return (
      <div className="space-y-4">
        <div className="flex items-start gap-3 rounded-lg border border-sky-200 bg-sky-500/5 p-4 dark:border-sky-800">
          <Info className="mt-0.5 h-5 w-5 shrink-0 text-sky-600 dark:text-sky-400" />
          <p className="text-sm text-muted-foreground">{t("shares.ownerOnly")}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-start justify-between gap-2">
        <div>
          <h3 className="text-sm font-medium flex items-center gap-2">
            <Share className="h-4 w-4 text-amber-500" />
            {t("shares.grantAccess")}
          </h3>
          <p className="text-xs text-muted-foreground mt-1">{t("shares.noSharesDesc")}</p>
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 w-7 p-0 shrink-0 text-muted-foreground"
          onClick={load}
          disabled={loading}
        >
          {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
        </Button>
      </div>

      {/* Add form */}
      <div className="space-y-2">
        <div className="flex flex-wrap items-end gap-2">
          <UserPickerCombobox
            value={userId}
            onChange={setUserId}
            placeholder={t("shares.userIdPlaceholder")}
            className="flex-1 min-w-[160px]"
          />
          <Select value={role} onValueChange={setRole}>
            <SelectTrigger className="w-[130px] text-base md:text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="user">{t("shares.role.user")}</SelectItem>
              <SelectItem value="viewer">{t("shares.role.viewer")}</SelectItem>
            </SelectContent>
          </Select>
          <Button
            size="icon"
            className="h-9 w-9 shrink-0"
            onClick={handleAdd}
            disabled={adding || !userId.trim()}
          >
            {adding ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">
          {role === "user" ? t("shares.roleDesc.user") : t("shares.roleDesc.viewer")}
        </p>
      </div>

      {/* Shares list */}
      {loading && shares.length === 0 ? (
        <div className="flex items-center justify-center py-8">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      ) : shares.length === 0 ? (
        <p className="text-xs text-muted-foreground text-center py-6">{t("shares.noShares")}</p>
      ) : (
        <div className="rounded-lg border divide-y">
          {shares.map((s) => (
            <div key={s.id} className="flex items-center justify-between gap-2 px-3 py-2">
              <div className="flex items-center gap-2 min-w-0 text-sm">
                <Badge variant="secondary" className="text-[10px] shrink-0">
                  {s.role}
                </Badge>
                <span className="font-medium truncate">{s.user_id}</span>
                {s.granted_by && (
                  <span className="text-[11px] text-muted-foreground shrink-0">
                    by {s.granted_by}
                  </span>
                )}
                {s.created_at && (
                  <span className="text-[11px] text-muted-foreground shrink-0">
                    {new Date(s.created_at).toLocaleDateString()}
                  </span>
                )}
              </div>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-7 p-0 shrink-0 text-muted-foreground hover:text-destructive"
                onClick={() => handleRevoke(s.user_id)}
                disabled={revokingId === s.user_id}
              >
                {revokingId === s.user_id
                  ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  : <Trash2 className="h-3.5 w-3.5" />}
              </Button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
