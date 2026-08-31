import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, describe, expect, it, vi } from 'vitest'
import i18n from '../../i18n'
import type { TeamTaskData, TeamWorkflowDetailResponse } from '../../types/team'

const handlers = vi.hoisted(() => new Map<string, (payload: unknown) => void>())
const getWorkflow = vi.hoisted(() => vi.fn())
const applyWorkflowAction = vi.hoisted(() => vi.fn())
const toast = vi.hoisted(() => ({ success: vi.fn(), warning: vi.fn(), error: vi.fn() }))

vi.mock('../../lib/ws', () => ({
  getWsClient: () => ({ on: (event: string, handler: (payload: unknown) => void) => { handlers.set(event, handler); return () => handlers.delete(event) } }),
}))
vi.mock('../../services/team-service', () => ({ teamService: { getWorkflow, applyWorkflowAction } }))
vi.mock('../../stores/toast-store', () => ({ toast }))
vi.mock('../chat/MarkdownRenderer', () => ({ MarkdownRenderer: () => null }))

import { TaskDetailModal } from './TaskDetailModal'

const task = {
  id: 'task-1', team_id: 'team-1', subject: 'Blocked work', status: 'blocked', workflow_id: 'workflow-1',
  created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
} as TeamTaskData

function workflow(status = 'running', outcome?: 'applied' | 'already_applied' | 'conflict'): TeamWorkflowDetailResponse & { outcome?: typeof outcome } {
  return {
    workflow: {
      id: 'workflow-1', team_id: 'team-1', status, plan_revision: 7, coordinator_agent_key: 'coordinator',
      delivery_status: 'dead', expansion_attempt_count: 3, delivery_attempt_count: 2,
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    },
    tasks: [{
      id: 'blocked-step', subject: 'Repair', status: 'blocked', workflow_step_id: 'step-1', workflow_kind: 'work',
      workflow_terminal: false, plan_revision: 7, recovery_count: 0, dispatch_count: 0, progress_percent: 0,
      blocker_reason: 'Missing upstream artifact',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    }],
    allowed_actions: ['retry_blocked'],
    ...(outcome ? { action: 'retry_blocked' as const, outcome } : {}),
  }
}

function renderModal(language = 'en') {
  void i18n.changeLanguage(language)
  getWorkflow.mockResolvedValueOnce(workflow())
  getWorkflow.mockResolvedValue(workflow('needs_revision'))
  applyWorkflowAction.mockResolvedValue(workflow('needs_revision', 'applied'))
  render(
    <I18nextProvider i18n={i18n}>
      <TaskDetailModal task={task} members={[]} onClose={vi.fn()} onAssign={vi.fn()} onDelete={vi.fn()} />
    </I18nextProvider>,
  )
}

afterEach(() => {
  cleanup()
  handlers.clear()
  vi.clearAllMocks()
  vi.useRealTimers()
})

describe('TaskDetailModal workflow actions', () => {
  it('shows only allowed actions and sends exact guarded payload without token fields', async () => {
    renderModal()
    fireEvent.click(await screen.findByRole('button', { name: 'Retry blocked step' }))
    fireEvent.change(screen.getByLabelText('Reason'), { target: { value: 'operator reason' } })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm' }))

    await waitFor(() => expect(applyWorkflowAction).toHaveBeenCalledOnce())
    expect(applyWorkflowAction).toHaveBeenCalledWith({
      teamId: 'team-1', workflowId: 'workflow-1', action: 'retry_blocked',
      expectedStatus: 'running', expectedPlanRevision: 7,
      taskId: 'blocked-step', expectedTaskStatus: 'blocked', reason: 'operator reason',
    })
    const payload = applyWorkflowAction.mock.calls[0]![0] as Record<string, unknown>
    expect(payload).not.toHaveProperty('token')
    expect(payload).not.toHaveProperty('expectedToken')
    expect(await screen.findByText('Needs revision')).toBeInTheDocument()
  })

  it.each(['applied', 'already_applied', 'conflict'] as const)('uses the authoritative %s response to replace visible workflow state', async (outcome) => {
    getWorkflow.mockResolvedValueOnce(workflow())
    getWorkflow.mockResolvedValue(workflow('completed'))
    applyWorkflowAction.mockResolvedValue(workflow('completed', outcome))
    render(
      <I18nextProvider i18n={i18n}>
        <TaskDetailModal task={task} members={[]} onClose={vi.fn()} onAssign={vi.fn()} onDelete={vi.fn()} />
      </I18nextProvider>,
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Retry blocked step' }))
    fireEvent.change(screen.getByLabelText('Reason'), { target: { value: 'operator reason' } })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm' }))
    expect(await screen.findByText('Completed')).toBeInTheDocument()
  })

  it('renders the current blocker reason and attempt/delivery detail', async () => {
    renderModal()
    expect(await screen.findByText('Blocker: Missing upstream artifact')).toBeInTheDocument()
    expect(screen.getByText('Expansion attempts: 3')).toBeInTheDocument()
    expect(screen.getByText('Delivery attempts: 2')).toBeInTheDocument()
    expect(screen.getByText('Delivery: Dead')).toBeInTheDocument()
  })

  it.each(['applied', 'already_applied', 'conflict'] as const)('replaces state, refetches workflow, and refetches board on %s outcome', async (outcome) => {
    void i18n.changeLanguage('en')
    getWorkflow.mockResolvedValueOnce(workflow())
    getWorkflow.mockResolvedValue(workflow('completed'))
    applyWorkflowAction.mockResolvedValue(workflow('completed', outcome))
    const onWorkflowChanged = vi.fn()
    render(
      <I18nextProvider i18n={i18n}>
        <TaskDetailModal task={task} members={[]} onClose={vi.fn()} onAssign={vi.fn()} onDelete={vi.fn()} onWorkflowChanged={onWorkflowChanged} />
      </I18nextProvider>,
    )
    await waitFor(() => expect(getWorkflow).toHaveBeenCalledTimes(1))
    fireEvent.click(await screen.findByRole('button', { name: 'Retry blocked step' }))
    fireEvent.change(screen.getByLabelText('Reason'), { target: { value: 'operator reason' } })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm' }))

    // 1. Authoritative response replaces visible state.
    expect(await screen.findByText('Completed')).toBeInTheDocument()
    // 2. Explicit workflow refetch fires after the action (initial load + post-action).
    await waitFor(() => expect(getWorkflow).toHaveBeenCalledTimes(2))
    // 3. Board refetch fires.
    expect(onWorkflowChanged).toHaveBeenCalledOnce()
  })

  it('debounces matching workflow updates into one detail refetch', async () => {
    vi.useFakeTimers()
    renderModal()
    await act(async () => { await Promise.resolve() })
    expect(getWorkflow).toHaveBeenCalledTimes(1)

    act(() => {
      handlers.get('team.workflow.updated')?.({ team_id: 'other-team', workflow_id: 'workflow-1' })
      handlers.get('team.workflow.updated')?.({ team_id: 'team-1', workflow_id: 'workflow-1' })
      handlers.get('team.workflow.updated')?.({ team_id: 'team-1', workflow_id: 'workflow-1' })
      vi.advanceTimersByTime(299)
    })
    expect(getWorkflow).toHaveBeenCalledTimes(1)
    await act(async () => { vi.advanceTimersByTime(1); await Promise.resolve() })
    expect(getWorkflow).toHaveBeenCalledTimes(2)
  })
})
