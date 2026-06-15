import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Play, AlertTriangle } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { toast } from "@/stores/use-toast-store";
import type {
  WebhookData,
  WebhookTestInput,
  WebhookTestResult,
  WebhookTestLLMResult,
  WebhookTestMessageResult,
} from "@/types/webhook";

interface Props {
  webhook: WebhookData | null;
  onClose: () => void;
  onRun: (id: string, body: WebhookTestInput) => Promise<WebhookTestResult>;
}

function isLLMResult(r: WebhookTestResult): r is WebhookTestLLMResult {
  return "output" in r;
}

export function WebhookTestDialog({ webhook, onClose, onRun }: Props) {
  const { t } = useTranslation("webhooks");
  const [input, setInput] = useState("");
  const [channelName, setChannelName] = useState("");
  const [chatId, setChatId] = useState("");
  const [content, setContent] = useState("");
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<WebhookTestResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (webhook) {
      setInput("");
      setChannelName("");
      setChatId("");
      setContent("");
      setResult(null);
      setError(null);
    }
  }, [webhook]);

  const isMessage = webhook?.kind === "message";
  const hasBoundChannel = !!webhook?.channel_id;

  const handleRun = async () => {
    if (!webhook) return;
    setRunning(true);
    setResult(null);
    setError(null);
    try {
      const body: WebhookTestInput = isMessage
        ? {
            channel_name: hasBoundChannel ? undefined : channelName.trim(),
            chat_id: chatId.trim(),
            content: content.trim(),
          }
        : { input: input.trim() };
      const res = await onRun(webhook.id, body);
      setResult(res);
    } catch (err) {
      const msg = err instanceof Error ? err.message : t("test.failed");
      setError(msg);
      toast.error(t("test.failed"), msg);
    } finally {
      setRunning(false);
    }
  };

  const canRun = isMessage
    ? chatId.trim() !== "" && content.trim() !== "" && (hasBoundChannel || channelName.trim() !== "")
    : input.trim() !== "";

  return (
    <Dialog open={!!webhook} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("test.title", { name: webhook?.name ?? "" })}</DialogTitle>
          <DialogDescription>{t("test.description")}</DialogDescription>
        </DialogHeader>

        {isMessage && (
          <div className="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-sm">
            <AlertTriangle className="h-4 w-4 text-amber-500 shrink-0 mt-0.5" />
            <span>{t("test.messageWarning")}</span>
          </div>
        )}

        <div className="space-y-4">
          {isMessage ? (
            <>
              {!hasBoundChannel && (
                <div className="space-y-1.5">
                  <Label htmlFor="test-channel">{t("test.channelName")}</Label>
                  <Input
                    id="test-channel"
                    value={channelName}
                    onChange={(e) => setChannelName(e.target.value)}
                    placeholder={t("test.channelNamePlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
              )}
              <div className="space-y-1.5">
                <Label htmlFor="test-chat">{t("test.chatId")}</Label>
                <Input
                  id="test-chat"
                  value={chatId}
                  onChange={(e) => setChatId(e.target.value)}
                  placeholder={t("test.chatIdPlaceholder")}
                  className="text-base md:text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="test-content">{t("test.content")}</Label>
                <Textarea
                  id="test-content"
                  value={content}
                  onChange={(e) => setContent(e.target.value)}
                  rows={3}
                  className="text-base md:text-sm"
                />
              </div>
            </>
          ) : (
            <div className="space-y-1.5">
              <Label htmlFor="test-input">{t("test.input")}</Label>
              <Textarea
                id="test-input"
                value={input}
                onChange={(e) => setInput(e.target.value)}
                placeholder={t("test.inputPlaceholder")}
                rows={4}
                className="text-base md:text-sm"
              />
            </div>
          )}

          {error && (
            <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive break-words">
              {error}
            </div>
          )}

          {result && (
            <div className="space-y-1.5">
              <Label>{t("test.result")}</Label>
              {isLLMResult(result) ? (
                <pre className="max-h-60 overflow-auto rounded bg-muted px-3 py-2 text-xs whitespace-pre-wrap break-words">
                  {result.output}
                </pre>
              ) : (
                <div className="rounded bg-muted px-3 py-2 text-sm">
                  {t("test.sent", {
                    channel: (result as WebhookTestMessageResult).channel_name,
                    chatId: (result as WebhookTestMessageResult).chat_id,
                  })}
                  {(result as WebhookTestMessageResult).warning && (
                    <p className="text-xs text-amber-600 mt-1">
                      {(result as WebhookTestMessageResult).warning}
                    </p>
                  )}
                </div>
              )}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>
            {t("test.close")}
          </Button>
          <Button type="button" onClick={handleRun} disabled={running || !canRun} className="gap-1">
            <Play className="h-3.5 w-3.5" />
            {running ? t("test.running") : t("test.run")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
