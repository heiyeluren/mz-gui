import { voiceOverStateLabel } from './voiceover'

/** W116-G676：对照旧版 Zoom 控件再对照。旧版无独立 Zoom 控件入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export const zoomClassName = 'mzgui-zoom'

export const zoomTargetPercent = 200

export function remFromPx(px: number, root = 16): string {
  const size = root > 0 ? px / root : 0
  return `${size}rem`
}

export function layoutSurvivesZoom(percent: number): boolean {
  return percent >= zoomTargetPercent
}

export function speakZoom(percent: number): string {
  if (!Number.isFinite(percent) || percent <= 0) {
    return voiceOverStateLabel('empty')
  }
  return `缩放百分之 ${Math.round(percent)}`
}
