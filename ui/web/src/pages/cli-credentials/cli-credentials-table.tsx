/**
 * CliCredentialsTable — table + row actions for CLI credential entries.
 * Extracted from cli-credentials-panel.tsx to stay under 200-line limit.
 * Phase 8: each row has a chip sub-row from agent_grants_summary.
 */
import { useTranslation } from "react-i18next";
import { KeyRound, Pencil, Trash2, Users, Shield } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { CliCredentialAgentChips } from "./cli-credential-agent-chips";
import type { SecureCLIBinary } from "./hooks/use-cli-credentials";

interface Props {
  items: SecureCLIBinary[];
  onEdit: (item: SecureCLIBinary) => void;
  onDelete: (item: SecureCLIBinary) => void;
  onUserCreds: (item: SecureCLIBinary) => void;
  onGrants: (item: SecureCLIBinary) => void;
}

export function CliCredentialsTable({ items, onEdit, onDelete, onUserCreds, onGrants }: Props) {
  const { t } = useTranslation("cli-credentials");
  const { t: tc } = useTranslation("common");

  return (
    <div className="overflow-x-auto rounded-md border">
      <table className="w-full min-w-[600px] text-sm">
        <thead>
          <tr className="border-b bg-muted/50">
            <th className="px-4 py-3 text-left font-medium">{t("columns.binary")}</th>
            <th className="px-4 py-3 text-left font-medium">{tc("description")}</th>
            <th className="px-4 py-3 text-left font-medium">{t("columns.scope")}</th>
            <th className="px-4 py-3 text-left font-medium">{tc("enabled")}</th>
            <th className="px-4 py-3 text-left font-medium">{t("columns.timeout")}</th>
            <th className="px-4 py-3 text-right font-medium">{tc("actions")}</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <>
              {/* Main data row */}
              <tr key={item.id} className="border-b hover:bg-muted/30">
                <td className="px-4 py-3">
                  <div className="flex items-center gap-2">
                    <KeyRound className="h-4 w-4 shrink-0 text-muted-foreground" />
                    <div>
                      <div className="font-medium">{item.binary_name}</div>
                      {item.binary_path && (
                        <div className="text-xs text-muted-foreground font-mono">{item.binary_path}</div>
                      )}
                    </div>
                  </div>
                </td>
                <td className="px-4 py-3 text-muted-foreground max-w-[220px] truncate">
                  {item.description || "—"}
                </td>
                <td className="px-4 py-3">
                  <Badge variant={item.is_global ? "outline" : "secondary"}>
                    {item.is_global ? tc("global") : t("columns.restricted")}
                  </Badge>
                </td>
                <td className="px-4 py-3">
                  <Badge variant={item.enabled ? "default" : "secondary"}>
                    {item.enabled ? tc("enabled") : tc("disabled")}
                  </Badge>
                </td>
                <td className="px-4 py-3 text-muted-foreground">{item.timeout_seconds}s</td>
                <td className="px-4 py-3 text-right">
                  <div className="flex items-center justify-end gap-1">
                    <Button variant="ghost" size="sm" onClick={() => onGrants(item)} title={t("grants.title", { name: item.binary_name })}>
                      <Shield className="h-3.5 w-3.5" />
                    </Button>
                    <Button variant="ghost" size="sm" onClick={() => onUserCreds(item)} title={t("userCredentials.title")}>
                      <Users className="h-3.5 w-3.5" />
                    </Button>
                    <Button variant="ghost" size="sm" onClick={() => onEdit(item)} className="gap-1">
                      <Pencil className="h-3.5 w-3.5" /> {tc("edit")}
                    </Button>
                    <Button
                      variant="ghost" size="sm"
                      onClick={() => onDelete(item)}
                      className="gap-1 text-destructive hover:text-destructive"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </td>
              </tr>
              {/* Agent chips sub-row — Phase 8 */}
              <tr key={`${item.id}-chips`} className="border-b last:border-0">
                <td colSpan={6} className="p-0">
                  <CliCredentialAgentChips
                    agentGrantsSummary={item.agent_grants_summary}
                    onOpenGrants={() => onGrants(item)}
                  />
                </td>
              </tr>
            </>
          ))}
        </tbody>
      </table>
    </div>
  );
}
