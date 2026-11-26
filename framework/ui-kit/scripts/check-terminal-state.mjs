import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const root = resolve(import.meta.dirname, '..')
const canonical = resolve(root, '../go/runtime/terminal/xterm/assets')
for (const name of ['state.js', 'state.d.ts']) {
  const [expected, actual] = await Promise.all([
    readFile(resolve(canonical, name), 'utf8'),
    readFile(resolve(root, 'src', `terminalState${name.slice('state'.length)}`), 'utf8'),
  ])
  if (expected !== actual) {
    throw new Error(`${name} 与 UI Kit terminalState 副本不一致；请同步两侧实现`)
  }
}
