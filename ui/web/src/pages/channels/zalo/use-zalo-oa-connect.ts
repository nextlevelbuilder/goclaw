import { useEffect, useRef, useState } from "react";
import { useWsCall } from "@/hooks/use-ws-call";

/**
 * extractCode normalizes the paste-code input. Operators can paste either
 * the raw `code` value or the full callback URL Zalo redirected them to
 * (e.g. `https://your-app.com/zalo-callback?oa_id=42...&code=iYP...&state=db8...`).
 * URL parsing runs first — if it looks like an http(s) URL we extract the
 * `code` query param; if absent, return empty code so the caller can show a
 * "URL has no code parameter" hint instead of forwarding the whole URL to
 * the server (which rejected it with an opaque error).
 *
 * When the pasted URL carries a `state` query, we opportunistically compare
 * it to the one we stashed from consent_url (mismatch reported; server is
 * authoritative). When it carries an `oa_id`, we return that so the exchange
 * call can persist it on the channel — without oa_id the channel stays in
 * "awaiting consent" state even after a successful exchange because there's
 * no separate Zalo endpoint to recover it.
 */
export function extractCode(input: string, stashedState: string): { code: string; oaID: string; mismatchedState: boolean } {
  const trimmed = input.trim();
  if (!/^https?:\/\//i.test(trimmed)) {
    return { code: trimmed, oaID: "", mismatchedState: false };
  }
  try {
    const u = new URL(trimmed);
    // URLSearchParams preserves verbatim whitespace; trim each value.
    const code = (u.searchParams.get("code") ?? "").trim();
    const state = (u.searchParams.get("state") ?? "").trim();
    const oaID = (u.searchParams.get("oa_id") ?? "").trim();
    return {
      code,
      oaID,
      mismatchedState: state !== "" && stashedState !== "" && state !== stashedState,
    };
  } catch {
    return { code: trimmed, oaID: "", mismatchedState: false };
  }
}

// Shared state machine for the zalo_oa paste-code consent flow. Consumed
// by both the ReauthDialog (triggered from the row) and the WizardAuthStep
// (auto-triggered after row creation).

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
 *                     (dialog open / wizard step active). Avoids racing WS calls
 *                     while the dialog is still mounting.
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

  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
    };
  }, []);

  // Fetch consent URL once the flow becomes active.
  useEffect(() => {
    if (!active || !instanceId) return;
    let cancelled = false;
    consent
      .call({ instance_id: instanceId })
      .then((resp) => {
        if (cancelled || !aliveRef.current) return;
        setUrl(resp.url);
        setState(resp.state);
      })
      .catch(() => {
        // error captured on consent.error
      });
    return () => {
      cancelled = true;
    };
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
    consent.reset();
    exchange.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active]);

  // Fire onSuccess exactly once when exchange completes. firedRef guards
  // against re-firing if the parent passes a fresh onSuccess closure during
  // the post-success window before reset (done stays true ~1.5s while the
  // success view is visible).
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

  async function handleSubmit() {
    if (!code.trim() || !state) return;
    // mismatchedState is intentionally ignored client-side: the server
    // re-validates state on exchange_code, and surfacing it here confuses
    // operators on legit flows where Zalo mangles the redirect but still
    // returns a valid code.
    const { code: finalCode, oaID } = extractCode(code.trim(), state);
    if (!finalCode) {
      setClientError("zaloOa.errCodeMissing");
      return;
    }
    setClientError(null);
    try {
      const params: Record<string, unknown> = {
        instance_id: instanceId,
        code: finalCode,
        state,
      };
      if (oaID !== "") {
        params.oa_id = oaID;
      }
      const resp = await exchange.call(params);
      if (!aliveRef.current) return;
      if (resp?.ok) setDone(true);
    } catch {
      // error captured on exchange.error
    }
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
    },
  };
}
