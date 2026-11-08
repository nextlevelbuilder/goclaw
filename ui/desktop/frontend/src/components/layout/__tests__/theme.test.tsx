import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { applyTheme, resolveTheme, toThemePreference } from '../../../lib/theme'
import { migrateUiState, useUiStore } from '../../../stores/ui-store'
import { setPrefersDark } from '../../ui/__tests__/test-helpers'
import { useResolvedTheme } from '../use-resolved-theme'

afterEach(() => {
  document.documentElement.classList.remove('dark')
})

describe('resolveTheme', () => {
  it('follows the OS for "system"', () => {
    expect(resolveTheme('system', true)).toBe('dark')
    expect(resolveTheme('system', false)).toBe('light')
  })

  it('keeps an explicit choice regardless of the OS', () => {
    expect(resolveTheme('light', true)).toBe('light')
    expect(resolveTheme('dark', false)).toBe('dark')
  })

  it('reads matchMedia when no preference is passed', () => {
    setPrefersDark(true)
    expect(resolveTheme('system')).toBe('dark')
    setPrefersDark(false)
    expect(resolveTheme('system')).toBe('light')
  })
})

describe('applyTheme', () => {
  it('toggles the .dark class on <html>', () => {
    applyTheme('dark')
    expect(document.documentElement).toHaveClass('dark')
    applyTheme('light')
    expect(document.documentElement).not.toHaveClass('dark')
  })
})

describe('theme preference persistence', () => {
  it('defaults unknown values to system', () => {
    expect(toThemePreference(undefined)).toBe('system')
    expect(toThemePreference('sepia')).toBe('system')
    expect(toThemePreference('dark')).toBe('dark')
  })

  it('migrates older persisted state', () => {
    expect(migrateUiState({ theme: 'light', settingsTab: 'providers', locale: 'en' })).toMatchObject({
      theme: 'light',
      settingsTab: 'appearance',
      locale: 'en',
    })
    expect(migrateUiState({ settingsTab: 'about' })).toMatchObject({ theme: 'system', settingsTab: 'about' })
    expect(migrateUiState(null)).toMatchObject({ theme: 'system', settingsTab: 'appearance' })
  })
})

describe('useResolvedTheme', () => {
  beforeEach(() => {
    act(() => { useUiStore.setState({ theme: 'system' }) })
  })

  it('tracks OS changes while the preference is system', () => {
    const listeners = setPrefersDark(false)
    const { result } = renderHook(() => useResolvedTheme())
    expect(result.current).toBe('light')

    // The OS flips to dark: matchMedia now reports dark and fires its change event.
    setPrefersDark(true)
    act(() => { listeners.forEach((l) => l({ matches: true } as MediaQueryListEvent)) })
    expect(result.current).toBe('dark')
  })

  it('switches immediately when the user picks a theme', () => {
    setPrefersDark(true)
    const { result } = renderHook(() => useResolvedTheme())
    expect(result.current).toBe('dark')
    act(() => { useUiStore.getState().setTheme('light') })
    expect(result.current).toBe('light')
    act(() => { useUiStore.getState().setTheme('system') })
    expect(result.current).toBe('dark')
  })
})

describe('toggleTheme', () => {
  it('flips an explicit theme', () => {
    act(() => { useUiStore.setState({ theme: 'dark' }) })
    act(() => { useUiStore.getState().toggleTheme() })
    expect(useUiStore.getState().theme).toBe('light')
  })

  it('leaves system mode for the opposite of what is painted', () => {
    act(() => { useUiStore.setState({ theme: 'system' }) })
    applyTheme('dark')
    act(() => { useUiStore.getState().toggleTheme() })
    expect(useUiStore.getState().theme).toBe('light')
  })
})
