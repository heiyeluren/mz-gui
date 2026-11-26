import { expect, test } from '@playwright/test'

test('media and mermaid render safely with a visual baseline', async ({ page }) => {
  await page.goto('/e2e/index.html')
  await expect(page.getByRole('img', { name: '简单流程图' })).toBeVisible()
  await expect(page.locator('[data-mermaid-staging]')).toHaveCount(0)
  await expect(page.locator('video')).toHaveAttribute('preload', 'none')
  await expect(page.getByRole('status', { name: '非法媒体' })).toBeVisible()
  await expect(page.getByLabel('媒体与图表')).toHaveScreenshot('media-mermaid.png')
})
