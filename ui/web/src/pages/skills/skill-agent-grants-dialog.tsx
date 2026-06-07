import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Trash2, ShieldCheck } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import type { SkillAgentGrant, SkillInfo } from "@/types/skill";

interface SkillAgentGrantsDialogProps {
  skill: SkillInfo;
  onClose: () => void;
  onLoad: (skillId: string) => Promise<SkillAgentGrant[]>;
  onGrant: (skillId: string, agentId: string, version: number, canManage: boolean) => Promise<void>;
  onGrantAll: (skillId: string, agentIds: string[], version: number, canManage: boolean) => Promise<void>;
  onRevoke: (skillId: string, agentId: string) => Promise<void>;
}

export function SkillAgentGrantsDialog({
  skill,
  onClose,
  onLoad,
  onGrant,
  onGrantAll,
  onRevoke,
}: SkillAgentGrantsDialogProps) {
  const { t } = useTranslation("skills");
  const { agents } = useAgents();
  const [grants, setGrants] = useState<SkillAgentGrant[]>([]);
  const [agentId, setAgentId] = useState("");
  const [canManage, setCanManage] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!skill.id) return;
    setLoading(true);
    setError("");
    onLoad(skill.id)
      .then(setGrants)
      .catch((err) => setError(err instanceof Error ? err.message : t("grants.loadFailed")))
      .finally(() => setLoading(false));
  }, [skill.id, onLoad, t]);

  const agentNames = useMemo(() => {
    const map = new Map<string, string>();
    for (const agent of agents) map.set(agent.id, agent.display_name || agent.agent_key);
    return map;
  }, [agents]);

  const selectedGrant = grants.find((grant) => grant.agent_id === agentId);

  useEffect(() => {
    setCanManage(selectedGrant?.can_manage ?? false);
  }, [selectedGrant]);

  const handleGrant = async () => {
    if (!skill.id || !agentId) return;
    setLoading(true);
    setError("");
    try {
      await onGrant(skill.id, agentId, skill.version ?? 1, canManage);
      setGrants((current) => {
        const next: SkillAgentGrant = {
          agent_id: agentId,
          pinned_version: skill.version ?? 1,
          granted_by: "",
          can_manage: canManage,
        };
        if (current.some((grant) => grant.agent_id === agentId)) {
          return current.map((grant) => (grant.agent_id === agentId ? next : grant));
        }
        return [...current, next];
      });
      setAgentId("");
      setCanManage(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("grants.saveFailed"));
    } finally {
      setLoading(false);
    }
  };

  const handleGrantAll = async () => {
    if (!skill.id || agents.length === 0) return;
    setLoading(true);
    setError("");
    try {
      await onGrantAll(skill.id, agents.map((agent) => agent.id), skill.version ?? 1, canManage);
      setGrants(agents.map((agent) => ({
        agent_id: agent.id,
        agent_key: agent.agent_key,
        display_name: agent.display_name,
        pinned_version: skill.version ?? 1,
        granted_by: "",
        can_manage: canManage,
      })));
      setAgentId("");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("grants.saveFailed"));
    } finally {
      setLoading(false);
    }
  };

  const handleRevoke = async (grant: SkillAgentGrant) => {
    if (!skill.id) return;
    setLoading(true);
    setError("");
    try {
      await onRevoke(skill.id, grant.agent_id);
      setGrants((current) => current.filter((item) => item.agent_id !== grant.agent_id));
      if (agentId === grant.agent_id) setAgentId("");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("grants.revokeFailed"));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("grants.title", { name: skill.name })}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4 overflow-y-auto min-h-0 pr-1">
          <div className="space-y-2">
            <Label>{t("grants.current")}</Label>
            {grants.length === 0 ? (
              <p className="rounded-md border px-3 py-4 text-sm text-muted-foreground">{t("grants.none")}</p>
            ) : (
              <div className="divide-y rounded-md border">
                {grants.map((grant) => (
                  <div key={grant.agent_id} className="flex items-center justify-between gap-3 px-3 py-2.5">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">{grant.display_name || grant.agent_key || agentNames.get(grant.agent_id) || grant.agent_id}</p>
                      <div className="mt-1 flex items-center gap-1.5">
                        <Badge variant="secondary" className="text-2xs">v{grant.pinned_version}</Badge>
                        {grant.can_manage && (
                          <Badge variant="outline" className="text-2xs border-emerald-500 text-emerald-600">
                            {t("grants.canManage")}
                          </Badge>
                        )}
                      </div>
                    </div>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8"
                      disabled={loading}
                      aria-label={t("grants.revoke")}
                      title={t("grants.revoke")}
                      onClick={() => handleRevoke(grant)}
                    >
                      <Trash2 className="h-4 w-4 text-destructive" />
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="space-y-3 rounded-md border p-3">
            <Label>{selectedGrant ? t("grants.update") : t("grants.add")}</Label>
            <Select value={agentId} onValueChange={setAgentId}>
              <SelectTrigger>
                <SelectValue placeholder={t("grants.selectAgent")} />
              </SelectTrigger>
              <SelectContent>
                {agents.map((agent) => (
                  <SelectItem key={agent.id} value={agent.id}>
                    {agent.display_name || agent.agent_key}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <label className="flex items-center justify-between gap-3 rounded-md border px-3 py-2">
              <span className="flex min-w-0 items-center gap-2 text-sm">
                <ShieldCheck className="h-4 w-4 text-emerald-600" />
                {t("grants.allowManage")}
              </span>
              <Switch checked={canManage} onCheckedChange={setCanManage} />
            </label>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" onClick={handleGrant} disabled={loading || !agentId} className="gap-1">
                <Plus className="h-3.5 w-3.5" />
                {selectedGrant ? t("grants.save") : t("grants.grant")}
              </Button>
              <Button size="sm" variant="outline" onClick={handleGrantAll} disabled={loading || agents.length === 0}>
                {t("grants.grantAllAgents")}
              </Button>
            </div>
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
      </DialogContent>
    </Dialog>
  );
}
