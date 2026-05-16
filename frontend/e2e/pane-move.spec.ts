import { expect, test, type Locator, type Page } from '@playwright/test'

test('starts pane move mode from the header handle and moves a pane to the workspace edge', async ({ page }) => {
  await page.goto('/')

  await expect(page.locator('[data-pane-id]').first()).toBeVisible()
  const initialPaneCount = await page.locator('[data-pane-id]').count()
  await page.getByTitle('Split horizontal').first().click()
  await expect(page.locator('[data-pane-id]')).toHaveCount(initialPaneCount + 1)

  const dragHandle = page.getByTitle('Drag to move pane').first()
  await expect(dragHandle).toHaveCSS('cursor', 'grab')
  const topDropZone = page.locator('[data-workspace-drop-edge="top"]')

  await dragHandleToTarget(page, dragHandle, topDropZone)

  await expect(topDropZone).toBeHidden()
  await expect.poll(async () => page.locator('[data-pane-id]').evaluateAll((nodes) =>
    nodes.map((node) => node.getAttribute('data-pane-id')),
  )).toHaveLength(initialPaneCount + 1)

  const paneIds = await page.locator('[data-pane-id]').evaluateAll((nodes) =>
    nodes.map((node) => node.getAttribute('data-pane-id')),
  )
  expect(new Set(paneIds).size).toBe(initialPaneCount + 1)
})

test('starts pane move mode from the header handle and moves a pane beside another pane', async ({ page }) => {
  await page.goto('/')

  await expect(page.locator('[data-pane-id]').first()).toBeVisible()
  const initialPaneCount = await page.locator('[data-pane-id]').count()
  await page.getByTitle('Split horizontal').first().click()
  await expect(page.locator('[data-pane-id]')).toHaveCount(initialPaneCount + 1)

  const dragHandle = page.getByTitle('Drag to move pane').first()
  const targetPane = page.locator('[data-pane-id]').nth(1)

  await dragHandleToPaneHalf(page, dragHandle, targetPane, 'left')

  await expect(page.locator('[data-pane-drop-preview="left"]')).toBeHidden()
  await expect(page.locator('[data-pane-id]')).toHaveCount(initialPaneCount + 1)
})

async function dragHandleToTarget(page: Page, handle: Locator, target: Locator) {
  const handleBox = await handle.boundingBox()
  if (!handleBox) throw new Error('drag handle did not have a bounding box')

  await page.mouse.move(handleBox.x + handleBox.width / 2, handleBox.y + handleBox.height / 2)
  await page.mouse.down()
  await page.mouse.move(handleBox.x + handleBox.width / 2 + 24, handleBox.y + handleBox.height / 2 + 6, {
    steps: 8,
  })

  await expect(target).toBeVisible()

  const targetBox = await target.boundingBox()
  if (!targetBox) throw new Error('drop target did not have a bounding box')

  await page.mouse.move(targetBox.x + targetBox.width / 2, targetBox.y + targetBox.height / 2, {
    steps: 12,
  })
  await page.mouse.up()
}

async function dragHandleToPaneHalf(page: Page, handle: Locator, targetPane: Locator, edge: 'left' | 'right' | 'top' | 'bottom') {
  const handleBox = await handle.boundingBox()
  if (!handleBox) throw new Error('drag handle did not have a bounding box')

  await page.mouse.move(handleBox.x + handleBox.width / 2, handleBox.y + handleBox.height / 2)
  await page.mouse.down()
  await page.mouse.move(handleBox.x + handleBox.width / 2 + 24, handleBox.y + handleBox.height / 2 + 6, {
    steps: 8,
  })

  const targetBox = await targetPane.boundingBox()
  if (!targetBox) throw new Error('target pane did not have a bounding box')

  const point = paneHalfPoint(targetBox, edge)
  await page.mouse.move(point.x, point.y, { steps: 12 })
  await expect(page.locator(`[data-pane-drop-preview="${edge}"]`)).toBeVisible()
  await page.mouse.up()
}

function paneHalfPoint(box: NonNullable<Awaited<ReturnType<Locator['boundingBox']>>>, edge: 'left' | 'right' | 'top' | 'bottom') {
  switch (edge) {
    case 'left':
      return { x: box.x + box.width * 0.2, y: box.y + box.height * 0.5 }
    case 'right':
      return { x: box.x + box.width * 0.8, y: box.y + box.height * 0.5 }
    case 'top':
      return { x: box.x + box.width * 0.5, y: box.y + box.height * 0.2 }
    case 'bottom':
      return { x: box.x + box.width * 0.5, y: box.y + box.height * 0.8 }
  }
}
