import { voiceOverName } from './voiceover'

const rtlLanguages = new Set(['ar', 'he', 'fa', 'ur'])

export type TextDirection = 'ltr' | 'rtl'

export function textDirection(locale: string): TextDirection {
  const lang = locale.trim().toLowerCase().split(/[-_]/)[0]
  return rtlLanguages.has(lang) ? 'rtl' : 'ltr'
}

export function speakTextDirection(direction: TextDirection | string): string {
  if (direction === 'rtl') {
    return '从右到左'
  }
  if (direction === 'ltr') {
    return '从左到右'
  }
  return voiceOverName(direction)
}

export function speakTheme(resolved: string): string {
  const labels: Record<string, string> = {
    light: '浅色',
    dark: '深色',
    'high-contrast': '高对比度',
  }
  const spoken = labels[resolved.trim()] ?? resolved
  return voiceOverName(spoken)
}
