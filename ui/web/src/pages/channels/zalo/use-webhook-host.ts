import { useEffect, useState } from "react";
import { useAuthStore } from "@/stores/use-auth-store";

const STORAGE_KEY_BASE = "goclaw.zalo.webhook_host";

function storageKey(tenantId: string): string {
  return tenantId ? `${STORAGE_KEY_BASE}.${tenantId}` : STORAGE_KEY_BASE;
}

function defaultHost(): string {
  if (typeof window === "undefined") return "";
  return window.location.origin;
}

/**
 * Persist a per-browser, per-tenant override for the gateway host that
 * operators paste into Zalo's dev console. Falls back to
 * window.location.origin when no override is stored.
 */
export function useWebhookHost(): [string, (next: string) => void] {
  const tenantId = useAuthStore((s) => s.tenantId);
  const key = storageKey(tenantId);

  const [host, setHost] = useState<string>(() => {
    if (typeof window === "undefined") return "";
    if (!tenantId) return defaultHost();
    return window.localStorage.getItem(key) ?? defaultHost();
  });

  useEffect(() => {
    if (typeof window === "undefined") return;
    if (!tenantId) return;
    setHost(window.localStorage.getItem(key) ?? defaultHost());
  }, [key, tenantId]);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const trimmed = host.trim();
    if (!trimmed || trimmed === defaultHost()) {
      window.localStorage.removeItem(key);
      return;
    }
    if (!isValidHttpURL(trimmed)) {
      return;
    }
    window.localStorage.setItem(key, trimmed);
  }, [host, key]);

  return [host, setHost];
}

function isValidHttpURL(value: string): boolean {
  try {
    const u = new URL(value);
    return u.protocol === "http:" || u.protocol === "https:";
  } catch {
    return false;
  }
}
