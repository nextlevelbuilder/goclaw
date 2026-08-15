import { useTranslation } from "react-i18next";
import {
  Ban,
  Bot,
  Check,
  Copy,
  History,
  KeyRound,
  Pencil,
  Play,
  Radio,
  Webhook as WebhookIcon,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatRelativeTime } from "@/lib/format";
import type { WebhookData } from "@/types/webhook";

interface Props {
  webhooks: WebhookData[];
  copiedId: string | null;
  onCopyId: (id: string) => void;
  agentName: (id?: string) => string;
  channelName: (id?: string) => string;
  onTest: (webhook: WebhookData) => void;
  onCalls: (webhook: WebhookData) => void;
  onEdit: (webhook: WebhookData) => void;
  onRotate: (webhook: WebhookData) => void;
  onRevoke: (webhook: WebhookData) => void;
}

export function WebhookListTable({
  webhooks,
  copiedId,
  onCopyId,
  agentName,
  channelName,
  onTest,
  onCalls,
  onEdit,
  onRotate,
  onRevoke,
}: Props) {
  const { t } = useTranslation("webhooks");

  return (
    <div className="overflow-x-auto rounded-md border">
      <table className="w-full min-w-[800px] text-sm">
        <thead>
          <tr className="border-b bg-muted/50">
            <th className="px-4 py-3 text-left font-medium">{t("columns.name")}</th>
            <th className="px-4 py-3 text-left font-medium">{t("columns.webhookId")}</th>
            <th className="px-4 py-3 text-left font-medium">{t("columns.kind")}</th>
            <th className="px-4 py-3 text-left font-medium">{t("columns.target")}</th>
            <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
            <th className="px-4 py-3 text-left font-medium">{t("columns.lastUsed")}</th>
            <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
          </tr>
        </thead>
        <tbody>
          {webhooks.map((w) => (
            <tr key={w.id} className={`border-b last:border-0 hover:bg-muted/30 ${w.revoked ? "opacity-50" : ""}`}>
              <td className="px-4 py-3">
                <div className="flex items-center gap-2">
                  <WebhookIcon className="h-4 w-4 text-muted-foreground shrink-0 mt-0.5" />
                  <div>
                    <div className="font-medium">{w.name}</div>
                    <code className="text-xs-plus text-muted-foreground font-mono">{w.secret_prefix}...</code>
                  </div>
                </div>
              </td>
              <td className="px-4 py-3">
                <div className="flex items-center gap-1">
                  <code className="text-xs font-mono text-muted-foreground" title={w.id}>
                    {w.id.slice(0, 8)}...
                  </code>
                  <button
                    onClick={() => onCopyId(w.id)}
                    className="ml-1 rounded p-0.5 hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                    title={w.id}
                  >
                    {copiedId === w.id ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
                  </button>
                </div>
              </td>
              <td className="px-4 py-3">
                <Badge variant="secondary" className="text-xs">{t(`kind.${w.kind}`)}</Badge>
              </td>
              <td className="px-4 py-3 text-muted-foreground">
                {w.kind === "llm" && w.agent_id && (
                  <span className="inline-flex items-center gap-1">
                    <Bot className="h-3.5 w-3.5" />
                    {agentName(w.agent_id)}
                  </span>
                )}
                {w.kind === "message" && w.channel_id && (
                  <span className="inline-flex items-center gap-1">
                    <Radio className="h-3.5 w-3.5" />
                    {channelName(w.channel_id)}
                  </span>
                )}
                {!w.agent_id && !w.channel_id && "-"}
              </td>
              <td className="px-4 py-3">
                {w.revoked ? (
                  <Badge variant="destructive" className="text-xs">{t("status.revoked")}</Badge>
                ) : (
                  <Badge variant="default" className="text-xs">{t("status.active")}</Badge>
                )}
              </td>
              <td className="px-4 py-3 text-muted-foreground" title={w.last_used_at ? new Date(w.last_used_at).toLocaleString() : undefined}>
                {w.last_used_at ? formatRelativeTime(w.last_used_at) : t("neverUsed")}
              </td>
              <td className="px-4 py-3 text-right">
                <div className="flex items-center justify-end gap-1">
                  {!w.revoked && (w.kind === "llm" || w.kind === "message") && (
                    <Button variant="ghost" size="sm" onClick={() => onTest(w)} title={t("actions.test")}>
                      <Play className="h-3.5 w-3.5" />
                    </Button>
                  )}
                  <Button variant="ghost" size="sm" onClick={() => onCalls(w)} title={t("actions.calls")}>
                    <History className="h-3.5 w-3.5" />
                  </Button>
                  {!w.revoked && (
                    <>
                      <Button variant="ghost" size="sm" onClick={() => onEdit(w)} title={t("actions.edit")}>
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                      <Button variant="ghost" size="sm" onClick={() => onRotate(w)} title={t("actions.rotate")}>
                        <KeyRound className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => onRevoke(w)}
                        className="text-destructive hover:text-destructive"
                        title={t("actions.revoke")}
                      >
                        <Ban className="h-3.5 w-3.5" />
                      </Button>
                    </>
                  )}
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
