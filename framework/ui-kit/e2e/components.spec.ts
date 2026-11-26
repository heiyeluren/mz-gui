import { expect, test } from '@playwright/test'

test('opens a dialog, preserves input and closes with Escape', async ({ page }) => {
  await page.goto('/e2e/index.html')
  await page.getByRole('button', { name: '打开对话框' }).click()
  await expect(page.getByRole('dialog', { name: '编辑名称' })).toBeVisible()
  await page.getByRole('textbox', { name: '名称' }).fill('MZ-GUI')
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog', { name: '编辑名称' })).toHaveCount(0)
  await page.getByRole('button', { name: '打开对话框' }).click()
  await expect(page.getByRole('textbox', { name: '名称' })).toHaveValue('MZ-GUI')
})

test('supports keyboard tabs, markdown safety and virtualized windows', async ({ page }) => {
  await page.goto('/e2e/index.html')
  await page.getByRole('tab', { name: '列表' }).focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('tab', { name: '网格' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('link', { name: '安全' })).toHaveAttribute('href', 'https://example.com/a')
  await expect(page.getByRole('link', { name: '危险' })).toHaveCount(0)
  await expect(page.getByText('危险')).toBeVisible()
  const options = await page.getByRole('option').count()
  expect(options).toBeLessThan(100)
})

test('honors reduced motion and 200 percent zoom without horizontal overflow', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/e2e/index.html')
  const motion = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--mzgui-motion-fast').trim())
  expect(motion).toBe('0ms')
  await page.evaluate(() => { document.body.style.zoom = '200%' })
  const sizes = await page.evaluate(() => ({ content: document.documentElement.scrollWidth, viewport: window.innerWidth }))
  expect(sizes.content).toBeLessThanOrEqual(sizes.viewport)
})
