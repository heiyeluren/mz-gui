import { expect, test } from '@playwright/test'

test('terminal supports input, IME, clipboard and resize', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.addInitScript(() => { Object.defineProperty(window, 'WebGL2RenderingContext', { value: undefined }) })
  await page.goto('/e2e/index.html')
  const terminal = page.getByRole('application', { name: '交互终端' })
  await expect(terminal).toBeVisible()
  const textarea = page.locator('.xterm-helper-textarea')
  await textarea.focus()
  await page.keyboard.type('pwd')
  await page.keyboard.press('Enter')
  await expect(page.getByTestId('terminal-last-input')).toHaveText('pwd\r')

  const cdp = await context.newCDPSession(page)
  await cdp.send('Input.imeSetComposition', { text: 'zhong', selectionStart: 5, selectionEnd: 5, replacementStart: 0, replacementEnd: 0 })
  await cdp.send('Input.imeSetComposition', { text: '中文', selectionStart: 2, selectionEnd: 2, replacementStart: 0, replacementEnd: 0 })
  await cdp.send('Input.insertText', { text: '中文' })
  await expect(page.getByTestId('terminal-last-input')).toHaveText('中文')

  await page.evaluate(() => navigator.clipboard.writeText('粘贴文本'))
  await page.keyboard.press(process.platform === 'win32' ? 'Shift+Insert' : 'ControlOrMeta+v')
  await expect(page.getByTestId('terminal-last-input')).toHaveText('粘贴文本')

  await expect(page.locator('.xterm-screen')).toContainText('ready')
  const copied = await page.evaluate(() => {
    const rows = document.querySelector<HTMLElement>('.xterm-rows')!
    rows.style.userSelect = 'text'
    const selection = window.getSelection()!
    const range = document.createRange()
    range.selectNodeContents(rows)
    selection.removeAllRanges()
    selection.addRange(range)
    return document.execCommand('copy')
  })
  expect(copied).toBe(true)
  expect(await page.evaluate(() => navigator.clipboard.readText())).toContain('ready')

  await page.setViewportSize({ width: 900, height: 640 })
  await expect(page.getByTestId('terminal-size')).not.toHaveText('0x0')
  const initialSize = await page.getByTestId('terminal-size').textContent()
  await page.setViewportSize({ width: 760, height: 520 })
  await expect.poll(() => page.getByTestId('terminal-size').textContent()).not.toBe(initialSize)
})

test('terminal renders high frequency output and recovers after a read failure', async ({ page }) => {
  await page.addInitScript(() => { Object.defineProperty(window, 'WebGL2RenderingContext', { value: undefined }) })
  await page.goto('/e2e/index.html')
  await page.getByRole('button', { name: '模拟高频输出' }).click()
  await expect(page.locator('.xterm-screen')).toContainText('line-0499', { timeout: 20_000 })

  await page.getByRole('button', { name: '模拟断线' }).click()
  await expect(page.getByText('读取终端输出失败')).toBeVisible()
  await expect(page.locator('.xterm-screen')).toContainText('recovered', { timeout: 8_000 })
})

test('data components support sorting, tree selection and large list scroll restoration', async ({ page }) => {
  await page.goto('/e2e/index.html')
  const table = page.getByRole('table', { name: '可排序文件' })
  await expect(table.locator('tbody tr').first()).toContainText('alpha')
  await table.getByRole('button', { name: '名称，升序' }).click()
  await expect(table.locator('tbody tr').first()).toContainText('zeta')
  await table.locator('tbody tr').first().focus()
  await page.keyboard.press('End')
  await expect(table.locator('tbody tr').last()).toHaveAttribute('aria-selected', 'true')

  const tree = page.getByRole('tree', { name: '模块树' })
  await tree.focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('treeitem', { name: '子模块' })).toBeVisible()
  await page.keyboard.press('ArrowDown')
  await expect(page.getByRole('treeitem', { name: '子模块' })).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('ArrowLeft')
  await page.keyboard.press('ArrowLeft')
  await expect(page.getByRole('treeitem', { name: '根模块' })).toHaveAttribute('aria-expanded', 'false')

  const list = page.getByRole('listbox', { name: '五千项列表' })
  await expect(page.getByRole('option').count()).resolves.toBeLessThan(50)
  await list.evaluate((element) => { element.scrollTop = element.scrollHeight; element.dispatchEvent(new Event('scroll', { bubbles: true })) })
  await expect(page.getByRole('option', { name: '项目 4999' })).toBeVisible()
  await page.getByRole('button', { name: '保存滚动位置' }).click()
  await list.evaluate((element) => { element.scrollTop = 0; element.dispatchEvent(new Event('scroll', { bubbles: true })) })
  await expect(page.getByRole('option', { name: '项目 0' })).toBeVisible()
  await page.getByRole('button', { name: '恢复滚动位置' }).click()
  await expect(page.getByRole('option', { name: '项目 4999' })).toBeVisible()
  await list.focus()
  await page.keyboard.press('Home')
  await expect(list).toHaveAttribute('aria-valuetext', '项目 0')
  await page.keyboard.press('End')
  await expect(list).toHaveAttribute('aria-valuetext', '项目 4999')
})
