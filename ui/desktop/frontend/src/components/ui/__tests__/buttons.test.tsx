import { beforeAll, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { Button } from '../Button'
import { IconButton } from '../IconButton'
import { Tooltip } from '../Tooltip'
import { installDomPolyfills, switchToEnglish } from './test-helpers'

beforeAll(async () => {
  installDomPolyfills()
  await switchToEnglish()
})

describe('Button', () => {
  it('renders each variant with its own styling and defaults to type=button', () => {
    for (const variant of ['primary', 'secondary', 'ghost', 'danger'] as const) {
      const { unmount } = render(<Button variant={variant}>Go</Button>)
      const btn = screen.getByRole('button', { name: 'Go' })
      expect(btn).toHaveAttribute('data-variant', variant)
      expect(btn).toHaveAttribute('type', 'button')
      unmount()
    }
    render(<Button variant="primary">P</Button>)
    expect(screen.getByRole('button', { name: 'P' }).className).toContain('bg-accent')
  })

  it('applies size classes', () => {
    render(<><Button size="sm">S</Button><Button size="md">M</Button></>)
    expect(screen.getByRole('button', { name: 'S' }).className).toContain('h-7')
    expect(screen.getByRole('button', { name: 'M' }).className).toContain('h-control')
  })

  it('fires onClick', () => {
    const onClick = vi.fn()
    render(<Button onClick={onClick}>Save</Button>)
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onClick).toHaveBeenCalledOnce()
  })

  it('while loading shows a spinner, keeps the label, is busy and ignores clicks', () => {
    const onClick = vi.fn()
    render(<Button loading onClick={onClick}>Saving</Button>)
    const btn = screen.getByRole('button', { name: 'Saving' })
    expect(btn).toBeDisabled()
    expect(btn).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByTestId('spinner')).toBeInTheDocument()
    fireEvent.click(btn)
    expect(onClick).not.toHaveBeenCalled()
  })
})

describe('IconButton', () => {
  it('uses aria-label as its accessible name', () => {
    render(<IconButton aria-label="Delete agent" icon={<svg />} />)
    expect(screen.getByRole('button', { name: 'Delete agent' })).toBeInTheDocument()
  })

  it('shows the label as a tooltip on keyboard focus', async () => {
    render(<IconButton aria-label="Open settings" icon={<svg />} />)
    await act(async () => { screen.getByRole('button', { name: 'Open settings' }).focus() })
    expect(await screen.findByRole('tooltip')).toHaveTextContent('Open settings')
  })

  it('can suppress the tooltip', async () => {
    render(<IconButton aria-label="Close" icon={<svg />} tooltip={false} />)
    await act(async () => { screen.getByRole('button', { name: 'Close' }).focus() })
    expect(screen.queryByRole('tooltip')).toBeNull()
  })
})

describe('Tooltip', () => {
  it('renders the child alone when content is empty', () => {
    render(<Tooltip content=""><button type="button">Plain</button></Tooltip>)
    expect(screen.getByRole('button', { name: 'Plain' })).toBeInTheDocument()
  })

  it('opens on focus with the given content', async () => {
    render(<Tooltip content="More detail"><button type="button">Info</button></Tooltip>)
    await act(async () => { screen.getByRole('button', { name: 'Info' }).focus() })
    expect(await screen.findByRole('tooltip')).toHaveTextContent('More detail')
  })
})
