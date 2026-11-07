import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { useUiStore } from '../../../stores/ui-store'
import { installDomPolyfills, switchToEnglish } from '../../ui/__tests__/test-helpers'
import { Titlebar } from '../Titlebar'

beforeAll(async () => {
  installDomPolyfills()
  await switchToEnglish()
})

afterEach(() => {
  delete window.runtime
})

function installRuntime(maximised = false) {
  const runtime = {
    WindowMinimise: vi.fn(),
    WindowToggleMaximise: vi.fn(),
    WindowIsMaximised: vi.fn(async () => maximised),
    Quit: vi.fn(),
  }
  window.runtime = runtime
  return runtime
}

describe('Titlebar', () => {
  it('drives the Wails window controls', async () => {
    const rt = installRuntime()
    render(<Titlebar />)
    fireEvent.click(screen.getByRole('button', { name: 'Minimize' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Maximize' }))
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(rt.WindowMinimise).toHaveBeenCalledOnce()
    expect(rt.WindowToggleMaximise).toHaveBeenCalledOnce()
    expect(rt.Quit).toHaveBeenCalledOnce()
  })

  it('labels the middle button Restore while maximised', async () => {
    installRuntime(true)
    render(<Titlebar />)
    expect(await screen.findByRole('button', { name: 'Restore' })).toBeInTheDocument()
  })

  it('toggles maximise on double-click of the drag region, not the controls', () => {
    const rt = installRuntime()
    render(<Titlebar />)
    fireEvent.doubleClick(screen.getByText('GoClaw'))
    expect(rt.WindowToggleMaximise).toHaveBeenCalledOnce()
    fireEvent.doubleClick(screen.getByRole('button', { name: 'Minimize' }))
    expect(rt.WindowToggleMaximise).toHaveBeenCalledOnce()
  })

  it('works without the Wails runtime (browser, tests)', () => {
    render(<Titlebar />)
    expect(() => fireEvent.click(screen.getByRole('button', { name: 'Close' }))).not.toThrow()
  })

  it('opens the command palette from the centre trigger', () => {
    act(() => { useUiStore.setState({ commandPaletteOpen: false }) })
    render(<Titlebar />)
    fireEvent.click(screen.getByRole('button', { name: /Search or run a command/ }))
    expect(useUiStore.getState().commandPaletteOpen).toBe(true)
  })

  it('minimal mode keeps only the mark and window controls', () => {
    render(<Titlebar minimal />)
    expect(screen.queryByRole('button', { name: /Search or run a command/ })).toBeNull()
    expect(screen.getByRole('button', { name: 'Close' })).toBeInTheDocument()
  })
})
