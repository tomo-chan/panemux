import { expect, test, type Page } from '@playwright/test'

// The core multiplexer — splitting, closing, resizing, restoring a layout, and
// workspace CRUD — had no scenario rows and no end-to-end coverage at all,
// while Agent Board and the command center had both. docs/scenarios.md's own
// rule is that a silently absent row is not a legitimate answer; these are the
// tests that let sections H and I of that ledger say `auto`.
//
// Every test works from the counts it observes rather than absolute ones, and
// cleans up after itself. The suite runs serially against one long-lived
// panemux process (playwright.config.ts sets fullyParallel: false), so a test
// that assumed "there are exactly two panes" would pass alone and fail in the
// suite — and would fail differently depending on which specs ran first.

const panes = (page: Page) => page.locator('[data-pane-id]')

async function paneIds(page: Page): Promise<string[]> {
  return panes(page).evaluateAll((nodes) => nodes.map((n) => n.getAttribute('data-pane-id') ?? ''))
}

test('splits a pane and closes it again', async ({ page }) => {
  await page.goto('/')
  await expect(panes(page).first()).toBeVisible()

  const before = await paneIds(page)

  await page.getByTitle('Split horizontal').first().click()
  await expect(panes(page)).toHaveCount(before.length + 1)

  const after = await paneIds(page)
  expect(new Set(after).size).toBe(before.length + 1)

  // The new pane is a real session, not just a box: it renders terminal
  // output of its own.
  await expect
    .poll(async () => (await page.locator('.xterm-rows').nth(1).textContent()) ?? '', {
      message: 'the pane created by splitting should render its own prompt',
    })
    .toMatch(/\S/)

  const created = after.find((id) => !before.includes(id))
  expect(created).toBeTruthy()

  await closePane(page, created as string)
  await expect(panes(page)).toHaveCount(before.length)
  expect(await paneIds(page)).toEqual(before)
})

test('splits vertically as well as horizontally', async ({ page }) => {
  await page.goto('/')
  await expect(panes(page).first()).toBeVisible()

  const before = await paneIds(page)

  await page.getByTitle('Split vertical').first().click()
  await expect(panes(page)).toHaveCount(before.length + 1)

  // A vertical split stacks the panes: the new one sits below, not beside.
  const boxes = await panes(page).evaluateAll((nodes) =>
    nodes.map((n) => {
      const r = n.getBoundingClientRect()
      return { top: r.top, left: r.left }
    }),
  )
  expect(boxes[1].top).toBeGreaterThan(boxes[0].top)

  const created = (await paneIds(page)).find((id) => !before.includes(id))
  await closePane(page, created as string)
  await expect(panes(page)).toHaveCount(before.length)
})

test('restores the layout after a reload', async ({ page }) => {
  await page.goto('/')
  await expect(panes(page).first()).toBeVisible()

  const before = await paneIds(page)

  // The split's own layout PUT has to have landed before the reload, or this
  // reads the config as it was a moment ago and looks exactly like "the layout
  // was not restored". It passed when run alone and failed in the full suite
  // before this wait was added — the sharpest kind of false positive, because
  // it points at the product rather than at the test.
  const saved = layoutSaved(page)
  await page.getByTitle('Split horizontal').first().click()
  await expect(panes(page)).toHaveCount(before.length + 1)
  const withSplit = await paneIds(page)
  expect((await saved).status()).toBe(200)

  // The reload is the assertion: a layout that lives only in React state
  // would come back as whatever the config file still said.
  await page.reload()
  await expect(panes(page)).toHaveCount(withSplit.length)
  expect(await paneIds(page)).toEqual(withSplit)

  const created = withSplit.find((id) => !before.includes(id))
  await closePane(page, created as string)
  await expect(panes(page)).toHaveCount(before.length)
})

test('resizes a split by dragging the divider, and the new sizes survive a reload', async ({ page }) => {
  await page.goto('/')
  await expect(panes(page).first()).toBeVisible()

  const before = await paneIds(page)

  // Wait out the split's OWN layout PUT before touching the divider. Without
  // this, the subscription below could resolve on the split's response instead
  // of the drag's — `page.waitForResponse` only sees responses that arrive
  // after it is called, so a split PUT still in flight is indistinguishable
  // from the one this test is actually waiting for.
  const splitSaved = layoutSaved(page)
  await page.getByTitle('Split horizontal').first().click()
  await expect(panes(page)).toHaveCount(before.length + 1)
  expect((await splitSaved).status()).toBe(200)

  const firstPane = panes(page).first()
  const widthBefore = (await firstPane.boundingBox())?.width ?? 0
  expect(widthBefore).toBeGreaterThan(0)

  const divider = page.locator('[data-divider-drop-zone="horizontal"]').first()
  const box = await divider.boundingBox()
  expect(box).toBeTruthy()

  await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2)
  await page.mouse.down()
  await page.mouse.move(box!.x + box!.width / 2 - 120, box!.y + box!.height / 2, { steps: 10 })
  await page.mouse.up()

  // The layout PUT is debounced by 500ms (useLayout's updateSizes), so the
  // reload below has to wait for it. Reloading on the drag alone reads the
  // config as it was before the drag and looks exactly like "resize is not
  // persisted" — which is what this test reported before the wait was added.
  // Subscribing here, after the last mouse event, is still comfortably ahead
  // of the debounce.
  const saved = layoutSaved(page)

  await expect
    .poll(async () => (await firstPane.boundingBox())?.width ?? 0, {
      message: 'dragging the divider left should narrow the pane on its left',
    })
    .toBeLessThan(widthBefore - 40)

  const widthAfter = (await firstPane.boundingBox())?.width ?? 0
  expect((await saved).status()).toBe(200)

  await page.reload()
  await expect(panes(page).first()).toBeVisible()

  // Poll the bound that DISCRIMINATES. A layout that was not restored comes
  // back at widthBefore, which is greater than widthAfter - 30 — so polling
  // the lower bound would return on its first sample whether the resize was
  // persisted or not, and the only assertion that could detect the failure
  // would be the un-retried one. Polling the upper bound also gives the first
  // paint room to settle before it is judged.
  await expect
    .poll(async () => (await panes(page).first().boundingBox())?.width ?? 0, {
      message: 'a resize is a layout change, so it must be persisted like any other',
    })
    .toBeLessThan(widthAfter + 30)
  expect((await panes(page).first().boundingBox())?.width ?? 0).toBeGreaterThan(widthAfter - 30)

  const created = (await paneIds(page)).find((id) => !before.includes(id))
  await closePane(page, created as string)
  await expect(panes(page)).toHaveCount(before.length)
})

