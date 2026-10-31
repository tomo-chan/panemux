import { useCallback, useEffect, useRef, useState } from 'react'
import type { Task, Workspace } from '../schemas'
import { useTasks } from './useTasks'
import { findTaskPane, taskOpenAction, type TaskOpenAction, type TaskPaneRef } from '../utils/taskBoard'
import { parseSessionLink, sameSession, stripSessionLink } from '../utils/sessionLink'
import { layoutContainsPane } from '../utils/layoutTree'

export function useSessionLink(
  dashboard: boolean,
  workspaces: Workspace[] | null,
  layoutError: string | null,
  open: (task: Task, action: TaskOpenAction, current: () => boolean) => Promise<TaskPaneRef | null>,
  stopped: (task: Task) => void,
) {
  const [request, setRequest] = useState(() => ({ parsed: parseSessionLink(window.location.search), seq: 0 }))
  const [pending, setPending] = useState(request.parsed.kind === 'valid')
  const [notice, setNotice] = useState<string | null>(request.parsed.kind === 'invalid' ? 'Invalid session link' : request.parsed.kind === 'valid' ? 'Opening session…' : null)
  const tasks = useTasks(dashboard || pending)
  const generation = useRef(0)
  const handled = useRef(-1)
  const baseline = useRef<typeof tasks.data>(null)
  const targetPane = useRef<string | null>(null)
  const dataRef = useRef(tasks.data)
  dataRef.current = tasks.data
  const refreshRef = useRef(tasks.refresh)
  refreshRef.current = tasks.refresh
  const mounted = useRef(true)

  const restart = useCallback(() => {
    const parsed = parseSessionLink(window.location.search)
    baseline.current = dataRef.current
    targetPane.current = null
    setRequest({ parsed, seq: ++generation.current })
    setPending(parsed.kind === 'valid')
    setNotice(parsed.kind === 'invalid' ? 'Invalid session link' : parsed.kind === 'valid' ? 'Opening session…' : null)
  }, [])
  useEffect(() => {
    const navigate = () => { restart(); if (parseSessionLink(window.location.search).kind === 'valid') void refreshRef.current() }
    window.addEventListener('popstate', navigate)
    return () => window.removeEventListener('popstate', navigate)
  }, [restart])
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])

  const clear = useCallback(() => {
    generation.current += 1
    window.history.replaceState(window.history.state, '', stripSessionLink(window.location.href))
    setRequest({ parsed: { kind: 'none' }, seq: generation.current })
    targetPane.current = null
    setPending(false)
    setNotice(null)
  }, [])

  useEffect(() => {
    if (!pending || request.parsed.kind !== 'valid' || handled.current === request.seq) return
    const fail = (message: string) => { handled.current = request.seq; setPending(false); setNotice(message) }
    if (layoutError) { fail(`Could not load layout: ${layoutError}`); return }
    if (tasks.error && !tasks.loading) { fail(`Could not collect sessions: ${tasks.error}`); return }
    if (!workspaces || !tasks.data || tasks.data === baseline.current || tasks.loading) return
    const key = request.parsed.key
    const host = tasks.data.hosts.find((item) => item.name === key.host)
    if (host?.status === 'connecting') return
    if (!host || host.status !== 'ok') { fail(host?.error || 'Session host is not available'); return }
    const matches = tasks.data.tasks.filter((task) => sameSession(task, key))
    if (matches.length !== 1) { fail(matches.length ? 'Ambiguous session key' : 'Session not found'); return }
    const task = matches[0]
    const action = taskOpenAction(task, findTaskPane(task, workspaces))
    handled.current = request.seq
    if (task.state === 'stop') {
      setPending(false)
      stopped(task)
      setNotice('Session is stopped. Resume manually from the task dashboard.')
    } else if (action.kind === 'unavailable') {
      setPending(false)
      setNotice(action.reason)
    } else {
      targetPane.current = action.kind === 'goto' ? action.pane.paneId : null
      const current = () => mounted.current && generation.current === request.seq
      void open(task, action, current).then((pane) => {
        if (!current()) return
        targetPane.current = pane?.paneId ?? null
        setPending(false)
        setNotice(null)
      }).catch((err: unknown) => {
        if (current()) { setPending(false); setNotice(`Could not open session: ${err instanceof Error ? err.message : String(err)}`) }
      })
    }
  }, [pending, request, tasks, workspaces, layoutError, open, stopped])

  const retry = () => { restart(); void tasks.refresh() }
  const selectPane = (paneId: string | null) => { if (request.parsed.kind !== 'none' && paneId !== targetPane.current) clear() }
  const selectWorkspace = (workspaceId: string) => {
    if (request.parsed.kind !== 'none' && !workspaces?.some((workspace) => workspace.id === workspaceId && targetPane.current && layoutContainsPane(workspace.layout, targetPane.current))) clear()
  }
  const selectTask = (task: Task) => { if (request.parsed.kind !== 'none' && (request.parsed.kind !== 'valid' || !sameSession(task, request.parsed.key))) clear() }
  return { tasks, pending, notice, retry, clear, selectPane, selectWorkspace, selectTask, dismiss: () => setNotice(null), closePane: (paneId: string) => { if (paneId === targetPane.current) clear() } }
}
