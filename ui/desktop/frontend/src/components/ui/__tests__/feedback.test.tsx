import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { useToastStore, toast } from '../../../stores/toast-store'
import { Badge } from '../Badge'
import { EmptyState } from '../EmptyState'
import { ErrorState } from '../ErrorState'
import { OfflineBanner } from '../OfflineBanner'
import { PageHeader } from '../PageHeader'
import { Skeleton, SkeletonList } from '../Skeleton'
import { StatusChip } from '../StatusChip'
import { Toaster } from '../Toast'
import { installDomPolyfills, switchToEnglish } from './test-helpers'

beforeAll(async () => {
  installDomPolyfills()
  await switchToEnglish()
})

describe('StatusChip', () => {
  it('always renders an icon and a word for every tone', () => {
    for (const tone of ['success', 'warning', 'danger', 'info', 'idle'] as const) {
      const { container, unmount } = render(<StatusChip tone={tone} />)
      const chip = container.querySelector(`[data-tone="${tone}"]`)!
      expect(chip.querySelector('svg')).not.toBeNull()
      expect(chip.textContent?.trim().length).toBeGreaterThan(0)
      unmount()
    }
  })

  it('uses the given label', () => {
    render(<StatusChip tone="warning" label="Rate limited" />)
    expect(screen.getByText('Rate limited')).toBeInTheDocument()
  })

  it('falls back to the tone word', () => {
    render(<StatusChip tone="danger" />)
    expect(screen.getByText('Failed')).toBeInTheDocument()
  })
})

describe('Badge', () => {
  it('renders content with a tone', () => {
    render(<Badge tone="accent">Lite</Badge>)
    expect(screen.getByText('Lite').className).toContain('bg-accent-subtle')
  })
})

describe('Skeleton', () => {
  it('is decorative on its own', () => {
    const { container } = render(<Skeleton className="w-10" />)
    expect(container.firstChild).toHaveAttribute('aria-hidden', 'true')
  })

  it('SkeletonList announces loading and renders the rows', () => {
    const { container } = render(<SkeletonList rows={3} />)
    const status = screen.getByRole('status', { name: 'Loading' })
    expect(status).toHaveAttribute('aria-busy', 'true')
    expect(container.querySelectorAll('.skeleton-shimmer').length).toBe(3 * 3)
  })
})

describe('EmptyState', () => {
  it('shows title, sentence and one action', () => {
    const onClick = vi.fn()
    render(<EmptyState title="No teams yet" description="Group agents so they share tasks." action={<button type="button" onClick={onClick}>Create team</button>} />)
    expect(screen.getByText('No teams yet')).toBeInTheDocument()
    expect(screen.getByText('Group agents so they share tasks.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Create team' }))
    expect(onClick).toHaveBeenCalled()
  })
})

describe('ErrorState', () => {
  it('is an alert with defaults, a code and Try again', () => {
    const onRetry = vi.fn()
    render(<ErrorState code="503" onRetry={onRetry} />)
    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent('Something went wrong')
    expect(alert).toHaveTextContent('Code: 503')
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(onRetry).toHaveBeenCalledOnce()
  })

  it('omits the retry button without a handler', () => {
    render(<ErrorState title="Couldn't load teams" />)
    expect(screen.getByText("Couldn't load teams")).toBeInTheDocument()
    expect(screen.queryByRole('button')).toBeNull()
  })
})

describe('OfflineBanner', () => {
  it('shows the countdown and a Retry now button', () => {
    const onRetry = vi.fn()
    render(<OfflineBanner retryInSeconds={12} onRetry={onRetry} />)
    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent("Can't reach the gateway")
    expect(alert).toHaveTextContent('Retrying automatically in 12 seconds.')
    fireEvent.click(screen.getByRole('button', { name: 'Retry now' }))
    expect(onRetry).toHaveBeenCalledOnce()
  })

  it('uses the singular and the reconnecting text', () => {
    const { rerender } = render(<OfflineBanner retryInSeconds={1} />)
    expect(screen.getByRole('alert')).toHaveTextContent('in 1 second.')
    rerender(<OfflineBanner retryInSeconds={null} />)
    expect(screen.getByRole('alert')).toHaveTextContent('Reconnecting')
  })
})

describe('PageHeader', () => {
  it('renders the title as a heading with description and actions', () => {
    render(<PageHeader title="Agents" description="5 agents" actions={<button type="button">New agent</button>} />)
    expect(screen.getByRole('heading', { level: 1, name: 'Agents' })).toBeInTheDocument()
    expect(screen.getByText('5 agents')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'New agent' })).toBeInTheDocument()
  })
})

describe('Toaster', () => {
  afterEach(() => {
    act(() => { useToastStore.setState({ toasts: [] }) })
    vi.useRealTimers()
  })

  it('renders toasts with status/alert roles and dismisses them', () => {
    render(<Toaster />)
    act(() => {
      toast.success('Agent saved', 'research-assistant is up to date.')
      toast.error("Couldn't save", 'The gateway did not respond.')
    })
    expect(screen.getByRole('status')).toHaveTextContent('Agent saved')
    expect(screen.getByRole('alert')).toHaveTextContent("Couldn't save")
    fireEvent.click(screen.getAllByRole('button', { name: 'Dismiss' })[0])
    expect(screen.queryByText('Agent saved')).toBeNull()
  })

  it('runs an inline action and auto-dismisses after the duration', () => {
    vi.useFakeTimers()
    const retry = vi.fn()
    render(<Toaster />)
    act(() => { toast.error('Failed', undefined, { action: { label: 'Try again', onClick: retry } }) })
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(retry).toHaveBeenCalledOnce()
    expect(screen.queryByText('Failed')).toBeNull()

    act(() => { toast.info('Gateway reconnected') })
    expect(screen.getByText('Gateway reconnected')).toBeInTheDocument()
    act(() => { vi.advanceTimersByTime(4000) })
    expect(screen.queryByText('Gateway reconnected')).toBeNull()
  })

  it('shows at most three toasts', () => {
    render(<Toaster />)
    act(() => { ['a', 'b', 'c', 'd'].forEach((x) => toast.info(x)) })
    expect(screen.queryByText('a')).toBeNull()
    expect(screen.getByText('d')).toBeInTheDocument()
  })
})
