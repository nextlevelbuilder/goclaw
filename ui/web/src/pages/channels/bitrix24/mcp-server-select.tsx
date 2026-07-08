import { useMemo } from "react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useMCP } from "@/pages/mcp/hooks/use-mcp";
import type { MCPServerData } from "@/types/mcp";

/**
 * Dropdown picker over MCP servers whose `require_user_credentials` flag is
 * true — the population channels can meaningfully auto-onboard against.
 *
 * The list comes from the same `useMCP()` query the MCP admin page uses so
 * cache is shared and the filter is a cheap client-side pass (~10 servers
 * per portal on realistic setups). Legacy `mcp_server_name` fallback is
 * handled at the Bitrix24 factory layer; this component only writes UUIDs.
 */
interface MCPServerSelectProps {
  value: string;
  onChange: (value: string | undefined) => void;
  placeholder?: string;
  disabled?: boolean;
}

const CLEAR_VALUE = "__none__";

export function MCPServerSelect({ value, onChange, placeholder, disabled }: MCPServerSelectProps) {
  const { servers, loading } = useMCP();

  const perUserServers = useMemo<MCPServerData[]>(
    () =>
      (servers ?? []).filter(
        (s) => s.require_user_credentials || s.settings?.require_user_credentials,
      ),
    [servers],
  );

  const selectValue = value && value !== "" ? value : CLEAR_VALUE;

  return (
    <div className="grid gap-1.5">
      <Select
        value={selectValue}
        onValueChange={(next) => onChange(next === CLEAR_VALUE ? undefined : next)}
        disabled={disabled || loading}
      >
        <SelectTrigger>
          <SelectValue placeholder={placeholder ?? (loading ? "Loading MCP servers…" : "Select an MCP server (optional)")} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={CLEAR_VALUE}>— None (disable MCP provisioning) —</SelectItem>
          {perUserServers.length === 0 && !loading && (
            <SelectItem value="__empty__" disabled>
              No per-user MCP servers registered
            </SelectItem>
          )}
          {perUserServers.map((srv) => (
            <SelectItem key={srv.id} value={srv.id}>
              {srv.display_name?.trim() || srv.name}
              {srv.url ? <span className="ml-2 text-xs text-muted-foreground">({srv.url})</span> : null}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
