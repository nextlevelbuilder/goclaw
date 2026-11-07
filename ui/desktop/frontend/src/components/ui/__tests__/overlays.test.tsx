import { useState } from 'react'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { ConfirmDialog } from '../ConfirmDialog'
import { Dialog, DialogFooter } from '../Dialog'
import { Menu } from '../Menu'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../Tabs'
import { installDomPolyfills, switchToEnglish } from './test-helpers'

beforeAll(async () => {
  installDomPolyfills()
  await switchToEnglish()
})

describe('Dialog', () => {
  function Harness({ onOpenChange }: { onOpenChange?: (o: boolean) => void }) {
    const [open, setOpen] = useState(true)
    return (
      <Dialog
        open={open}
        onOpenChange={(o) => { setOpen(o); onOpenChange?.(o) }}
        title="Edit team"
        description="Change the team name."
        footer={(
          <DialogFooter destructive={<button type="button">Delete team</button>}>
            <button type="button">Cancel</button>
            <button type="button">Save changes</button>
          </DialogFooter>
        )}
      >
        <input aria-label="Team name" />
      </Dialog>
    )
  }

  it('is a labelled, described dialog', () => {
    render(<Harness />)
    const dialog = screen.getByRole('dialog', { name: 'Edit team' })
    expect(dialog).toHaveAccessibleDescription('Change the team name.')
    expect(dialog.className).toContain('shadow-popover')
  })

  it('traps focus inside', () => {
    render(<Harness />)
    expect(screen.getByRole('dialog').contains(document.activeElement)).toBe(true)
  })

  it('closes on Escape and from the close button', () => {
    const onOpenChange = vi.fn()
    const { unmount } = render(<Harness onOpenChange={onOpenChange} />)
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
    expect(onOpenChange).toHaveBeenCalledWith(false)
    expect(screen.queryByRole('dialog')).toBeNull()
    unmount()

    render(<Harness onOpenChange={onOpenChange} />)
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('puts the destructive action on the left of the footer', () => {
    render(<Harness />)
    const del = screen.getByRole('button', { name: 'Delete team' })
    const save = screen.getByRole('button', { name: 'Save changes' })
    expect(del.compareDocumentPosition(save) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(del.parentElement?.className).toContain('mr-auto')
  })

  it('ignores Escape when not dismissible', () => {
    const onOpenChange = vi.fn()
    render(<Dialog open onOpenChange={onOpenChange} title="Saving" dismissible={false} />)
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
    expect(onOpenChange).not.toHaveBeenCalled()
  })
})

describe('ConfirmDialog', () => {
  it('names the object and confirms', () => {
    const onConfirm = vi.fn()
    render(<ConfirmDialog open onOpenChange={() => {}} title="Delete agent research-assistant?" description="This cannot be undone." variant="destructive" confirmLabel="Delete agent" onConfirm={onConfirm} />)
    expect(screen.getByRole('dialog', { name: 'Delete agent research-assistant?' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Delete agent' }))
    expect(onConfirm).toHaveBeenCalledOnce()
  })

  it('requires the typed value before confirming', () => {
    const onConfirm = vi.fn()
    render(<ConfirmDialog open onOpenChange={() => {}} title="Delete 2 agents?" description="Type to confirm." variant="destructive" confirmLabel="Delete" confirmValue="delete" onConfirm={onConfirm} />)
    const confirm = screen.getByRole('button', { name: 'Delete' })
    expect(confirm).toBeDisabled()
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'DELETE ' } })
    expect(confirm).toBeEnabled()
    fireEvent.click(confirm)
    expect(onConfirm).toHaveBeenCalledOnce()
  })

  it('cancel closes without confirming', () => {
    const onOpenChange = vi.fn()
    const onConfirm = vi.fn()
    render(<ConfirmDialog open onOpenChange={onOpenChange} title="Reset?" description="All data is lost." onConfirm={onConfirm} />)
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
    expect(onConfirm).not.toHaveBeenCalled()
  })
})

describe('Menu', () => {
  it('opens from the default row trigger and runs an item', async () => {
    const onEdit = vi.fn()
    const onDelete = vi.fn()
    render(<Menu items={[{ label: 'Edit', onSelect: onEdit }, 'separator', { label: 'Delete agent', onSelect: onDelete, destructive: true }]} />)
    const trigger = screen.getByRole('button', { name: 'More actions' })
    await act(async () => {
      fireEvent.keyDown(trigger, { key: 'Enter' })
    })
    const items = await screen.findAllByRole('menuitem')
    expect(items.map((i) => i.textContent)).toEqual(['Edit', 'Delete agent'])
    expect(items[1].className).toContain('text-danger')
    await act(async () => { fireEvent.click(items[1]) })
    expect(onDelete).toHaveBeenCalledOnce()
    await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
  })
})

describe('Tabs', () => {
  it('switches panels', async () => {
    render(
      <Tabs defaultValue="a">
        <TabsList aria-label="Sections">
          <TabsTrigger value="a">General</TabsTrigger>
          <TabsTrigger value="b">Advanced</TabsTrigger>
        </TabsList>
        <TabsContent value="a">General panel</TabsContent>
        <TabsContent value="b">Advanced panel</TabsContent>
      </Tabs>,
    )
    expect(screen.getByRole('tab', { name: 'General' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tabpanel')).toHaveTextContent('General panel')
    await act(async () => {
      fireEvent.mouseDown(screen.getByRole('tab', { name: 'Advanced' }), { button: 0, ctrlKey: false })
    })
    expect(screen.getByRole('tab', { name: 'Advanced' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tabpanel')).toHaveTextContent('Advanced panel')
  })
})
