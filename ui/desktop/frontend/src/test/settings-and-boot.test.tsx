import { describe, it, expect, vi, afterEach } from 'vitest'
import { act, fireEvent, render, renderHook, screen } from '@testing-library/react'

const { isGatewayReady } = vi.hoisted(() => ({ isGatewayReady: vi.fn() }))
vi.mock('../lib/wails', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/wails')>()),
  wails: {
    isGatewayReady,
    getGatewayToken: vi.fn(),
    getGatewayURL: vi.fn(),
    getDataDir: vi.fn().mockResolvedValue('C:\\data'),
    getGatewayPort: vi.fn(),
    getVersion: vi.fn().mockResolvedValue('1.0.0'),
  },
}))

import '../i18n'
import { GATEWAY_MAX_ATTEMPTS, GATEWAY_POLL_MS, useGatewayBoot } from '../hooks/use-gateway-boot'
import { StartFailedScreen } from '../components/common/SplashScreen'
import { SettingsView } from '../components/settings/SettingsView'
import { useUiStore } from '../stores/ui-store'

afterEach(() => {
  vi.useRealTimers()
  isGatewayReady.mockReset()
})

describe('useGatewayBoot', () => {
  it('fails with a timeout when the gateway never answers, and can be retried', async () => {
    vi.useFakeTimers()
    isGatewayReady.mockResolvedValue(false)
    const { result } = renderHook(() => useGatewayBoot())
    expect(result.current.state).toEqual({ status: 'starting', phase: 'gateway' })

    await act(async () => {
      await vi.advanceTimersByTimeAsync(GATEWAY_POLL_MS * (GATEWAY_MAX_ATTEMPTS + 1))
    })
    expect(result.current.state).toEqual({ status: 'failed', reason: 'timeout' })

    act(() => result.current.retry())
    expect(result.current.state).toEqual({ status: 'starting', phase: 'gateway' })
  })
})

describe('StartFailedScreen', () => {
  it('explains the failure and offers Try again', () => {
    const onRetry = vi.fn()
    render(<StartFailedScreen reason="timeout" onRetry={onRetry} />)
    expect(screen.getByRole('alert')).toHaveTextContent(/couldn't start|không khởi động|无法启动|не удалось/i)
    fireEvent.click(screen.getByRole('button'))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })
})

describe('SettingsView', () => {
  it('switches the theme from the Appearance section', () => {
    useUiStore.setState({ theme: 'system', settingsTab: 'appearance' })
    render(<SettingsView />)
    const radios = screen.getAllByRole('radio')
    expect(radios).toHaveLength(3)
    fireEvent.click(radios[2])
    expect(useUiStore.getState().theme).toBe('dark')
  })

  it('lists the five sections', () => {
    render(<SettingsView />)
    expect(screen.getByRole('navigation').querySelectorAll('button')).toHaveLength(5)
  })
})
