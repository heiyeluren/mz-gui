import { useState } from 'react'

/** W116-G677：对照旧版 WindowState 再对照。旧版无独立 WindowState UI 入口。诚实不画。不含产品语义。不 VoiceOver。不改 window RPC。 */
export type WindowSnapshot = {
  windowID: string
  routeID: string
  revision: number
  payload: string
}

export type WindowStateStore = {
  load(windowID: string): WindowSnapshot | null
  save(snapshot: WindowSnapshot): void
  remove?(windowID: string): void
}

export type WindowStateOptions = {
  store: WindowStateStore
  windowID: string
  routeID: string
  initialPayload?: string
}

export function useWindowState({ store, windowID, routeID, initialPayload = '' }: WindowStateOptions) {
  const [snapshot, setSnapshot] = useState<WindowSnapshot>(() => store.load(windowID) ?? {
    windowID,
    routeID,
    revision: 0,
    payload: initialPayload,
  })

  const update = (payload: string) => {
    const next: WindowSnapshot = {
      windowID,
      routeID,
      revision: snapshot.revision + 1,
      payload,
    }
    store.save(next)
    setSnapshot(next)
  }

  return {
    windowID: snapshot.windowID,
    routeID: snapshot.routeID,
    revision: snapshot.revision,
    payload: snapshot.payload,
    update,
  }
}

export type WebStorageWindowStateStoreOptions = {
  storage: Storage
  keyPrefix?: string
}

const defaultWebStorageWindowStateKeyPrefix = 'mzgui:window-state:'

function webStorageWindowStateKey(prefix: string, windowID: string): string {
  return `${prefix}${windowID}`
}

function isWindowSnapshot(value: unknown): value is WindowSnapshot {
  const snapshot = value as Partial<WindowSnapshot>
  return Boolean(snapshot)
    && typeof snapshot.windowID === 'string'
    && typeof snapshot.routeID === 'string'
    && typeof snapshot.revision === 'number'
    && Number.isFinite(snapshot.revision)
    && typeof snapshot.payload === 'string'
}

export function createWebStorageWindowStateStore({
  storage,
  keyPrefix = defaultWebStorageWindowStateKeyPrefix,
}: WebStorageWindowStateStoreOptions): WindowStateStore {
  return {
    load(windowID) {
      try {
        const parsed = JSON.parse(storage.getItem(webStorageWindowStateKey(keyPrefix, windowID)) || 'null')
        return isWindowSnapshot(parsed) && parsed.windowID === windowID ? parsed : null
      } catch {
        return null
      }
    },
    save(snapshot) {
      storage.setItem(webStorageWindowStateKey(keyPrefix, snapshot.windowID), JSON.stringify(snapshot))
    },
    remove(windowID) {
      storage.removeItem(webStorageWindowStateKey(keyPrefix, windowID))
    },
  }
}

export function resolveDurableWebStorage(): Storage | null {
  if (typeof window === 'undefined') return null
  try {
    if (window.localStorage) return window.localStorage
  } catch {
    // localStorage 可能被宿主禁用，继续尝试会话级降级。
  }
  try {
    return window.sessionStorage ?? null
  } catch {
    return null
  }
}
