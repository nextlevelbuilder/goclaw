import { useState } from 'react'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { DataTable, type DataTableColumn } from '../DataTable'
import { installDomPolyfills, switchToEnglish } from './test-helpers'

beforeAll(async () => {
  installDomPolyfills()
  await switchToEnglish()
})

interface Row { id: string; name: string; model: string }
const rows: Row[] = [
  { id: '1', name: 'research-assistant', model: 'claude-sonnet' },
  { id: '2', name: 'inbox-triage', model: 'gpt-4o-mini' },
  { id: '3', name: 'support-bot', model: 'qwen-max' },
]
const columns: DataTableColumn<Row>[] = [
  { id: 'name', header: 'Agent', cell: (r) => r.name },
  { id: 'model', header: 'Model', cell: (r) => r.model },
]

describe('DataTable', () => {
  it('renders the header row and one row per item', () => {
    render(<DataTable aria-label="Agents" columns={columns} rows={rows} getRowId={(r) => r.id} />)
    const table = screen.getByRole('table', { name: 'Agents' })
    expect(within(table).getAllByRole('columnheader').map((h) => h.textContent)).toEqual(['Agent', 'Model'])
    expect(within(table).getAllByRole('row')).toHaveLength(4)
  })

  it('renders the empty slot when there are no rows', () => {
    render(<DataTable aria-label="Agents" columns={columns} rows={[]} getRowId={(r) => r.id} empty={<p>No agents yet</p>} />)
    expect(screen.getByText('No agents yet')).toBeInTheDocument()
  })

  it('activates a row by click and Enter', () => {
    const onRowClick = vi.fn()
    render(<DataTable aria-label="Agents" columns={columns} rows={rows} getRowId={(r) => r.id} onRowClick={onRowClick} />)
    const row = screen.getByText('inbox-triage').closest('tr')!
    fireEvent.click(row)
    fireEvent.keyDown(row, { key: 'Enter' })
    expect(onRowClick).toHaveBeenCalledTimes(2)
    expect(onRowClick).toHaveBeenCalledWith(rows[1])
  })

  it('selects rows, shows the bulk bar and clears it', () => {
    const onBulk = vi.fn()
    render(
      <DataTable
        aria-label="Agents" columns={columns} rows={rows} getRowId={(r) => r.id} selectable
        bulkActions={(ids) => <button type="button" onClick={() => onBulk(ids)}>Delete</button>}
      />,
    )
    expect(screen.queryByRole('toolbar')).toBeNull()
    const [first, second] = screen.getAllByRole('checkbox', { name: 'Select row' })
    fireEvent.click(first)
    fireEvent.click(second)
    const bar = screen.getByRole('toolbar', { name: '2 selected' })
    expect(screen.getByText('research-assistant').closest('tr')).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('checkbox', { name: 'Select all rows' })).toHaveAttribute('aria-checked', 'mixed')
    fireEvent.click(within(bar).getByRole('button', { name: 'Delete' }))
    expect(onBulk).toHaveBeenCalledWith(['1', '2'])
    fireEvent.click(within(bar).getByRole('button', { name: 'Clear selection' }))
    expect(screen.queryByRole('toolbar')).toBeNull()
  })

  it('select all toggles every row in controlled mode', () => {
    function Harness() {
      const [sel, setSel] = useState<string[]>([])
      return (
        <>
          <DataTable aria-label="Agents" columns={columns} rows={rows} getRowId={(r) => r.id} selectable selectedIds={sel} onSelectionChange={setSel} />
          <output>{sel.join(',')}</output>
        </>
      )
    }
    render(<Harness />)
    const all = screen.getByRole('checkbox', { name: 'Select all rows' })
    fireEvent.click(all)
    expect(screen.getByRole('status')).toHaveTextContent('1,2,3')
    fireEvent.click(all)
    expect(screen.getByRole('status')).toHaveTextContent('')
  })

  it('selecting a checkbox does not activate the row', () => {
    const onRowClick = vi.fn()
    render(<DataTable aria-label="Agents" columns={columns} rows={rows} getRowId={(r) => r.id} selectable onRowClick={onRowClick} />)
    fireEvent.click(screen.getAllByRole('checkbox', { name: 'Select row' })[0])
    expect(onRowClick).not.toHaveBeenCalled()
  })
})
