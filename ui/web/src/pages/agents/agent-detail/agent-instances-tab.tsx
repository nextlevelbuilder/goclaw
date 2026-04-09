import { useState, useEffect, useMemo } from "react";
import { Save, Loader2, Users, FileText } from "lucide-react";
import { toast } from "@/stores/use-toast-store";
import { userFriendlyError } from "@/lib/error-utils";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Textarea } from "@/components/ui/textarea";
import { useContactResolver } from "@/hooks/use-contact-resolver";
import { useAgentInstances, type UserInstance } from "../hooks/use-agent-instances";
import { UserPickerCombobox } from "@/components/shared/user-picker-combobox";

interface AgentInstancesTabProps {
  agentId: string;
}

export function AgentInstancesTab({ agentId }: AgentInstancesTabProps) {
  const { t } = useTranslation("agents");
  const { instances, loading, saving, getFiles, setFile } = useAgentInstances(agentId);
  const [selected, setSelected] = useState<string | null>(null);
  const [content, setContent] = useState("");
  const [originalContent, setOriginalContent] = useState("");
  const [loadingFiles, setLoadingFiles] = useState(false);

  useEffect(() => {
    if (!selected) return;
    let cancelled = false;
    setLoadingFiles(true);
    getFiles(selected).then((files) => {
      if (cancelled) return;
      const userFile = files.find((f) => f.file_name === "USER.md");
      const c = userFile?.content ?? "";
      setContent(c);
      setOriginalContent(c);
    }).catch((err) => {
      if (!cancelled) toast.error(t("instances.loading"), userFriendlyError(err));
    }).finally(() => {
      if (!cancelled) setLoadingFiles(false);
    });
    return () => { cancelled = true; };
  }, [selected, getFiles, t]);

  const handleSave = async () => {
    if (!selected) return;
    try {
      await setFile(selected, "USER.md", content);
      setOriginalContent(content);
    } catch {
      // toast shown by hook
    }
  };

  const isDirty = content !== originalContent;

  // Resolve user_ids to contact names for instances without metadata
  const instanceUserIDs = useMemo(() => instances.map((i) => i.user_id), [instances]);
  const { resolve } = useContactResolver(instanceUserIDs);

  // Existing instance user_ids for deduplication
  const existingIDs = useMemo(() => new Set(instances.map((i) => i.user_id)), [instances]);

  const [addUserId, setAddUserId] = useState("");

  const handleAddUser = (val: string) => {
    setAddUserId(val);
    if (val && !existingIDs.has(val)) {
      setSelected(val);
    }
  };

  if (loading) {
    return <div className="py-8 text-center text-sm text-muted-foreground">{t("instances.loadingInstances")}</div>;
  }

  return (
    <div className="flex gap-4" style={{ minHeight: 400 }}>
      {/* Instance list */}
      <div className="w-64 shrink-0 space-y-1 overflow-y-auto rounded-md border p-2">
        <div className="px-1 pb-2">
          <UserPickerCombobox
            value={addUserId}
            onChange={handleAddUser}
            placeholder={t("instances.searchContacts")}
            className="w-full"
          />
        </div>
        {instances.length > 0 && (
          <div className="px-2 pb-1 pt-1 text-xs font-medium text-muted-foreground">
            {instances.length} instance{instances.length !== 1 ? "s" : ""}
          </div>
        )}
        {instances.length === 0 && (
          <div className="flex flex-col items-center gap-2 py-6 text-center">
            <Users className="h-6 w-6 text-muted-foreground/50" />
            <p className="text-xs text-muted-foreground">{t("instances.noInstances")}</p>
          </div>
        )}
        {instances.map((inst) => (
          <InstanceRow
            key={inst.user_id}
            instance={inst}
            isSelected={selected === inst.user_id}
            onClick={() => setSelected(inst.user_id)}
            resolve={resolve}
          />
        ))}
      </div>

      {/* Editor */}
      <div className="flex flex-1 flex-col gap-3">
        {!selected ? (
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
            {t("instances.selectInstance")}
          </div>
        ) : loadingFiles ? (
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
            {t("instances.loading")}
          </div>
        ) : (
          <>
            <div className="flex items-center gap-2">
              <FileText className="h-4 w-4 text-muted-foreground" />
              <span className="text-sm font-medium">USER.md</span>
              <span className="text-xs text-muted-foreground">— {selected}</span>
            </div>
            <Textarea
              className="flex-1 font-mono text-sm"
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder="(empty)"
              style={{ minHeight: 300 }}
            />
            <div className="flex items-center justify-end gap-2">
              <Button onClick={handleSave} disabled={saving || !isDirty} size="sm">
                {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                {saving ? t("instances.saving") : t("instances.save")}
              </Button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}


function InstanceRow({ instance, isSelected, onClick, resolve }: { instance: UserInstance; isSelected: boolean; onClick: () => void; resolve: (id: string) => import("@/types/contact").ChannelContact | null }) {
  const lastSeen = instance.last_seen_at ? formatRelative(instance.last_seen_at) : null;
  const contact = resolve(instance.user_id);
  const displayName = instance.metadata?.display_name || instance.metadata?.chat_title || contact?.display_name || null;

  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors ${
        isSelected ? "bg-accent text-accent-foreground" : "hover:bg-muted/50"
      }`}
    >
      <span className="truncate text-xs font-medium">
        {displayName || instance.user_id}
      </span>
      {displayName && (
        <span className="truncate font-mono text-2xs text-muted-foreground">{instance.user_id}</span>
      )}
      <div className="flex items-center gap-2">
        {instance.file_count > 0 && (
          <Badge variant="outline" className="text-2xs">
            {instance.file_count} file{instance.file_count !== 1 ? "s" : ""}
          </Badge>
        )}
        {lastSeen && (
          <span className="text-2xs text-muted-foreground">{lastSeen}</span>
        )}
      </div>
    </button>
  );
}

function formatRelative(iso: string): string {
  const d = new Date(iso);
  const now = Date.now();
  const diff = now - d.getTime();
  if (diff < 60_000) return "just now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  if (diff < 604_800_000) return `${Math.floor(diff / 86_400_000)}d ago`;
  return d.toLocaleDateString();
}
