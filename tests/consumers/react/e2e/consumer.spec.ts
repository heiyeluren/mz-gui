import { expect, test } from '@playwright/test'

test('renders and interacts with packaged UI Kit components', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('navigation', { name: '消费者导航' })).toBeVisible()
  await expect(page.getByRole('table', { name: '消费者文件' })).toContainText('consumer.go')
  await page.getByRole('option', { name: '第二项' }).click()
  await expect(page.getByRole('listbox', { name: '消费者列表' })).toHaveAttribute('aria-valuetext', '第二项')
  await page.getByRole('button', { name: '打开消费者对话框' }).click()
  await expect(page.getByRole('dialog', { name: '独立消费者' })).toContainText('npm tarball 渲染成功')
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toHaveCount(0)
})
