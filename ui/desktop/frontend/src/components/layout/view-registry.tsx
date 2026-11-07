import { lazy, type ComponentType, type LazyExoticComponent } from 'react'
import type { AppView } from '../../stores/ui-store'
import {
  IconActivity,
  IconBarChart,
  IconBot,
  IconChat,
  IconClock,
  IconGear,
  IconHardDrive,
  IconHome,
  IconPlug,
  IconRadio,
  IconStar,
  IconTrendingUp,
  IconUsers,
  IconWrench,
  type IconProps,
} from '../common/Icons'

export type NavGroupId = 'chat' | 'configure' | 'monitor'

/**
 * How the shell frames a view inside the main panel:
 * - `fill`: the view owns the whole panel (its own header and scrolling).
 * - `page`: legacy list screens; the shell adds page padding and scrolling.
 * - `fill-padded`: full height with page padding (screens that manage inner scroll).
 */
export type ViewFrame = 'fill' | 'page' | 'fill-padded'

export interface ViewDefinition {
  id: AppView
  icon: ComponentType<IconProps>
  component: ComponentType | LazyExoticComponent<ComponentType>
  frame: ViewFrame
  /** Sidebar item highlighted while this view is open (team-board → teams). */
  navParent?: AppView
}

const named = <K extends string>(load: () => Promise<Record<K, ComponentType>>, name: K) =>
  lazy(() => load().then((m) => ({ default: m[name] })))

/** Every top-level view and the component that renders it. */
export const VIEW_REGISTRY: Record<AppView, ViewDefinition> = {
  chat: { id: 'chat', icon: IconHome, frame: 'fill', component: named(() => import('../chat/ChatCanvas'), 'ChatCanvas') },
  sessions: { id: 'sessions', icon: IconChat, frame: 'fill', component: named(() => import('../sessions/SessionsView'), 'SessionsView') },
  teams: { id: 'teams', icon: IconUsers, frame: 'fill', component: named(() => import('../teams/TeamsView'), 'TeamsView') },
  'team-board': { id: 'team-board', icon: IconUsers, frame: 'fill', navParent: 'teams', component: named(() => import('../teams/TeamBoard'), 'TeamBoard') },
  agents: { id: 'agents', icon: IconBot, frame: 'page', component: named(() => import('../agents/AgentList'), 'AgentList') },
  skills: { id: 'skills', icon: IconStar, frame: 'page', component: named(() => import('../skills/SkillList'), 'SkillList') },
  tools: { id: 'tools', icon: IconWrench, frame: 'page', component: named(() => import('../tools/ToolList'), 'ToolList') },
  providers: { id: 'providers', icon: IconActivity, frame: 'page', component: named(() => import('../providers/ProviderList'), 'ProviderList') },
  mcp: { id: 'mcp', icon: IconPlug, frame: 'page', component: named(() => import('../mcp/McpServerList'), 'McpServerList') },
  channels: { id: 'channels', icon: IconRadio, frame: 'page', component: named(() => import('../channels/ChannelList'), 'ChannelList') },
  storage: { id: 'storage', icon: IconHardDrive, frame: 'fill-padded', component: named(() => import('../storage/StorageTab'), 'StorageTab') },
  traces: { id: 'traces', icon: IconTrendingUp, frame: 'page', component: named(() => import('../traces/TraceList'), 'TraceList') },
  usage: { id: 'usage', icon: IconBarChart, frame: 'fill', component: named(() => import('../usage/UsageView'), 'UsageView') },
  cron: { id: 'cron', icon: IconClock, frame: 'page', component: named(() => import('../cron/CronList'), 'CronList') },
  settings: { id: 'settings', icon: IconGear, frame: 'fill', component: named(() => import('../settings/SettingsView'), 'SettingsView') },
}

/** Sidebar groups in display order. Storage has no card and sits last in Configure. */
export const NAV_GROUPS: ReadonlyArray<{ id: NavGroupId; views: readonly AppView[] }> = [
  { id: 'chat', views: ['sessions', 'teams'] },
  { id: 'configure', views: ['agents', 'skills', 'tools', 'providers', 'mcp', 'channels', 'storage'] },
  { id: 'monitor', views: ['traces', 'cron', 'usage', 'settings'] },
]

/** Views reachable from the sidebar, in order. */
export const NAV_VIEWS: readonly AppView[] = NAV_GROUPS.flatMap((g) => g.views)

/** The sidebar item that should look active for a view (null for chat/home). */
export function navItemFor(view: AppView): AppView | null {
  const parent = VIEW_REGISTRY[view].navParent ?? view
  return NAV_VIEWS.includes(parent) ? parent : null
}

/** i18n key (desktop namespace) of a view's label. */
export function viewLabelKey(view: AppView): string {
  return `nav.views.${view}`
}
