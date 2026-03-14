import {
  LayoutDashboard,
  MessageSquare,
  Bot,
  History,
  Zap,
  Clock,
  Activity,
  Radio,
  Radar,
  Terminal,
  Settings,
  ShieldCheck,
  Users,
  Link,
  Package,
  Plug,
  Volume2,
  Cpu,
  ArrowRightLeft,
  ClipboardList,
  HardDrive,
  Inbox,
  Brain,
  Network,
  Contact,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { SidebarGroup } from "./sidebar-group";
import { SidebarItem } from "./sidebar-item";
import { ConnectionStatus } from "./connection-status";
import { ROUTES } from "@/lib/constants";
import { brand } from "@/lib/brand";
import { cn } from "@/lib/utils";
import { usePendingPairingsCount } from "@/hooks/use-pending-pairings-count";
import { useFeaturesStore } from "@/stores/use-features-store";

interface SidebarProps {
  collapsed: boolean;
  onNavItemClick?: () => void;
}

export function Sidebar({ collapsed, onNavItemClick }: SidebarProps) {
  const { t } = useTranslation("sidebar");
  const { pendingCount } = usePendingPairingsCount();
  const fe = useFeaturesStore((s) => s.isFeatureEnabled);

  return (
    <aside
      className={cn(
        "flex h-full flex-col border-r bg-sidebar text-sidebar-foreground transition-all duration-200",
        collapsed ? "w-16" : "w-64",
      )}
      onClick={(e) => {
        // Close mobile drawer when clicking a nav link
        if (onNavItemClick && (e.target as HTMLElement).closest("a")) {
          onNavItemClick();
        }
      }}
    >
      {/* Logo / title */}
      <div className="flex h-14 items-center border-b px-4">
        {!collapsed && (
          <span className="text-base font-semibold tracking-tight">
            {brand.appName}
          </span>
        )}
        {collapsed && (
          <span className="mx-auto text-lg font-bold">{brand.appKey.slice(0, 2).toUpperCase()}</span>
        )}
      </div>

      {/* Nav items */}
      <nav className="flex-1 space-y-4 overflow-y-auto px-2 py-4">
        <SidebarGroup label={t("groups.core")} collapsed={collapsed} features={["overview", "chat", "agents", "agent_teams"]}>
          {fe("overview") && <SidebarItem to={ROUTES.OVERVIEW} icon={LayoutDashboard} label={t("nav.overview")} collapsed={collapsed} />}
          {fe("chat") && <SidebarItem to={ROUTES.CHAT} icon={MessageSquare} label={t("nav.chat")} collapsed={collapsed} />}
          {fe("agents") && <SidebarItem to={ROUTES.AGENTS} icon={Bot} label={t("nav.agents")} collapsed={collapsed} />}
          {fe("agent_teams") && <SidebarItem to={ROUTES.TEAMS} icon={Users} label={t("nav.agentTeams")} collapsed={collapsed} />}
        </SidebarGroup>

        <SidebarGroup label={t("groups.conversations")} collapsed={collapsed} features={["sessions", "pending_messages", "contacts"]}>
          {fe("sessions") && <SidebarItem to={ROUTES.SESSIONS} icon={History} label={t("nav.sessions")} collapsed={collapsed} />}
          {fe("pending_messages") && <SidebarItem to={ROUTES.PENDING_MESSAGES} icon={Inbox} label={t("nav.pendingMessages")} collapsed={collapsed} />}
          {fe("contacts") && <SidebarItem to={ROUTES.CONTACTS} icon={Contact} label={t("nav.contacts")} collapsed={collapsed} />}
        </SidebarGroup>

        <SidebarGroup label={t("groups.connectivity")} collapsed={collapsed} features={["channels", "nodes"]}>
          {fe("channels") && <SidebarItem to={ROUTES.CHANNELS} icon={Radio} label={t("nav.channels")} collapsed={collapsed} />}
          {fe("nodes") && <SidebarItem to={ROUTES.NODES} icon={Link} label={t("nav.nodes")} collapsed={collapsed} badge={pendingCount} />}
        </SidebarGroup>

        <SidebarGroup label={t("groups.capabilities")} collapsed={collapsed} features={["skills", "builtin_tools", "mcp_servers", "tts", "cron"]}>
          {fe("skills") && <SidebarItem to={ROUTES.SKILLS} icon={Zap} label={t("nav.skills")} collapsed={collapsed} />}
          {fe("builtin_tools") && <SidebarItem to={ROUTES.BUILTIN_TOOLS} icon={Package} label={t("nav.builtinTools")} collapsed={collapsed} />}
          {fe("mcp_servers") && <SidebarItem to={ROUTES.MCP} icon={Plug} label={t("nav.mcpServers")} collapsed={collapsed} />}
          {fe("tts") && <SidebarItem to={ROUTES.TTS} icon={Volume2} label={t("nav.tts")} collapsed={collapsed} />}
          {fe("cron") && <SidebarItem to={ROUTES.CRON} icon={Clock} label={t("nav.cron")} collapsed={collapsed} />}
        </SidebarGroup>

        <SidebarGroup label={t("groups.data")} collapsed={collapsed} features={["memory", "knowledge_graph", "storage"]}>
          {fe("memory") && <SidebarItem to={ROUTES.MEMORY} icon={Brain} label={t("nav.memory")} collapsed={collapsed} />}
          {fe("knowledge_graph") && <SidebarItem to={ROUTES.KNOWLEDGE_GRAPH} icon={Network} label={t("nav.knowledgeGraph")} collapsed={collapsed} />}
          {fe("storage") && <SidebarItem to={ROUTES.STORAGE} icon={HardDrive} label={t("nav.storage")} collapsed={collapsed} />}
        </SidebarGroup>

        <SidebarGroup label={t("groups.monitoring")} collapsed={collapsed} features={["traces", "events", "delegations", "activity", "logs"]}>
          {fe("traces") && <SidebarItem to={ROUTES.TRACES} icon={Activity} label={t("nav.traces")} collapsed={collapsed} />}
          {fe("events") && <SidebarItem to={ROUTES.EVENTS} icon={Radar} label={t("nav.realtimeEvents")} collapsed={collapsed} />}
          {fe("delegations") && <SidebarItem to={ROUTES.DELEGATIONS} icon={ArrowRightLeft} label={t("nav.delegations")} collapsed={collapsed} />}
          {fe("activity") && <SidebarItem to={ROUTES.ACTIVITY} icon={ClipboardList} label={t("nav.activity")} collapsed={collapsed} />}
          {fe("logs") && <SidebarItem to={ROUTES.LOGS} icon={Terminal} label={t("nav.logs")} collapsed={collapsed} />}
        </SidebarGroup>

        <SidebarGroup label={t("groups.system")} collapsed={collapsed} features={["providers", "config", "approvals"]}>
          {fe("providers") && <SidebarItem to={ROUTES.PROVIDERS} icon={Cpu} label={t("nav.providers")} collapsed={collapsed} />}
          {fe("config") && <SidebarItem to={ROUTES.CONFIG} icon={Settings} label={t("nav.config")} collapsed={collapsed} />}
          {fe("approvals") && <SidebarItem to={ROUTES.APPROVALS} icon={ShieldCheck} label={t("nav.approvals")} collapsed={collapsed} />}
        </SidebarGroup>
      </nav>

      {/* Footer: connection status */}
      <div className="border-t px-4 py-3">
        <ConnectionStatus />
      </div>
    </aside>
  );
}
