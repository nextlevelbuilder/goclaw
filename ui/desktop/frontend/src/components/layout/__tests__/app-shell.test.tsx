import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, within } from '@testing-library/react'

// Each registered view renders a marker so navigation can be asserted without
// mounting the real screens (they talk to the gateway).
vi.mock('../../chat/ChatCanvas', () => ({ ChatCanvas: () => <p>view:chat</p> }))
vi.mock('../../sessions/SessionsView', () => ({ SessionsView: () => <p>view:sessions</p> }))
vi.mock('../../teams/TeamsView', () => ({ TeamsView: () => <p>view:teams</p> }))
vi.mock('../../teams/TeamBoard', () => ({ TeamBoard: () => <p>view:team-board</p> }))
vi.mock('../../agents/AgentList', () => ({ AgentList: () => <p>view:agents</p> }))
vi.mock('../../skills/SkillList', () => ({ SkillList: () => <p>view:skills</p> }))
vi.mock('../../tools/ToolList', () => ({ ToolList: () => <p>view:tools</p> }))
vi.mock('../../providers/ProviderList', () => ({ ProviderList: () => <p>view:providers</p> }))
vi.mock('../../mcp/McpServerList', () => ({ McpServerList: () => <p>view:mcp</p> }))
vi.mock('../../channels/ChannelList', () => ({ ChannelList: () => <p>view:channels</p> }))
vi.mock('../../storage/StorageTab', () => ({ StorageTab: () => <p>view:storage</p> }))
vi.mock('../../traces/TraceList', () => ({ TraceList: () => <p>view:traces</p> }))
vi.mock('../../usage/UsageView', () => ({ UsageView: () => <p>view:usage</p> }))
vi.mock('../../cron/CronList', () => ({ CronList: () => <p>view:cron</p> }))
vi.mock('../../settings/SettingsView', () => ({ SettingsView: () => <p>view:settings</p> }))
vi.mock('../../../hooks/use-agents', () => ({
  useAgents: () => ({ agents: [], selectedAgent: null, selectedAgentId: null, selectAgent: () => {}, refreshAgents: async () => [] }),
}))
vi.mock('../../../hooks/use-sessions', () => ({
  useSessions: () => ({ sessions: [], activeSessionKey: null, setActiveSession: () => {}, createSession: () => {}, deleteSession: async () => {} }),
}))
vi.mock('../../../hooks/use-pending-pairings-count', () => ({ usePendingPairingsCount: () => ({ pendingCount: 2 }) }))

import { useUiStore, type AppView } from '../../../stores/ui-store'
import { installDomPolyfills, switchToEnglish } from '../../ui/__tests__/test-helpers'
import { AppShell } from '../AppShell'
import { NAV_GROUPS, NAV_VIEWS, VIEW_REGISTRY, navItemFor } from '../view-registry'
import { APP_VIEWS } from '../../../stores/ui-store'
import en from '../../../i18n/locales/en/desktop.json'

function setWidth(px: number) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: px })
  act(() => { window.dispatchEvent(new Event('resize')) })
}

function sidebarMode(): string | null {
  return document.querySelector('aside[data-mode="rail"], aside[data-mode="full"]')?.getAttribute('data-mode') ?? null
}

beforeAll(async () => {
  installDomPolyfills()
  await switchToEnglish()
})

beforeEach(() => {
  useUiStore.setState({ activeView: 'chat', sidebarOpen: true, commandPaletteOpen: false, settingsTab: 'appearance' })
  setWidth(1280)
})

afterEach(() => {
  useUiStore.setState({ commandPaletteOpen: false })
})

describe('view registry', () => {
  it('has one entry per app view, keyed by its own id', () => {
    for (const view of APP_VIEWS) {
      expect(VIEW_REGISTRY[view].id).toBe(view)
    }
    expect(Object.keys(VIEW_REGISTRY).sort()).toEqual([...APP_VIEWS].sort())
  })

  it('groups the sidebar as Chat / Configure / Monitor with Storage last in Configure', () => {
    expect(NAV_GROUPS.map((g) => g.id)).toEqual(['chat', 'configure', 'monitor'])
    expect(NAV_GROUPS[0].views).toEqual(['sessions', 'teams'])
    const configure = NAV_GROUPS[1].views
    expect(configure[configure.length - 1]).toBe('storage')
    expect(NAV_VIEWS).not.toContain('chat')
    expect(NAV_VIEWS).not.toContain('team-board')
    // Every view except home chat and the team board is reachable from the sidebar.
    expect([...NAV_VIEWS].sort()).toEqual(APP_VIEWS.filter((v) => v !== 'chat' && v !== 'team-board').sort())
  })

  it('maps the team board onto the Teams item', () => {
    expect(navItemFor('team-board')).toBe('teams')
    expect(navItemFor('chat')).toBeNull()
    expect(navItemFor('providers')).toBe('providers')
  })

  it('has an English label for every view', () => {
    const labels = en.nav.views as Record<string, string>
    for (const view of APP_VIEWS) expect(labels[view]).toBeTruthy()
  })
})

