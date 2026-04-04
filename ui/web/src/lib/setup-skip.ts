import { LOCAL_STORAGE_KEYS } from "@/lib/constants";

const SETUP_SKIPPED_PREFIX = `${LOCAL_STORAGE_KEYS.SETUP_SKIPPED}:`;

interface SetupSkipScope {
  tenantId?: string;
  tenantSlug?: string;
  userId?: string;
}

function getScopePart(value: string | null | undefined, fallback: string) {
  const trimmed = value?.trim();
  return trimmed && trimmed.length > 0 ? trimmed : fallback;
}

function getScopedSetupSkipKey(scope: SetupSkipScope) {
  const tenantScope = getScopePart(scope.tenantId ?? scope.tenantSlug, "default");
  const userScope = getScopePart(scope.userId, "anonymous");
  return `${SETUP_SKIPPED_PREFIX}${tenantScope}:${userScope}`;
}

export function isSetupSkipped(scope: SetupSkipScope) {
  return localStorage.getItem(getScopedSetupSkipKey(scope)) === "1";
}

export function markSetupSkipped(scope: SetupSkipScope) {
  localStorage.setItem(getScopedSetupSkipKey(scope), "1");
}

export function clearSetupSkippedState() {
  const keysToRemove: string[] = [];
  for (let i = 0; i < localStorage.length; i += 1) {
    const key = localStorage.key(i);
    if (!key) continue;
    if (key.startsWith(SETUP_SKIPPED_PREFIX)) {
      keysToRemove.push(key);
    }
  }

  keysToRemove.forEach((key) => localStorage.removeItem(key));
}
