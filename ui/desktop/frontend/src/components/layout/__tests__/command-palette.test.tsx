import { beforeAll, beforeEach, describe, expect, it } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { useAgentStore } from '../../../stores/agent-store'
import { useSessionStore } from '../../../stores/session-store'
import { useUiStore } from '../../../stores/ui-store'
import { installDomPolyfills, switchToEnglish } from '../../ui/__tests__/test-helpers'
import { buildPaletteItems, normalizeSearch } from '../command-palette-items'
import { CommandPalette } from '../CommandPalette'

const t = (k: string) => k

const agents = [
  { id: 'a1', key: 'research-assistant', name: 'Research assistant', model: 'm', status: 'online' as const },
  { id: 'a2', key: 'tro-ly', name: 'Trợ lý tiếng Việt', model: 'm', status: 'online' as const },
]
const sessions = [
  { key: 's-old', agentId: 'a1', title: 'Research notes: suppliers', lastMessageAt: 1, messageCount: 3 },
  { key: 's-new', agentId: 'a1', title: 'Summarize failed cron runs', lastMessageAt: 2, messageCount: 5 },
]

beforeAll(async () => {
  installDomPolyfills()
  await switchToEnglish()
})

beforeEach(() => {
  act(() => {
    useAgentStore.setState({ agents, selectedAgentId: 'a1' })
    useSessionStore.setState({ sessions, activeSessionKey: null })
    useUiStore.setState({ activeView: 'chat', commandPaletteOpen: false, theme: 'dark' })
  })
})

describe('buildPaletteItems', () => {
  it('lists actions, views, agents and recent sessions for an empty query', () => {
    const items = buildPaletteItems({ query: '', agents, sessions, t })
    const groups = [...new Set(items.map((i) => i.group))]
    expect(groups).toEqual(['actions', 'views', 'agents', 'sessions'])
    expect(items.filter((i) => i.group === 'sessions').map((i) => i.label)).toEqual([
      'Summarize failed cron runs',
      'Research notes: suppliers',
    ])
  })

  it('filters every group case- and accent-insensitively', () => {
    const items = buildPaletteItems({ query: 'research', agents, sessions, t })
    expect(items.map((i) => i.id)).toEqual(['agent:a1', 'session:s-old'])
    expect(buildPaletteItems({ query: 'tieng viet', agents, sessions, t }).map((i) => i.id)).toEqual(['agent:a2'])
    expect(buildPaletteItems({ query: 'tro-ly', agents, sessions, t }).map((i) => i.id)).toEqual(['agent:a2'])
  })

  it('normalizes Vietnamese text', () => {
    expect(normalizeSearch('  Đường Tiếng ')).toBe('duong tieng')
  })
})

describe('CommandPalette', () => {
  function open() {
    render(<CommandPalette />)
    act(() => { useUiStore.getState().setCommandPaletteOpen(true) })
    return screen.getByRole('combobox', { name: 'Command palette' })
  }

  const selected = () => screen.getAllByRole('option').find((o) => o.getAttribute('aria-selected') === 'true')

  it('focuses the search box and selects the first result', () => {
    const input = open()
    expect(screen.getByRole('dialog', { name: 'Command palette' })).toBeInTheDocument()
    expect(input).toHaveFocus()
    expect(selected()).toHaveTextContent('New chat')
    expect(input).toHaveAttribute('aria-activedescendant', selected()!.id)
  })

  it('moves with the arrow keys, wraps, and Enter opens a view', () => {
    const input = open()
    fireEvent.change(input, { target: { value: 'prov' } })
    expect(selected()).toHaveTextContent('Providers')
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(useUiStore.getState().activeView).toBe('providers')
    expect(useUiStore.getState().commandPaletteOpen).toBe(false)
  })

  it('ArrowUp wraps to the last item and ArrowDown back to the first', () => {
    const input = open()
    fireEvent.change(input, { target: { value: 'research' } })
    expect(selected()).toHaveTextContent('Research assistant')
    fireEvent.keyDown(input, { key: 'ArrowUp' })
    expect(selected()).toHaveTextContent('Research notes: suppliers')
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(selected()).toHaveTextContent('Research assistant')
  })

  it('Enter on an agent makes it the chat agent', () => {
    const input = open()
    fireEvent.change(input, { target: { value: 'tieng' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(useAgentStore.getState().selectedAgentId).toBe('a2')
    expect(useUiStore.getState().activeView).toBe('chat')
  })

  it('Enter on a session opens it', () => {
    act(() => { useUiStore.setState({ activeView: 'cron' }) })
    const input = open()
    fireEvent.change(input, { target: { value: 'cron runs' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(useSessionStore.getState().activeSessionKey).toBe('s-new')
    expect(useUiStore.getState().activeView).toBe('chat')
  })

  it('runs actions: toggle theme and open settings', () => {
    let input = open()
    fireEvent.change(input, { target: { value: 'theme' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(useUiStore.getState().theme).toBe('light')

    act(() => { useUiStore.getState().setCommandPaletteOpen(true) })
    input = screen.getByRole('combobox', { name: 'Command palette' })
    fireEvent.change(input, { target: { value: 'open settings' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(useUiStore.getState().activeView).toBe('settings')
  })

  it('shows a no-results line', () => {
    const input = open()
    fireEvent.change(input, { target: { value: 'zzzz' } })
    expect(screen.queryAllByRole('option')).toHaveLength(0)
    expect(screen.getByText('No results for "zzzz"')).toBeInTheDocument()
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(useUiStore.getState().activeView).toBe('chat')
  })

  it('Escape closes and clears the query', () => {
    const input = open()
    fireEvent.change(input, { target: { value: 'agents' } })
    fireEvent.keyDown(input, { key: 'Escape' })
    expect(useUiStore.getState().commandPaletteOpen).toBe(false)
    expect(screen.queryByRole('dialog')).toBeNull()
    act(() => { useUiStore.getState().setCommandPaletteOpen(true) })
    expect(screen.getByRole('combobox', { name: 'Command palette' })).toHaveValue('')
  })
})
