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
  const tenantScope = getScopePart(
    scope.tenantId ?? scope.tenantSlug ?? localStorage.getItem(LOCAL_STORAGE_KEYS.TENANT_ID),
    "default",
  );
  const userScope = getScopePart(
    scope.userId ?? localStorage.getItem(LOCAL_STORAGE_KEYS.USER_ID),
    "anonymous",
  );

  return `${SETUP_SKIPPED_PREFIX}${encodeURIComponent(tenantScope)}:${encodeURIComponent(userScope)}`;
}

export function isSetupSkipped(scope: SetupSkipScope) {
  if (localStorage.getItem(LOCAL_STORAGE_KEYS.SETUP_SKIPPED) !== null) {
    localStorage.removeItem(LOCAL_STORAGE_KEYS.SETUP_SKIPPED);
  }
  return localStorage.getItem(getScopedSetupSkipKey(scope)) === "1";
}

export function markSetupSkipped(scope: SetupSkipScope) {
  localStorage.removeItem(LOCAL_STORAGE_KEYS.SETUP_SKIPPED);
  localStorage.setItem(getScopedSetupSkipKey(scope), "1");
}

export function clearSetupSkippedState() {
  const keysToRemove: string[] = [];
  for (let i = 0; i < localStorage.length; i += 1) {
    const key = localStorage.key(i);
    if (!key) continue;
    if (key === LOCAL_STORAGE_KEYS.SETUP_SKIPPED || key.startsWith(SETUP_SKIPPED_PREFIX)) {
      keysToRemove.push(key);
    }
  }

  keysToRemove.forEach((key) => localStorage.removeItem(key));
}
