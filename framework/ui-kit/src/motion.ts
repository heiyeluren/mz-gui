export type MotionPreference = 'full' | 'reduced'

export type MotionSignals = {
  prefersReducedMotion?: boolean
  resourcePressure?: boolean
  remoteDesktop?: boolean
}

export function resolveMotionPreference(signals: MotionSignals = {}): MotionPreference {
  if (signals.prefersReducedMotion || signals.resourcePressure || signals.remoteDesktop) {
    return 'reduced'
  }
  return 'full'
}