test('adds, renames and deletes a workspace', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('tab').first()).toBeVisible()

  const tabsBefore = await page.getByRole('tab').count()

  await page.getByRole('button', { name: 'Add workspace' }).first().click()
  await expect(page.getByRole('tab')).toHaveCount(tabsBefore + 1)

  // The title comes from the API rather than the tab's own text: a tab
  // renders its pane summary inside itself, so its accessible name is
  // "Workspace 31 panes · 0 up · 1 pendingTerminal", not "Workspace 3".
  const addedTitle = await page.evaluate(async () => {
    const list = await (await fetch('/api/workspaces')).json()
    return list.items[list.items.length - 1].title as string
  })
  expect(addedTitle).not.toBe('')

  await page.getByRole('button', { name: `Rename ${addedTitle} workspace` }).click()
  const nameInput = page.getByRole('textbox', { name: 'Workspace name' })
  await expect(nameInput).toBeVisible()
  await nameInput.fill('Renamed By E2E')
  await nameInput.press('Enter')

  await expect(page.getByRole('tab', { name: 'Renamed By E2E' })).toBeVisible()

  // The rename is persisted, not just local state.
  await page.reload()
  await expect(page.getByRole('tab', { name: 'Renamed By E2E' })).toBeVisible()

  // Deleting a workspace is confirmed first — it takes its panes with it.
  // Playwright dismisses dialogs by default, so without this the delete is
  // silently cancelled and the test reads as "delete does not work".
  const confirmed = new Promise<string>((resolve) => {
    page.once('dialog', (dialog) => {
      resolve(dialog.message())
      void dialog.accept()
    })
  })

  await page.getByRole('button', { name: 'Delete Renamed By E2E workspace' }).click()
  expect(await confirmed).toContain('Renamed By E2E')
  await expect(page.getByRole('tab', { name: 'Renamed By E2E' })).toHaveCount(0)
  await expect(page.getByRole('tab')).toHaveCount(tabsBefore)
})

// A deliberate omission: "deleting the last workspace is refused" is NOT
// covered here. Driving it means deleting every workspace on the one panemux
// process the whole suite shares, which wrecks the fixture for every spec that
// runs after this one — workspace-switch.spec.ts asserts on tabs named
// "Default" and "Workspace 2" by name. The rule is covered where it can be
// driven in isolation, against the real router: internal/server's
// TestServer_APIIntegration case for DELETE /api/workspaces/{id}, and
// internal/api's own handler tests.

// Resolves when panemux has persisted a layout change. Every layout mutation
// goes through PUT /api/workspaces/{id}/layout (or /api/layout when there are
// no workspaces), so this is the one signal that separates "React state
// changed" from "the change would survive a restart".
function layoutSaved(page: Page) {
  return page.waitForResponse(
    (response) =>
      response.request().method() === 'PUT' && new URL(response.url()).pathname.endsWith('/layout'),
  )
}

// Closing a pane is two requests — DELETE the session, then PUT the layout —
// and the test has to wait for the second one. The suite runs serially against
// one shared panemux process, so a test that returned while its own cleanup
// was still in flight would leave the next test's `page.goto('/')` reading a
// layout that is about to change under it. That showed up as failures in
// unrelated specs, which is the worst way for it to show up.
async function closePane(page: Page, paneId: string) {
  const pane = page.locator(`[data-pane-id="${paneId}"]`)
  const saved = layoutSaved(page)
  await pane.getByTitle('Close pane').click()
  await expect(pane).toHaveCount(0)
  expect((await saved).status()).toBe(200)
  await expectPersistedPanes(page, async (ids) => !ids.includes(paneId))
}

// Polls what panemux would serve on a reload, not what React is rendering.
async function expectPersistedPanes(page: Page, predicate: (ids: string[]) => Promise<boolean>) {
  await expect
    .poll(async () => predicate(await persistedPaneIds(page)), {
      message: 'the persisted layout should have caught up with the UI',
    })
    .toBe(true)
}

async function persistedPaneIds(page: Page): Promise<string[]> {
  return page.evaluate(async () => {
    const layout = await (await fetch('/api/layout')).json()
    const ids: string[] = []
    const walk = (node: { children?: Array<{ pane?: { id: string }; children?: unknown }> }) => {
      for (const child of node.children ?? []) {
        if (child.pane) ids.push(child.pane.id)
        walk(child as { children?: Array<{ pane?: { id: string } }> })
      }
    }
    walk(layout)
    return ids
  })
}