describe('navigation', () => {
  it('opens every sidebar view and marks only that item current', { timeout: 20000 }, async () => {
    render(<AppShell />)
    expect(await screen.findByText('view:chat')).toBeInTheDocument()
    const nav = screen.getByRole('navigation', { name: 'Main navigation' })
    for (const view of NAV_VIEWS) {
      const label = (en.nav.views as Record<string, string>)[view]
      fireEvent.click(within(nav).getByRole('button', { name: new RegExp(`^${label}`) }))
      expect(await screen.findByText(`view:${view}`)).toBeInTheDocument()
      const current = within(nav).getAllByRole('button').filter((b) => b.getAttribute('aria-current') === 'page')
      expect(current).toHaveLength(1)
      expect(current[0]).toHaveTextContent(label)
      expect(within(current[0]).getByTestId('nav-active-marker')).toBeInTheDocument()
    }
  })

  it('shows the breadcrumb of the current view in the title bar', async () => {
    render(<AppShell />)
    act(() => { useUiStore.getState().setActiveView('providers') })
    const crumbs = screen.getByRole('navigation', { name: 'Location' })
    expect(within(crumbs).getByText('Providers')).toHaveAttribute('aria-current', 'page')
  })

  it('redirects legacy settings tabs to their own views', async () => {
    render(<AppShell />)
    act(() => { useUiStore.getState().openSettings('channels') })
    expect(await screen.findByText('view:channels')).toBeInTheDocument()
    act(() => { useUiStore.getState().openSettings('about') })
    expect(await screen.findByText('view:settings')).toBeInTheDocument()
    expect(useUiStore.getState().settingsTab).toBe('about')
  })

  it('highlights Teams while the team board is open', async () => {
    render(<AppShell />)
    act(() => { useUiStore.getState().openTeamBoard('t1') })
    expect(await screen.findByText('view:team-board')).toBeInTheDocument()
    const nav = screen.getByRole('navigation', { name: 'Main navigation' })
    expect(within(nav).getByRole('button', { name: /^Teams/ })).toHaveAttribute('aria-current', 'page')
  })

  it('shows the pending pairing count on Channels', () => {
    render(<AppShell />)
    expect(screen.getByLabelText('2 pending pairing requests')).toBeInTheDocument()
  })

  it('keeps the keyboard shortcuts: Ctrl+, settings, Esc back to chat, Ctrl+N new chat, Ctrl+K palette', async () => {
    render(<AppShell />)
    fireEvent.keyDown(window, { key: ',', ctrlKey: true })
    expect(await screen.findByText('view:settings')).toBeInTheDocument()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(await screen.findByText('view:chat')).toBeInTheDocument()
    act(() => { useUiStore.getState().setActiveView('cron') })
    fireEvent.keyDown(window, { key: 'n', ctrlKey: true })
    expect(useUiStore.getState().activeView).toBe<AppView>('chat')
    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
    expect(await screen.findByRole('dialog', { name: 'Command palette' })).toBeInTheDocument()
  })
})

describe('sidebar rail', () => {
  it('shows the full sidebar at 1100px and wider; Ctrl+B toggles the rail', () => {
    setWidth(1100)
    render(<AppShell />)
    expect(sidebarMode()).toBe('full')
    fireEvent.keyDown(window, { key: 'b', ctrlKey: true })
    expect(sidebarMode()).toBe('rail')
    fireEvent.keyDown(window, { key: 'b', ctrlKey: true })
    expect(sidebarMode()).toBe('full')
  })

  it('collapses to icons with labels below 1100px, regardless of the saved preference', () => {
    setWidth(1099)
    render(<AppShell />)
    expect(sidebarMode()).toBe('rail')
    const nav = screen.getByRole('navigation', { name: 'Main navigation' })
    // Icon-only items keep their accessible names (shown as tooltips).
    expect(within(nav).getByRole('button', { name: 'Agents' })).toBeInTheDocument()
    expect(within(nav).queryByText('Configure')).toBeNull()
  })

  it('switches mode live when the window is resized', () => {
    render(<AppShell />)
    expect(sidebarMode()).toBe('full')
    setWidth(960)
    expect(sidebarMode()).toBe('rail')
    setWidth(1280)
    expect(sidebarMode()).toBe('full')
  })

  it('expands as an overlay when narrow, and Ctrl+B / Esc / navigation close it', async () => {
    setWidth(1000)
    render(<AppShell />)
    const overlay = () => document.querySelector('aside[data-mode="overlay"]')
    fireEvent.click(screen.getByRole('button', { name: 'Expand sidebar' }))
    expect(overlay()).not.toBeNull()
    expect(sidebarMode()).toBe('rail') // the rail stays underneath
    fireEvent.keyDown(window, { key: 'b', ctrlKey: true })
    expect(overlay()).toBeNull()
    fireEvent.keyDown(window, { key: 'b', ctrlKey: true })
    expect(overlay()).not.toBeNull()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(overlay()).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Expand sidebar' }))
    const navs = screen.getAllByRole('navigation', { name: 'Main navigation' })
    fireEvent.click(within(navs[navs.length - 1]).getByRole('button', { name: /^Skills/ }))
    expect(overlay()).toBeNull()
    expect(await screen.findByText('view:skills')).toBeInTheDocument()
  })

  it('closes the overlay when the window grows past the breakpoint', () => {
    setWidth(1000)
    render(<AppShell />)
    fireEvent.click(screen.getByRole('button', { name: 'Expand sidebar' }))
    setWidth(1280)
    expect(document.querySelector('aside[data-mode="overlay"]')).toBeNull()
    expect(sidebarMode()).toBe('full')
  })
})
