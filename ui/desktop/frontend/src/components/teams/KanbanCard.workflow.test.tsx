import { cleanup, render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, describe, expect, it, vi } from 'vitest'
import i18n from '../../i18n'
import type { TeamTaskData } from '../../types/team'
import { KanbanCard } from './KanbanCard'

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

const baseTask = {
  id: 'task-1', team_id: 'team-1', subject: 'Ship the widget', status: 'in_progress', priority: 1,
} as TeamTaskData

describe('KanbanCard workflow badges', () => {
  it('renders step, revision, and translated status badges for workflow tasks', () => {
    void i18n.changeLanguage('en')
    const task = {
      ...baseTask,
      workflow_id: 'workflow-1',
      workflow_step_id: 'step-7',
      plan_revision: 4,
      status: 'in_review',
    } as TeamTaskData
    render(
      <I18nextProvider i18n={i18n}>
        <KanbanCard task={task} onClick={vi.fn()} />
      </I18nextProvider>,
    )
    expect(screen.getByText('step-7')).toBeInTheDocument()
    expect(screen.getByText('r4')).toBeInTheDocument()
    expect(screen.getByText('In review')).toBeInTheDocument()
  })

  it('does not render workflow badges when the task has no workflow_id', () => {
    void i18n.changeLanguage('en')
    render(
      <I18nextProvider i18n={i18n}>
        <KanbanCard task={baseTask} onClick={vi.fn()} />
      </I18nextProvider>,
    )
    expect(screen.queryByText(/^r\d+$/)).not.toBeInTheDocument()
  })
})
