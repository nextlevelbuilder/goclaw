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
  Blocks,
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
  Building2,
  ArrowLeftRight,
  FileArchive,
  DatabaseBackup,
  Webhook,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { SidebarGroup } from "./sidebar-group";
import { SidebarItem } from "./sidebar-item";
import { ConnectionStatus } from "./connection-status";
import { ROUTES } from "@/lib/constants";
import { cn } from "@/lib/utils";
import { usePendingPairingsCount } from "@/hooks/use-pending-pairings-count";
import { useAuthStore } from "@/stores/use-auth-store";
import { useTenants } from "@/hooks/use-tenants";

interface SidebarProps {
  collapsed: boolean;
  onNavItemClick?: () => void;
}

export function Sidebar({ collapsed, onNavItemClick }: SidebarProps) {
  const { t } = useTranslation("sidebar");
  const { pendingCount } = usePendingPairingsCount();
  const role = useAuthStore((s) => s.role);
  const { isOwner } = useTenants();
  const isAdmin = role === "admin" || role === "owner";

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
          <div className="flex items-center gap-2.5">
            <img src="/goclaw-icon.svg" alt="GoClaw" className="h-8 w-8" />
            <span className="text-lg font-bold tracking-tight text-sidebar-primary">
              GoClaw
            </span>
          </div>
        )}
        {collapsed && (
          <img src="/goclaw-icon.svg" alt="GoClaw" className="mx-auto h-7 w-7" />
        )}
      </div>

      {/* Nav items */}
      <nav className="flex-1 space-y-4 overflow-y-auto px-2 py-4">
        <SidebarGroup label={t("groups.core")} collapsed={collapsed}>
          <SidebarItem to={ROUTES.OVERVIEW} icon={LayoutDashboard} label={t("nav.overview")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.CHAT} icon={MessageSquare} label={t("nav.chat")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.AGENTS} icon={Bot} label={t("nav.agents")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.TEAMS} icon={Users} label={t("nav.agentTeams")} collapsed={collapsed} />
        </SidebarGroup>

        <SidebarGroup label={t("groups.conversations")} collapsed={collapsed}>
          <SidebarItem to={ROUTES.SESSIONS} icon={History} label={t("nav.sessions")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.PENDING_MESSAGES} icon={Inbox} label={t("nav.pendingMessages")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.CONTACTS} icon={Contact} label={t("nav.contacts")} collapsed={collapsed} />
        </SidebarGroup>

        <SidebarGroup label={t("groups.connectivity")} collapsed={collapsed}>
          <SidebarItem to={ROUTES.CHANNELS} icon={Radio} label={t("nav.channels")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.NODES} icon={Link} label={t("nav.nodes")} collapsed={collapsed} badge={pendingCount} />
        </SidebarGroup>

        <SidebarGroup label={t("groups.capabilities")} collapsed={collapsed}>
          <SidebarItem to={ROUTES.SKILLS} icon={Zap} label={t("nav.skills")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.BUILTIN_TOOLS} icon={Package} label={t("nav.builtinTools")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.MCP} icon={Plug} label={t("nav.mcpServers")} collapsed={collapsed} />
          {isOwner && (
            <SidebarItem to={ROUTES.TTS} icon={Volume2} label={t("nav.tts")} collapsed={collapsed} />
          )}
          <SidebarItem to={ROUTES.CRON} icon={Clock} label={t("nav.cron")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.HOOKS} icon={Webhook} label={t("nav.hooks")} collapsed={collapsed} />
        </SidebarGroup>

        <SidebarGroup label={t("groups.data")} collapsed={collapsed}>
          <SidebarItem to={ROUTES.MEMORY} icon={Brain} label={t("nav.memory")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.VAULT} icon={FileArchive} label={t("nav.vault")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.KNOWLEDGE_GRAPH} icon={Network} label={t("nav.knowledgeGraph")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.STORAGE} icon={HardDrive} label={t("nav.storage")} collapsed={collapsed} />
        </SidebarGroup>

        <SidebarGroup label={t("groups.monitoring")} collapsed={collapsed}>
          <SidebarItem to={ROUTES.TRACES} icon={Activity} label={t("nav.traces")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.EVENTS} icon={Radar} label={t("nav.realtimeEvents")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.ACTIVITY} icon={ClipboardList} label={t("nav.activity")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.LOGS} icon={Terminal} label={t("nav.logs")} collapsed={collapsed} />
        </SidebarGroup>

        {isAdmin && (
        <SidebarGroup label={t("groups.system")} collapsed={collapsed}>
          {isOwner && (
            <SidebarItem to={ROUTES.TENANTS} icon={Building2} label={t("nav.tenants")} collapsed={collapsed} />
          )}
          <SidebarItem to={ROUTES.PROVIDERS} icon={Cpu} label={t("nav.providers")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.CLI_CREDENTIALS} icon={KeyRound} label={t("nav.cliCredentials")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.API_KEYS} icon={KeyRound} label={t("nav.apiKeys")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.PACKAGES} icon={Blocks} label={t("nav.packages")} collapsed={collapsed} />
          {isOwner && (
            <SidebarItem to={ROUTES.CONFIG} icon={Settings} label={t("nav.config")} collapsed={collapsed} />
          )}
          <SidebarItem to={ROUTES.APPROVALS} icon={ShieldCheck} label={t("nav.approvals")} collapsed={collapsed} />
          <SidebarItem to={ROUTES.IMPORT_EXPORT} icon={ArrowLeftRight} label={t("nav.importExport")} collapsed={collapsed} />
          {isOwner && (
            <SidebarItem to={ROUTES.BACKUP_RESTORE} icon={DatabaseBackup} label={t("nav.backupRestore")} collapsed={collapsed} />
          )}
        </SidebarGroup>
        )}
      </nav>

      {/* Footer: connection status */}
      <div className={cn("border-t py-3", collapsed ? "px-2 flex justify-center" : "px-4")}>
        <ConnectionStatus collapsed={collapsed} />
      </div>
    </aside>
  );
}
