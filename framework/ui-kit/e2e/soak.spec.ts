import { expect, test } from '@playwright/test'

test('terminal output and virtual list remain bounded during soak', async ({ page }) => {
  const seconds = Number(process.env.MZGUI_SOAK_SECONDS ?? 2)
  test.setTimeout(Math.max(90_000, (seconds + 70) * 1000))
  await page.addInitScript(() => { Object.defineProperty(window, 'WebGL2RenderingContext', { value: undefined }) })
  await page.goto('/e2e/index.html')
  const list = page.getByRole('listbox', { name: '五千项列表' })
  const start = Date.now()
  let outputBatches = 0
  let scrollBatches = 0
  while (Date.now() - start < seconds * 1000) {
    await page.getByRole('button', { name: '模拟高频输出' }).click()
    outputBatches++
    await list.evaluate((element, bottom) => {
      element.scrollTop = bottom ? element.scrollHeight : 0
      element.dispatchEvent(new Event('scroll', { bubbles: true }))
    }, scrollBatches % 2 === 0)
    scrollBatches++
    await page.waitForTimeout(100)
  }
  await expect(page.getByTestId('terminal-cursor')).toHaveText(String(1 + outputBatches * 500), { timeout: 30_000 })
  await expect(page.locator('.xterm-screen')).toContainText(`batch-${outputBatches}-line-0499`, { timeout: 30_000 })
  await expect(page.getByRole('option').count()).resolves.toBeLessThan(50)
  console.log(JSON.stringify({ seconds, outputBatches, scrollBatches, renderedOptions: await page.getByRole('option').count(), bufferedFrames: await page.getByTestId('terminal-buffered').textContent() }))
})
