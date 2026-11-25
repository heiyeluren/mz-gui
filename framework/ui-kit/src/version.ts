// 与 framework/go/runtime/updater 的 SemVer 比较保持一致，无法解析时返回 null。
export function compareVersions(left?: string, right?: string): number | null {
  const parse = (value?: string) => {
    const match = /^(?:v)?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/.exec(value?.trim() ?? '')
    if (!match || match[4]?.split('.').some(part => /^0[0-9]+$/.test(part))) return null
    return match
  }
  const a = parse(left), b = parse(right)
  if (!a || !b) return null
  for (let index = 1; index <= 3; index++) {
    const x = BigInt(a[index]), y = BigInt(b[index])
    if (x !== y) return x > y ? 1 : -1
  }
  if (a[4] === b[4]) return 0
  if (!a[4] || !b[4]) return a[4] ? -1 : 1
  const x = a[4].split('.'), y = b[4].split('.')
  for (let index = 0; index < Math.max(x.length, y.length); index++) {
    if (x[index] === y[index]) continue
    if (x[index] === undefined || y[index] === undefined) return x[index] === undefined ? -1 : 1
    const xn = /^[0-9]+$/.test(x[index]), yn = /^[0-9]+$/.test(y[index])
    if (xn && yn) return BigInt(x[index]) > BigInt(y[index]) ? 1 : -1
    if (xn !== yn) return xn ? -1 : 1
    return x[index] > y[index] ? 1 : -1
  }
  return 0
}
