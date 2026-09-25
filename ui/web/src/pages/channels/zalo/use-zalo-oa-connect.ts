import { useEffect, useRef, useState } from "react";
import { useWsCall } from "@/hooks/use-ws-call";

/**
 * extractCode validates the full callback URL returned by Zalo. The callback
 * state is required so an authorization code cannot be rebound to another
 * active consent attempt.
 */
export function extractCode(input: string): { code: string; oaID: string; state: string } {
  const trimmed = input.trim();
  if (!/^https?:\/\//i.test(trimmed)) {
    return { code: "", oaID: "", state: "" };
  }
  try {
    const u = new URL(trimmed);
    return {
      code: (u.searchParams.get("code") ?? "").trim(),
      oaID: (u.searchParams.get("oa_id") ?? "").trim(),
      state: (u.searchParams.get("state") ?? "").trim(),
    };
  } catch {
    return { code: "", oaID: "", state: "" };
  }
}

// Shared state machine for the zalo_oa paste-code consent flow. Consumed
// by the reauth dialog triggered from the credentials tab.

interface ConsentResp {
  url: string;
  state: string;
}

interface ExchangeResp {
  ok: boolean;
  oa_id?: string;
  expires_at?: string;
}

export interface UseZaloOAConnectResult {
  url: string;
  code: string;
  setCode: (c: string) => void;
  state: string;
  copied: boolean;
  done: boolean;
  handleCopy: () => Promise<void>;
  handleOpenPopup: () => void;
  handleOpenInTab: () => void;
  handleSubmit: () => Promise<void>;
  submitting: boolean;
  loadingConsent: boolean;
  consentError: string | null;
  exchangeError: string | null;
  clientErrorKey: string | null; // i18n key; body is responsible for translation
  reset: () => void;
}

/**
 * @param instanceId   Channel-instance UUID to authorize.
 * @param active       Gate state fetching — set to true once the flow is visible
 *                     (dialog open). Avoids racing WS calls while the dialog
 *                     is still mounting.
 * @param onSuccess    Invoked once when exchange completes successfully.
 */
