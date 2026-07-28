import { cleanup, render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, describe, expect, it, vi } from 'vitest'
import i18n from '../../i18n'
import type { TeamTaskData } from '../../types/team'
import { TeamTaskListView } from './team-task-list-view'

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('TeamTaskListView workflow badges', () => {
  it('renders step and revision badges on rows for workflow tasks', () => {
    void i18n.changeLanguage('en')
    const task = {
      id: 'task-1', team_id: 'team-1', subject: 'Ship the widget', status: 'in_progress', priority: 1,
      workflow_id: 'workflow-1', workflow_step_id: 'step-7', plan_revision: 4,
    } as TeamTaskData
    render(
      <I18nextProvider i18n={i18n}>
        <TeamTaskListView
          tasks={[task]}
          members={[]}
          loading={false}
          selected={new Set()}
          onSelectChange={vi.fn()}
          onTaskClick={vi.fn()}
          onBulkDelete={vi.fn()}
        />
      </I18nextProvider>,
    )
    expect(screen.getByText('step-7')).toBeInTheDocument()
    expect(screen.getByText('r4')).toBeInTheDocument()
  })

  it('omits workflow badges when the task has no workflow_id', () => {
    void i18n.changeLanguage('en')
    const task = {
      id: 'task-1', team_id: 'team-1', subject: 'Plain task', status: 'pending', priority: 2,
    } as TeamTaskData
    render(
      <I18nextProvider i18n={i18n}>
        <TeamTaskListView
          tasks={[task]}
          members={[]}
          loading={false}
          selected={new Set()}
          onSelectChange={vi.fn()}
          onTaskClick={vi.fn()}
          onBulkDelete={vi.fn()}
        />
      </I18nextProvider>,
    )
    expect(screen.queryByText(/^r\d+$/)).not.toBeInTheDocument()
  })
})
