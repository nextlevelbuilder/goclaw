import type { AppView } from '../../stores/ui-store'
import { NAV_VIEWS, viewLabelKey } from './view-registry'

export type PaletteGroup = 'actions' | 'views' | 'agents' | 'sessions'

export type PaletteTarget =
  | { kind: 'action'; action: 'new-chat' | 'toggle-theme' | 'open-settings' }
  | { kind: 'view'; view: AppView }
  | { kind: 'agent'; agentId: string }
  | { kind: 'session'; sessionKey: string }

export interface PaletteItem {
  id: string
  group: PaletteGroup
  label: string
  /** Right-aligned hint (shortcut, "Open", relative date...). */
  hint?: string
  target: PaletteTarget
}

export interface PaletteSource {
  query: string
  agents: ReadonlyArray<{ id: string; name: string; key?: string }>
  sessions: ReadonlyArray<{ key: string; title: string; lastMessageAt: number }>
  /** desktop-namespace translator. */
  t: (key: string) => string
}

/** Max agents / sessions listed per group. */
export const PALETTE_GROUP_LIMIT = 5

/** Case- and accent-insensitive form used for matching ("Tiếng" → "tieng"). */
export function normalizeSearch(text: string): string {
  return text.normalize('NFD').replace(/\p{Diacritic}/gu, '').replace(/đ/g, 'd').replace(/Đ/g, 'd').toLowerCase().trim()
}

function matches(query: string, ...fields: Array<string | undefined>): boolean {
  if (!query) return true
  return fields.some((f) => f !== undefined && normalizeSearch(f).includes(query))
}

/** Build the palette results for a query, grouped in display order. */
export function buildPaletteItems({ query, agents, sessions, t }: PaletteSource): PaletteItem[] {
  const q = normalizeSearch(query)

  const actions: PaletteItem[] = [
    { id: 'action:new-chat', group: 'actions', label: t('palette.actions.newChat'), hint: 'Ctrl N', target: { kind: 'action', action: 'new-chat' } },
    { id: 'action:toggle-theme', group: 'actions', label: t('palette.actions.toggleTheme'), target: { kind: 'action', action: 'toggle-theme' } },
    { id: 'action:open-settings', group: 'actions', label: t('palette.actions.openSettings'), hint: 'Ctrl ,', target: { kind: 'action', action: 'open-settings' } },
  ]

  const views: PaletteItem[] = NAV_VIEWS.map((view) => ({
    id: `view:${view}`,
    group: 'views' as const,
    label: t(viewLabelKey(view)),
    target: { kind: 'view' as const, view },
  }))

  const agentItems: PaletteItem[] = agents
    .filter((a) => matches(q, a.name, a.key))
    .slice(0, PALETTE_GROUP_LIMIT)
    .map((a) => ({ id: `agent:${a.id}`, group: 'agents' as const, label: a.name, hint: t('palette.open'), target: { kind: 'agent' as const, agentId: a.id } }))

  const sessionItems: PaletteItem[] = [...sessions]
    .sort((a, b) => b.lastMessageAt - a.lastMessageAt)
    .filter((s) => matches(q, s.title))
    .slice(0, PALETTE_GROUP_LIMIT)
    .map((s) => ({ id: `session:${s.key}`, group: 'sessions' as const, label: s.title, target: { kind: 'session' as const, sessionKey: s.key } }))

  return [
    ...actions.filter((i) => matches(q, i.label)),
    ...views.filter((i) => matches(q, i.label)),
    ...agentItems,
    ...sessionItems,
  ]
}
