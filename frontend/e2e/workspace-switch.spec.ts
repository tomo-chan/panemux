import { expect, test, type Page, type WebSocketRoute } from '@playwright/test'

test('renders terminal output when switching to a previously hidden workspace', async ({ page }) => {
  await page.goto('/')

  await page.getByRole('tab', { name: 'Default' }).click()
  await expect(page.getByRole('tab', { name: 'Default' })).toHaveAttribute('aria-selected', 'true')
  await expect.poll(async () => await visibleTerminalText(page), {
    message: 'default workspace terminal should render an initial prompt',
  }).toMatch(/\S/)

  await page.getByRole('tab', { name: 'Workspace 2' }).click()

  await expect(page.getByRole('tab', { name: 'Workspace 2' })).toHaveAttribute('aria-selected', 'true')
  await expect.poll(async () => await visibleTerminalText(page), {
    message: 'workspace switch should not leave the terminal blank',
  }).toMatch(/\S/)
})

// The task event stream is replaced by frames this test sends (issue #279):
// the receiver is what is under test, and a real agent waiting on its
// files is the server's tests' business.
test('flags an inactive workspace and notifies a task wait from the event stream', async ({ page }) => {
  await page.addInitScript(() => {
    type Recorded = { title: string; body: string; tag: string; click: () => void }
    const recorded: Recorded[] = []
    ;(window as Window & { __panemuxNotifications?: Recorded[] }).__panemuxNotifications = recorded

    class MockNotification {
      static permission = 'granted'
      static requestPermission = () => Promise.resolve('granted' as NotificationPermission)
      onclick: (() => void) | null = null

      constructor(title: string, options?: NotificationOptions) {
        recorded.push({ title, body: options?.body ?? '', tag: options?.tag ?? '', click: () => this.onclick?.() })
      }

      close() {}
    }

    Object.defineProperty(window, 'Notification', {
      configurable: true,
      writable: true,
      value: MockNotification,
    })
  })

  let seq = 10
  let stream: WebSocketRoute | null = null
  const task = (state: string, wait: Record<string, string> = {}) => ({
    id: 'local:claude:e2e-1',
    host: '',
    agent: 'claude',
    session_id: 'e2e-1',
    cwd: '/workspace/user/project',
    state,
    location: { kind: 'outside', pane_id: 'workspace-2-main', attachable: false },
    ...wait,
  })
  const sendChange = (state: string, prevState: string, wait: Record<string, string> = {}) => {
    seq += 1
    stream?.send(JSON.stringify({ type: 'task', epoch: 'e2e', seq, op: 'changed', prev_state: prevState, task: task(state, wait) }))
  }
  await page.routeWebSocket(/\/ws\/tasks\/events$/, (ws) => {
    stream = ws
    ws.send(JSON.stringify({ type: 'snapshot', epoch: 'e2e', seq, hosts: [{ name: '', status: 'ok' }], tasks: [task('busy')] }))
  })
  const notifications = () =>
    page.evaluate(() =>
      ((window as Window & { __panemuxNotifications?: Array<{ title: string; body: string; tag: string }> }).__panemuxNotifications ?? [])
        .map(({ title, body, tag }) => ({ title, body, tag })),
    )

  await page.goto('/')
  await page.getByRole('tab', { name: 'Default' }).click()
  await expect(page.getByRole('tab', { name: 'Default' })).toHaveAttribute('aria-selected', 'true')
  await expect.poll(() => stream !== null).toBe(true)

  sendChange('wait', 'busy', { waiting_for: 'input needed', wait_id: 'w1-e2e-first' })
  const workspace2 = page.getByRole('tab', { name: 'Workspace 2' })
  await expect(workspace2).toHaveAttribute('data-attention', 'true')
  await expect.poll(notifications).toEqual([
    { title: 'Agent waiting', body: 'claude on Local: project', tag: 'w1-e2e-first' },
  ])

  // Answered somewhere else: the flag goes without anyone selecting it.
  sendChange('busy', 'wait')
  await expect(workspace2).not.toHaveAttribute('data-attention')

  // A later wait is notified again, and its notification leads to the pane.
  sendChange('wait', 'busy', { waiting_for: 'input needed', wait_id: 'w1-e2e-second' })
  await expect.poll(async () => (await notifications()).length).toBe(2)
  await page.evaluate(() => {
    const recorded = (window as Window & { __panemuxNotifications?: Array<{ click: () => void }> }).__panemuxNotifications ?? []
    recorded[recorded.length - 1].click()
  })
  await expect(workspace2).toHaveAttribute('aria-selected', 'true')
  await expect(workspace2).not.toHaveAttribute('data-attention')
})

async function visibleTerminalText(page: Page) {
  const text = await page.locator('.xterm-rows').first().textContent()
  return text ?? ''
}
