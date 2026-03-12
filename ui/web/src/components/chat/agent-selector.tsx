import { useState, useEffect, useRef, useLayoutEffect } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { Bot, ChevronDown } from "lucide-react";
import { useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import type { AgentData } from "@/types/agent";

interface AgentSelectorProps {
  value: string;
  onChange: (agentId: string) => void;
}

export function AgentSelector({ value, onChange }: AgentSelectorProps) {
  const { t } = useTranslation("common");
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);
  const [agents, setAgents] = useState<AgentData[]>([]);
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const [dropdownStyle, setDropdownStyle] = useState<React.CSSProperties>({});

  useEffect(() => {
    if (!connected) return;
    http
      .get<{ agents: AgentData[] }>("/v1/agents")
      .then((res) => {
        const active = (res.agents ?? []).filter((a) => a.status === "active");
        setAgents(active);
        // Auto-select first agent if current value doesn't match any active agent
        if (active.length > 0 && !active.some((a) => a.agent_key === value)) {
          onChange(active[0]!.agent_key);
        }
      })
      .catch(() => {});
  }, [http, connected]);

  // Compute dropdown position for portal rendering
  useLayoutEffect(() => {
    if (!open || !containerRef.current) return;
    const rect = containerRef.current.getBoundingClientRect();
    setDropdownStyle({
      position: "fixed",
      top: rect.bottom + 4,
      left: rect.left,
      width: rect.width,
      zIndex: 9999,
    });
  }, [open]);

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      const target = e.target as Node;
      if (
        containerRef.current && !containerRef.current.contains(target) &&
        (!dropdownRef.current || !dropdownRef.current.contains(target))
      ) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  const selected = agents.find((a) => a.agent_key === value);

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-2 rounded-lg border bg-background px-3 py-2 text-sm hover:bg-accent"
      >
        <Bot className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span className="flex-1 truncate text-left">
          {selected?.display_name ?? selected?.agent_key ?? (value || t("selectAgent"))}
        </span>
        <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
      </button>

      {open && createPortal(
        <div
          ref={dropdownRef}
          style={dropdownStyle}
          className="max-h-60 overflow-y-auto rounded-lg border bg-popover p-1 shadow-md"
        >
          {agents.length === 0 && (
            <div className="px-3 py-2 text-sm text-muted-foreground">
              {t("noAgentsAvailable")}
            </div>
          )}
          {agents.map((agent) => (
            <button
              key={agent.agent_key}
              type="button"
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => {
                onChange(agent.agent_key);
                setOpen(false);
              }}
              className={`flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm hover:bg-accent ${
                agent.agent_key === value ? "bg-accent" : ""
              }`}
            >
              <Bot className="h-4 w-4 shrink-0 text-muted-foreground" />
              <span className="flex-1 truncate text-left">
                {agent.display_name || agent.agent_key}
              </span>
              {agent.is_default && (
                <span className="text-xs text-muted-foreground">{t("default")}</span>
              )}
            </button>
          ))}
        </div>,
        document.body,
      )}
    </div>
  );
}
