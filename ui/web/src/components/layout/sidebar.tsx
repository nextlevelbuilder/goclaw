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
  ClipboardList,
  HardDrive,
  Inbox,
  Brain,
  Network,
  Contact,
  KeyRound,
  FileText,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { SidebarGroup } from "./sidebar-group";
import { SidebarItem } from "./sidebar-item";
import { ConnectionStatus } from "./connection-status";
import { ROUTES } from "@/lib/constants";
import { cn } from "@/lib/utils";
import { useBrandingStore } from "@/stores/use-branding-store";
import { usePendingPairingsCount } from "@/hooks/use-pending-pairings-count";
import { useFeaturesStore } from "@/stores/use-features-store";

interface SidebarProps {
  collapsed: boolean;
  onNavItemClick?: () => void;
}

export function Sidebar({ collapsed, onNavItemClick }: SidebarProps) {
  const { t } = useTranslation("sidebar");
  const appName = useBrandingStore((s) => s.appName);
  const abbrev = appName.slice(0, 2).toUpperCase();
  const { pendingCount } = usePendingPairingsCount();
  const isRouteEnabled = useFeaturesStore((s) => s.isRouteEnabled);

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
            {appName}
          </span>
        )}
        {collapsed && (
          <span className="mx-auto text-lg font-bold">{abbrev}</span>
        )}
      </div>

      {/* Nav items */}
      <nav className="flex-1 space-y-4 overflow-y-auto px-2 py-4">
        <SidebarGroup label={t("groups.core")} collapsed={collapsed}>
          <SidebarItem to={ROUTES.OVERVIEW} icon={LayoutDashboard} label={t("nav.overview")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.CHAT} icon={MessageSquare} label={t("nav.chat")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.AGENTS} icon={Bot} label={t("nav.agents")} collapsed={collapsed} />
          {isRouteEnabled(ROUTES.TEAMS) && (
            <SidebarItem to={ROUTES.TEAMS} icon={Users} label={t("nav.agentTeams")} collapsed={collapsed} />
          )}
        </SidebarGroup>

        {(isRouteEnabled(ROUTES.SESSIONS) || isRouteEnabled(ROUTES.PENDING_MESSAGES) || isRouteEnabled(ROUTES.CONTACTS)) && (
          <SidebarGroup label={t("groups.conversations")} collapsed={collapsed}>
            {isRouteEnabled(ROUTES.SESSIONS) && <SidebarItem to={ROUTES.SESSIONS} icon={History} label={t("nav.sessions")} collapsed={collapsed} />}
            {isRouteEnabled(ROUTES.PENDING_MESSAGES) && <SidebarItem to={ROUTES.PENDING_MESSAGES} icon={Inbox} label={t("nav.pendingMessages")} collapsed={collapsed} />}
            {isRouteEnabled(ROUTES.CONTACTS) && <SidebarItem to={ROUTES.CONTACTS} icon={Contact} label={t("nav.contacts")} collapsed={collapsed} />}
          </SidebarGroup>
        )}

        {(isRouteEnabled(ROUTES.CHANNELS) || isRouteEnabled(ROUTES.NODES)) && (
          <SidebarGroup label={t("groups.connectivity")} collapsed={collapsed}>
            {isRouteEnabled(ROUTES.CHANNELS) && <SidebarItem to={ROUTES.CHANNELS} icon={Radio} label={t("nav.channels")} collapsed={collapsed} />}
            {isRouteEnabled(ROUTES.NODES) && <SidebarItem to={ROUTES.NODES} icon={Link} label={t("nav.nodes")} collapsed={collapsed} badge={pendingCount} />}
          </SidebarGroup>
        )}

        {(isRouteEnabled(ROUTES.SKILLS) || isRouteEnabled(ROUTES.BUILTIN_TOOLS) || isRouteEnabled(ROUTES.MCP) || isRouteEnabled(ROUTES.TTS) || isRouteEnabled(ROUTES.CRON)) && (
          <SidebarGroup label={t("groups.capabilities")} collapsed={collapsed}>
            {isRouteEnabled(ROUTES.SKILLS) && <SidebarItem to={ROUTES.SKILLS} icon={Zap} label={t("nav.skills")} collapsed={collapsed} />}
            {isRouteEnabled(ROUTES.BUILTIN_TOOLS) && <SidebarItem to={ROUTES.BUILTIN_TOOLS} icon={Package} label={t("nav.builtinTools")} collapsed={collapsed} />}
            {isRouteEnabled(ROUTES.MCP) && <SidebarItem to={ROUTES.MCP} icon={Plug} label={t("nav.mcpServers")} collapsed={collapsed} />}
            {isRouteEnabled(ROUTES.TTS) && <SidebarItem to={ROUTES.TTS} icon={Volume2} label={t("nav.tts")} collapsed={collapsed} />}
            {isRouteEnabled(ROUTES.CRON) && <SidebarItem to={ROUTES.CRON} icon={Clock} label={t("nav.cron")} collapsed={collapsed} />}
          </SidebarGroup>
        )}

        {(isRouteEnabled(ROUTES.MEMORY) || isRouteEnabled(ROUTES.KNOWLEDGE_GRAPH) || isRouteEnabled(ROUTES.STORAGE)) && (
          <SidebarGroup label={t("groups.data")} collapsed={collapsed}>
            {isRouteEnabled(ROUTES.MEMORY) && <SidebarItem to={ROUTES.MEMORY} icon={Brain} label={t("nav.memory")} collapsed={collapsed} />}
            {isRouteEnabled(ROUTES.KNOWLEDGE_GRAPH) && <SidebarItem to={ROUTES.KNOWLEDGE_GRAPH} icon={Network} label={t("nav.knowledgeGraph")} collapsed={collapsed} />}
            {isRouteEnabled(ROUTES.STORAGE) && <SidebarItem to={ROUTES.STORAGE} icon={HardDrive} label={t("nav.storage")} collapsed={collapsed} />}
          </SidebarGroup>
        )}

        {(isRouteEnabled(ROUTES.TRACES) || isRouteEnabled(ROUTES.EVENTS) || isRouteEnabled(ROUTES.ACTIVITY) || isRouteEnabled(ROUTES.LOGS)) && (
          <SidebarGroup label={t("groups.monitoring")} collapsed={collapsed}>
            {isRouteEnabled(ROUTES.TRACES) && <SidebarItem to={ROUTES.TRACES} icon={Activity} label={t("nav.traces")} collapsed={collapsed} />}
            {isRouteEnabled(ROUTES.EVENTS) && <SidebarItem to={ROUTES.EVENTS} icon={Radar} label={t("nav.realtimeEvents")} collapsed={collapsed} />}
            {isRouteEnabled(ROUTES.ACTIVITY) && <SidebarItem to={ROUTES.ACTIVITY} icon={ClipboardList} label={t("nav.activity")} collapsed={collapsed} />}
            {isRouteEnabled(ROUTES.LOGS) && <SidebarItem to={ROUTES.LOGS} icon={Terminal} label={t("nav.logs")} collapsed={collapsed} />}
          </SidebarGroup>
        )}

        <SidebarGroup label={t("groups.system")} collapsed={collapsed}>
          <SidebarItem to={ROUTES.PROVIDERS} icon={Cpu} label={t("nav.providers")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.CLI_CREDENTIALS} icon={KeyRound} label={t("nav.cliCredentials")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.API_KEYS} icon={KeyRound} label={t("nav.apiKeys")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.GATEWAY_USERS} icon={Users} label={t("nav.gatewayUsers")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.CONFIG} icon={Settings} label={t("nav.config")} collapsed={collapsed} />
          {isRouteEnabled(ROUTES.APPROVALS) && <SidebarItem to={ROUTES.APPROVALS} icon={ShieldCheck} label={t("nav.approvals")} collapsed={collapsed} />}
          <SidebarItem to="/docs" icon={FileText} label={t("nav.apiDocs")} collapsed={collapsed} external />
        </SidebarGroup>
      </nav>

      {/* Footer: connection status */}
      <div className={cn("border-t py-3", collapsed ? "px-2 flex justify-center" : "px-4")}>
        <ConnectionStatus collapsed={collapsed} />
      </div>
    </aside>
  );
}
