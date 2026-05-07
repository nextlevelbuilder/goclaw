import { useState, useCallback } from "react";
import { useWsCall } from "@/hooks/use-ws-call";
import { useWsEvent } from "@/hooks/use-ws-event";

export type QrStatus = "idle" | "waiting" | "done" | "error";

export function useWeChatQrLogin(instanceId: string | null) {
  const [imgContent, setImgContent] = useState<string | null>(null);
  const [status, setStatus] = useState<QrStatus>("idle");
  const [errorMsg, setErrorMsg] = useState("");
  const { call: startQR, loading } = useWsCall("wechat.qr.start");

  const start = useCallback(async () => {
    if (!instanceId) return;
    setStatus("waiting");
    setImgContent(null);
    setErrorMsg("");
    try {
      await startQR({ instance_id: instanceId });
    } catch (err) {
      setStatus("error");
      setErrorMsg(err instanceof Error ? err.message : "Failed to start QR session");
    }
  }, [startQR, instanceId]);

  const reset = useCallback(() => {
    setStatus("idle");
    setImgContent(null);
    setErrorMsg("");
  }, []);

  useWsEvent(
    "wechat.qr.code",
    useCallback(
      (payload: unknown) => {
        const p = payload as { instance_id: string; img_content: string };
        if (p.instance_id !== instanceId) return;
        setImgContent(p.img_content);
        setStatus("waiting");
      },
      [instanceId],
    ),
  );

  useWsEvent(
    "wechat.qr.done",
    useCallback(
      (payload: unknown) => {
        const p = payload as { instance_id: string; success: boolean; error?: string };
        if (p.instance_id !== instanceId) return;
        if (p.success) {
          setStatus("done");
        } else {
          setStatus("error");
          setErrorMsg(p.error ?? "QR authentication failed");
        }
      },
      [instanceId],
    ),
  );

  return {
    imgContent, status, errorMsg, loading, start, reset, retry: start,
  };
}
