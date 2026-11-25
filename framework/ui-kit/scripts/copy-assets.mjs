import { copyFile, mkdir } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'

const root = resolve(import.meta.dirname, '..')
for (const name of ['styles.css', 'PopupSelect.css', 'ResizeHandle.css']) {
  const destination = resolve(root, 'dist', name)
  await mkdir(dirname(destination), { recursive: true })
  await copyFile(resolve(root, 'src', name), destination)
}
