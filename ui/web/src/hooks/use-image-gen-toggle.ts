import { useState, useCallback } from "react";

const STORAGE_KEY_PREFIX = "goclaw.agent.";
const STORAGE_KEY_SUFFIX = ".imageGenEnabled";

function storageKey(agentKey: string): string {
  return `${STORAGE_KEY_PREFIX}${agentKey}${STORAGE_KEY_SUFFIX}`;
}

function readPersisted(agentKey: string): boolean {
  if (!agentKey) return true;
  try {
    const raw = localStorage.getItem(storageKey(agentKey));
    if (raw === null) return true; // default ON
    return raw !== "false";
  } catch {
    // localStorage unavailable (SSR, private-browsing restriction)
    return true;
  }
}

function writePersisted(agentKey: string, enabled: boolean): void {
  if (!agentKey) return;
  try {
    localStorage.setItem(storageKey(agentKey), String(enabled));
  } catch {
    // ignore write failures
  }
}

/**
 * Per-agent image-generation toggle backed by localStorage.
 *
 * Returns the current enabled state for the given agentKey and a stable
 * setter that persists the change. The toggle resets to the stored value
 * (or true by default) whenever agentKey changes.
 */
export function useImageGenToggle(agentKey: string): [boolean, (next: boolean) => void] {
  const [enabled, setEnabled] = useState<boolean>(() => readPersisted(agentKey));

  // Track the last agentKey to reset state on agent change without useEffect.
  // Using a ref-like pattern inside state to avoid stale closures.
  const [lastKey, setLastKey] = useState<string>(agentKey);
  if (agentKey !== lastKey) {
    setLastKey(agentKey);
    setEnabled(readPersisted(agentKey));
  }

  const toggle = useCallback(
    (next: boolean) => {
      setEnabled(next);
      writePersisted(agentKey, next);
    },
    [agentKey],
  );

  return [enabled, toggle];
}
