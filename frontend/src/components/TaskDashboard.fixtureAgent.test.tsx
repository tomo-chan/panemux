import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { TaskDashboard } from './TaskDashboard'
import type { TasksState } from '../hooks/useTasks'
import { TasksResponseSchema } from '../schemas'

// A third agent added through registration alone (issue #377). The input is
// tasks-fixture-agent, the GET /api/tasks response the Go suite's
// panemux_fixture_agent run captured, so what the board offers on the fixture
// agent's task comes from what that agent declares (detect, collect, launch,
// summary — no resume) and from the task's actions, never from its name.

const response = TasksResponseSchema.parse(
  JSON.parse(readFileSync(resolve(process.cwd(), '..', 'testdata', 'api-contract', 'tasks-fixture-agent.json'), 'utf8')),
)
const fixtureAgent = 'panemux-fixture-agent'
const fixtureTask = response.tasks.find((t) => t.agent === fixtureAgent)!
const NOW = Date.parse('2026-01-01T00:05:00Z')

function tasksState(overrides: Partial<TasksState> = {}): TasksState {
  return {
    data: response,
    error: null,
    loading: false,
    updatedAt: NOW - 8000,
    refresh: vi.fn().mockResolvedValue(undefined),
    reconnect: vi.fn().mockResolvedValue(undefined),
    saveRecord: vi.fn().mockResolvedValue(null),
    launch: vi.fn().mockResolvedValue({ ok: false, error: 'not stubbed' }),
    resume: vi.fn().mockResolvedValue({ ok: false, error: 'not stubbed' }),
    requestSummary: vi.fn().mockResolvedValue(null),
    inputGiven: vi.fn().mockResolvedValue(null),
    attach: vi.fn().mockResolvedValue({ ok: false, error: 'not stubbed' }),
    detach: vi.fn().mockResolvedValue(undefined),
    hostSessionName: vi.fn().mockResolvedValue({ ok: false, error: 'not stubbed' }),
    hostTerminal: vi.fn().mockResolvedValue({ ok: false, error: 'not stubbed' }),
    closeHostTerminal: vi.fn().mockResolvedValue(undefined),
    codexHook: vi.fn().mockResolvedValue({ ok: true, launched: { status: 'absent' } }),
    setCodexHookAuto: vi.fn().mockResolvedValue({ ok: false, error: 'not stubbed' }),
    installCodexHook: vi.fn().mockResolvedValue({ ok: false, error: 'not stubbed' }),
    ...overrides,
  }
}

function renderDashboard(state: TasksState = tasksState(), onOpenTask = vi.fn()) {
  render(
    <TaskDashboard tasksState={state} workspaces={[]} onOpenTask={onOpenTask} onShowWorkspaces={vi.fn()} now={() => NOW} />,
  )
  return { onOpenTask }
}

const card = () => screen.getByTestId(`task-card-${fixtureTask.id}`)
const detail = () => screen.getByRole('complementary', { name: 'Task details' })

describe('TaskDashboard with the fixture agent', () => {
  //efficacy:exempt pins that the shipped dashboard already serves a third agent from its declarations; this branch changes no dashboard code
  it('shows its task as one of a known agent, with its pane and no Resume', () => {
    const { onOpenTask } = renderDashboard()

    expect(within(screen.getByRole('region', { name: 'Waiting for input' })).getByTestId(`task-card-${fixtureTask.id}`)).toBeTruthy()
    expect(card()).toHaveTextContent(fixtureAgent)
    expect(card()).not.toHaveTextContent('(unknown agent)')
    expect(card()).toHaveTextContent('approve the plan')
    expect(within(card()).queryByRole('button', { name: /^Resume/ })).toBeNull()

    fireEvent.click(within(card()).getByRole('button', { name: 'Open: project' }))
    expect(onOpenTask).toHaveBeenCalledWith(expect.objectContaining({ id: fixtureTask.id }), { kind: 'open' })

    fireEvent.click(card())
    expect(within(detail()).getByRole('heading', { level: 2 })).toHaveTextContent('project')
    expect(within(detail()).queryByRole('button', { name: /^Resume/ })).toBeNull()
    expect(detail()).not.toHaveTextContent('Resume is not available')
    expect(detail()).not.toHaveTextContent('does not know the agent')
    expect(within(detail()).getAllByRole('button', { name: 'Mark done' }).length).toBeGreaterThan(0)
  })

  //efficacy:exempt pins that the shipped dashboard already shows any agent's summary; this branch changes no dashboard code
  it('shows its summary and what comes next', () => {
    renderDashboard()

    expect(within(card()).getByTestId('task-next')).toHaveTextContent('Next: Update the docs · 2 left')
    fireEvent.click(card())
    const work = within(detail()).getByRole('region', { name: 'Work' })
    expect(work).toHaveTextContent('Adding task summaries to the dashboard.')
    expect(work).toHaveTextContent('Update the docs')
    expect(work).toHaveTextContent('Run make check')
    expect(detail()).not.toHaveTextContent('does not summarize its tasks')
    // The summary is current, so there is nothing to ask for again.
    expect(within(detail()).queryByRole('button', { name: /^Summarize/ })).toBeNull()
  })

  //efficacy:exempt pins that the shipped dashboard already asks any agent's summary again; this branch changes no dashboard code
  it('summarizes it again on request once its summary is outdated', async () => {
    const requestSummary = vi.fn().mockResolvedValue(null)
    const outdated = { ...fixtureTask, summary: { ...fixtureTask.summary!, outdated: true } }
    renderDashboard(tasksState({ data: { ...response, tasks: [outdated] }, requestSummary }))

    fireEvent.click(card())
    await act(async () => {
      fireEvent.click(within(detail()).getByRole('button', { name: 'Summarize again' }))
    })
    expect(requestSummary).toHaveBeenCalledWith(expect.objectContaining({ id: fixtureTask.id }))
  })

  //efficacy:exempt pins that the shipped New task dialog already offers an agent declaring launch; this branch changes no dashboard code
  it('offers the fixture agent in the New task dialog and starts it', async () => {
    const launch = vi.fn().mockResolvedValue({
      ok: true,
      launched: { id: 'local:panemux-fixture-agent:0f0e0d0c-0b0a-4908-8706-050403020100', session_id: '0f0e0d0c-0b0a-4908-8706-050403020100', tmux_session: 'task-0f0e0d0c' },
    })
    renderDashboard(tasksState({ launch }))
    fireEvent.click(screen.getByRole('button', { name: 'New task' }))
    const dialog = within(screen.getByRole('dialog', { name: 'New task' }))

    const options = Array.from((dialog.getByLabelText('Agent') as HTMLSelectElement).options).map((o) => o.value)
    expect(options).toEqual(['claude', 'codex', fixtureAgent])
    fireEvent.change(dialog.getByLabelText('Agent'), { target: { value: fixtureAgent } })
    expect(dialog.queryByText(/cannot be started on/)).toBeNull()
    fireEvent.change(dialog.getByLabelText('Working directory'), { target: { value: '/workspace/user/project' } })
    fireEvent.change(dialog.getByLabelText('First instruction'), { target: { value: 'go' } })
    await act(async () => {
      fireEvent.click(dialog.getByRole('button', { name: 'Start' }))
    })
    expect(launch).toHaveBeenCalledWith({ host: '', agent: fixtureAgent, cwd: '/workspace/user/project', prompt: 'go', labels: [] })
  })
})
