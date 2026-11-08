import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import i18n from '../../../i18n'
import { SkillTable } from '../skill-card'
import type { SkillInfo } from '../../../types/skill'

const custom: SkillInfo = { id: 's1', name: 'pdf-reader', description: 'Reads PDFs', source: 'upload', enabled: true, status: 'active' }
const system: SkillInfo = { id: 's2', name: 'core-search', description: '', source: 'system', is_system: true, enabled: false, status: 'archived', missing_deps: ['python'] }

function renderTable(props: Partial<React.ComponentProps<typeof SkillTable>> = {}) {
  const handlers = { onToggle: vi.fn(), onEdit: vi.fn(), onDelete: vi.fn() }
  render(
    <I18nextProvider i18n={i18n}>
      <SkillTable skills={[custom, system]} {...handlers} {...props} />
    </I18nextProvider>,
  )
  return handlers
}

describe('SkillTable', () => {
  it('shows each skill with an enable switch named after it', () => {
    renderTable()
    expect(screen.getByText('pdf-reader')).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: /pdf-reader/i })).toBeChecked()
    expect(screen.getByRole('switch', { name: /core-search/i })).not.toBeChecked()
  })

  it('reports a toggle with the skill and the new value', () => {
    const { onToggle } = renderTable()
    fireEvent.click(screen.getByRole('switch', { name: /pdf-reader/i }))
    expect(onToggle).toHaveBeenCalledWith(custom, false)
  })

  it('offers a row menu only for skills the user owns', () => {
    renderTable()
    expect(document.querySelectorAll('button[aria-haspopup="menu"]')).toHaveLength(1)
  })

  it('disables the switches while offline', () => {
    renderTable({ readOnly: true })
    expect(screen.getByRole('switch', { name: /pdf-reader/i })).toBeDisabled()
  })
})