export function useZaloOAConnect(
  instanceId: string,
  active: boolean,
  onSuccess: () => void,
): UseZaloOAConnectResult {
  const consent = useWsCall<ConsentResp>("channels.instances.zalo_oa.consent_url");
  const exchange = useWsCall<ExchangeResp>("channels.instances.zalo_oa.exchange_code");

  const [code, setCode] = useState("");
  const [state, setState] = useState("");
  const [url, setUrl] = useState("");
  const [copied, setCopied] = useState(false);
  const [done, setDone] = useState(false);
  const [clientError, setClientError] = useState<string | null>(null);
  const firedRef = useRef(false);
  const aliveRef = useRef(true);
  const activeRef = useRef(active);
  const instanceIdRef = useRef(instanceId);
  const inFlightRef = useRef(false);
  const popupRef = useRef<Window | null>(null);
  const consentGenerationRef = useRef(0);

  activeRef.current = active;
  instanceIdRef.current = instanceId;

  async function requestConsent() {
    const generation = ++consentGenerationRef.current;
    const requestedInstanceId = instanceIdRef.current;
    const resp = await consent.call({ instance_id: requestedInstanceId });
    if (!aliveRef.current || !activeRef.current) return;
    if (generation !== consentGenerationRef.current) return;
    if (instanceIdRef.current !== requestedInstanceId) return;
    setUrl(resp.url);
    setState(resp.state);
  }

  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
    };
  }, []);

  // Fetch consent URL once the flow becomes active.
  useEffect(() => {
    if (!active || !instanceId) return;
    void requestConsent().catch(() => {
      // error captured on consent.error
    });
    // consent.call identity churns per render; the instanceId+active trigger is intentional
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, instanceId]);

  // Reset state when the flow goes inactive.
  useEffect(() => {
    if (active) return;
    setCode("");
    setState("");
    setUrl("");
    setCopied(false);
    setDone(false);
    setClientError(null);
    firedRef.current = false;
    inFlightRef.current = false;
    consent.reset();
    consentGenerationRef.current += 1;
    exchange.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active]);

  useEffect(() => {
    inFlightRef.current = false;
  }, [instanceId]);

  // Fire onSuccess exactly once when exchange completes. firedRef guards
  // against re-firing if the parent passes a fresh onSuccess closure during
  // the post-success window before reset.
  useEffect(() => {
    if (!done || firedRef.current) return;
    firedRef.current = true;
    onSuccess();
  }, [done, onSuccess]);

  async function handleCopy() {
    if (!url) return;
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard unavailable on http://; user can still copy manually
    }
  }

  function handleOpenInTab() {
    if (!url) return;
    window.open(url, "_blank", "noopener,noreferrer");
  }

  // Popup mode: open the consent URL in a child window. When Zalo lands the
  // popup on the same-origin callback page, the callback page postMessages
  // the full href; we auto-exchange and close the popup. Popup blocked →
  // fall back to a new tab with manual paste.
  function handleOpenPopup() {
    if (!url) return;
    const w = window.open(url, "zalo_oa_consent", "width=520,height=680");
    if (!w) {
      handleOpenInTab();
      return;
    }
    popupRef.current = w;
  }

  async function submitCode(finalCode: string, oaID: string, callbackState: string) {
    if (inFlightRef.current || done) return;
    inFlightRef.current = true;
    const submitInstanceId = instanceIdRef.current;
    setClientError(null);
    try {
      const params: Record<string, unknown> = {
        instance_id: submitInstanceId,
        code: finalCode,
        state: callbackState,
      };
      if (oaID !== "") {
        params.oa_id = oaID;
      }
      const resp = await exchange.call(params);
      if (!aliveRef.current || !activeRef.current) return;
      if (instanceIdRef.current !== submitInstanceId) return;
      if (resp?.ok) setDone(true);
    } catch {
      if (!aliveRef.current || !activeRef.current) return;
      if (instanceIdRef.current !== submitInstanceId) return;
      setCode("");
      setState("");
      setUrl("");
      void requestConsent().catch(() => {
        // refresh error captured on consent.error; exchange.error remains visible
      });
    } finally {
      if (instanceIdRef.current === submitInstanceId) {
        inFlightRef.current = false;
      }
    }
  }

  // Listen for the callback page's postMessage (same-origin only).
  useEffect(() => {
    const onMsg = (e: MessageEvent) => {
      if (!e.data || typeof e.data !== "object") return;
      if (e.data.type !== "zalo-oa-callback") return;
      if (e.origin !== window.location.origin) return; // never trust cross-origin
      const href = typeof e.data.href === "string" ? e.data.href : "";
      if (!href || !state) return;
      const { code: finalCode, oaID, state: callbackState } = extractCode(href);
      if (!finalCode) return;
      if (!callbackState || callbackState !== state) {
        setClientError("zaloOa.errStateMismatch");
        return;
      }
      popupRef.current?.close();
      void submitCode(finalCode, oaID, callbackState);
    };
    window.addEventListener("message", onMsg);
    return () => window.removeEventListener("message", onMsg);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state, instanceId]);

  // Close any open popup when the flow goes inactive.
  useEffect(() => {
    if (active) return;
    popupRef.current?.close();
    popupRef.current = null;
  }, [active]);

  async function handleSubmit() {
    if (!code.trim() || !state) return;
    const { code: finalCode, oaID, state: callbackState } = extractCode(code.trim());
    if (!finalCode) {
      setClientError("zaloOa.errCodeMissing");
      return;
    }
    if (!callbackState || callbackState !== state) {
      setClientError("zaloOa.errStateMismatch");
      return;
    }
    await submitCode(finalCode, oaID, callbackState);
  }

  const setCodeWithReset = (c: string) => {
    setCode(c);
    if (clientError) setClientError(null);
    // Drop stale server-side exchange error while user types the next code.
    if (exchange.error) exchange.reset();
  };

  return {
    url,
    code,
    setCode: setCodeWithReset,
    state,
    copied,
    done,
    handleCopy,
    handleOpenPopup,
    handleOpenInTab,
    handleSubmit,
    submitting: exchange.loading,
    loadingConsent: consent.loading,
    consentError: consent.error?.message ?? null,
    exchangeError: exchange.error?.message ?? null,
    clientErrorKey: clientError,
    reset: () => {
      consent.reset();
      exchange.reset();
      setCode("");
      setState("");
      setUrl("");
      setDone(false);
      setClientError(null);
      firedRef.current = false;
      inFlightRef.current = false;
    },
  };
}
