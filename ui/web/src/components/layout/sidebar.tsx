import {
  LayoutDashboard,
  MessageSquare,
  Bot,
  History,
  Zap,
  Clock,
  Activity,
  BarChart3,
  Radio,
  Terminal,
  Settings,
  ShieldCheck,
  Users,
  Link,
  Wrench,
  Package,
  Plug,
  Volume2,
  Cpu,
  ArrowRightLeft,
} from "lucide-react";
import { SidebarGroup } from "./sidebar-group";
import { SidebarItem } from "./sidebar-item";
import { ConnectionStatus } from "./connection-status";
import { ROUTES } from "@/lib/constants";
import { cn } from "@/lib/utils";

interface SidebarProps {
  collapsed: boolean;
  onNavItemClick?: () => void;
}

export function Sidebar({ collapsed, onNavItemClick }: SidebarProps) {
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
            GoClaw
          </span>
        )}
        {collapsed && (
          <span className="mx-auto text-lg font-bold">OC</span>
        )}
      </div>

      {/* Nav items */}
      <nav className="flex-1 space-y-4 overflow-y-auto px-2 py-4">
        <SidebarGroup label="Core" collapsed={collapsed}>
          <SidebarItem to={ROUTES.OVERVIEW} icon={LayoutDashboard} label="Overview" collapsed={collapsed} />
          <SidebarItem to={ROUTES.CHAT} icon={MessageSquare} label="Chat" collapsed={collapsed} />
        </SidebarGroup>

        <SidebarGroup label="Management" collapsed={collapsed}>
          <SidebarItem to={ROUTES.AGENTS} icon={Bot} label="Agents" collapsed={collapsed} />
          <SidebarItem to={ROUTES.TEAMS} icon={Users} label="Agent Teams" collapsed={collapsed} />
          <SidebarItem to={ROUTES.SESSIONS} icon={History} label="Sessions" collapsed={collapsed} />
          <SidebarItem to={ROUTES.CHANNELS} icon={Radio} label="Channels" collapsed={collapsed} />
          <SidebarItem to={ROUTES.SKILLS} icon={Zap} label="Skills" collapsed={collapsed} />
          <SidebarItem to={ROUTES.CRON} icon={Clock} label="Cron" collapsed={collapsed} />
          <SidebarItem to={ROUTES.CUSTOM_TOOLS} icon={Wrench} label="Custom Tools" collapsed={collapsed} />
          <SidebarItem to={ROUTES.BUILTIN_TOOLS} icon={Package} label="Built-in Tools" collapsed={collapsed} />
          <SidebarItem to={ROUTES.MCP} icon={Plug} label="MCP Servers" collapsed={collapsed} />
        </SidebarGroup>

        <SidebarGroup label="Monitoring" collapsed={collapsed}>
          <SidebarItem to={ROUTES.TRACES} icon={Activity} label="Traces" collapsed={collapsed} />
          <SidebarItem to={ROUTES.DELEGATIONS} icon={ArrowRightLeft} label="Delegations" collapsed={collapsed} />
          <SidebarItem to={ROUTES.USAGE} icon={BarChart3} label="Usage" collapsed={collapsed} />
          <SidebarItem to={ROUTES.LOGS} icon={Terminal} label="Logs" collapsed={collapsed} />
        </SidebarGroup>

        <SidebarGroup label="System" collapsed={collapsed}>
          <SidebarItem to={ROUTES.PROVIDERS} icon={Cpu} label="Providers" collapsed={collapsed} />
          <SidebarItem to={ROUTES.CONFIG} icon={Settings} label="Config" collapsed={collapsed} />
          <SidebarItem to={ROUTES.APPROVALS} icon={ShieldCheck} label="Approvals" collapsed={collapsed} />
          <SidebarItem to={ROUTES.NODES} icon={Link} label="Nodes" collapsed={collapsed} />
          <SidebarItem to={ROUTES.TTS} icon={Volume2} label="TTS" collapsed={collapsed} />
        </SidebarGroup>
      </nav>

      {/* Footer: connection status */}
      <div className="border-t px-4 py-3">
        <ConnectionStatus />
      </div>
    </aside>
  );
}
