/** W116-G675：对照旧版 Theme overlay 再对照。旧版无独立 Theme overlay 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type ThemeMode = 'light' | 'dark' | 'system' | 'high-contrast'
export type ResolvedTheme = 'light' | 'dark' | 'high-contrast'

export type ThemeTokens = {
  canvas: string
  surface: string
  surfaceMuted: string
  text: string
  textMuted: string
  border: string
  accent: string
  success: string
  warning: string
  danger: string
}

export type ThemeSnapshot = {
  mode: ThemeMode
  resolved: ResolvedTheme
  revision: number
  tokens: ThemeTokens
}

const palettes: Record<ResolvedTheme, ThemeTokens> = {
  light: {
    canvas: '#f5f6f8',
    surface: '#ffffff',
    surfaceMuted: '#eef1f5',
    text: '#1f2933',
    textMuted: '#667085',
    border: '#d9dee7',
    accent: '#5b5bd6',
    success: '#14804a',
    warning: '#b54708',
    danger: '#b42318',
  },
  dark: {
    canvas: '#17191d',
    surface: '#22252a',
    surfaceMuted: '#2b2f36',
    text: '#f2f4f7',
    textMuted: '#aab2c0',
    border: '#3b414b',
    accent: '#8b8bf4',
    success: '#32d583',
    warning: '#f79009',
    danger: '#f97066',
  },
  'high-contrast': {
    canvas: '#000000',
    surface: '#000000',
    surfaceMuted: '#111111',
    text: '#ffffff',
    textMuted: '#ffffff',
    border: '#ffffff',
    accent: '#ffff00',
    success: '#00ff00',
    warning: '#ffff00',
    danger: '#ff0000',
  },
}

export const themeVariableMap: Record<keyof ThemeTokens, string> = {
  canvas: '--mzgui-color-canvas',
  surface: '--mzgui-color-surface',
  surfaceMuted: '--mzgui-color-surface-muted',
  text: '--mzgui-color-text',
  textMuted: '--mzgui-color-text-muted',
  border: '--mzgui-color-border',
  accent: '--mzgui-color-accent',
  success: '--mzgui-color-success',
  warning: '--mzgui-color-warning',
  danger: '--mzgui-color-danger',
}

export function resolveTheme(mode: ThemeMode, prefersDark: boolean): ResolvedTheme {
  switch (mode) {
    case 'dark':
      return 'dark'
    case 'high-contrast':
      return 'high-contrast'
    case 'system':
      return prefersDark ? 'dark' : 'light'
    default:
      return 'light'
  }
}

export function createThemeSnapshot(mode: ThemeMode, revision: number, prefersDark = false): ThemeSnapshot {
  const resolved = resolveTheme(mode, prefersDark)
  return {
    mode,
    resolved,
    revision,
    tokens: { ...palettes[resolved] },
  }
}

export function validateBrandTokens(tokens: ThemeTokens): Error | null {
  if (contrastRatio(tokens.text, tokens.canvas) < 4.5 || contrastRatio(tokens.text, tokens.surface) < 4.5) {
    return new Error('brand tokens fail contrast')
  }
  return null
}

export function applyThemeSnapshot(target: HTMLElement, snapshot: ThemeSnapshot): void {
  target.dataset.mzguiTheme = snapshot.resolved
  for (const [token, cssVar] of Object.entries(themeVariableMap) as Array<[keyof ThemeTokens, string]>) {
    target.style.setProperty(cssVar, snapshot.tokens[token])
  }
}

export function contrastRatio(first: string, second: string): number {
  const left = relativeLuminance(first)
  const right = relativeLuminance(second)
  const lighter = Math.max(left, right)
  const darker = Math.min(left, right)
  return (lighter + 0.05) / (darker + 0.05)
}

function relativeLuminance(hex: string): number {
  const value = hex.replace('#', '')
  const raw = value.length === 3
    ? value.split('').map((part) => part + part).join('')
    : value
  const channels = [0, 2, 4].map((offset) => {
    const channel = parseInt(raw.slice(offset, offset + 2), 16) / 255
    return channel <= 0.03928 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2]
}
