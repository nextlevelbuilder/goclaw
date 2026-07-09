import { getVaultNodeColor } from "@/adapters/vault-graph-adapter";

export function resolveGraphNodeDisplayColor(attrs: Record<string, unknown>, isDark: boolean): string {
  const docType = typeof attrs.docType === "string" ? attrs.docType : "";
  if (docType) {
    return getVaultNodeColor(docType, isDark);
  }
  return typeof attrs.color === "string" && attrs.color
    ? attrs.color
    : getVaultNodeColor("other", isDark);
}
