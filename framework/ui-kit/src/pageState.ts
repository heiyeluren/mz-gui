export type PageSnapshot = {
  windowID: string
  routeID: string
  revision: number
  payload: string
}

export type PageStateStore = {
  loadPage(windowID: string, routeID: string): PageSnapshot | null
  savePage(snapshot: PageSnapshot): void
  restorePages(fromWindowID: string, toWindowID: string): void
}

export type PageStateOptions = {
  store: PageStateStore
  windowID: string
  routeID: string
  initialPayload?: string
}

export function restoreWindowPages(store: PageStateStore, fromWindowID: string, toWindowID: string) {
  store.restorePages(fromWindowID, toWindowID)
}

export function usePageState({ store, windowID, routeID, initialPayload = '' }: PageStateOptions) {
  const loaded = store.loadPage(windowID, routeID)
  const snapshot: PageSnapshot = loaded ?? {
    windowID,
    routeID,
    revision: 0,
    payload: initialPayload,
  }

  const update = (payload: string) => {
    store.savePage({
      windowID,
      routeID,
      revision: snapshot.revision + 1,
      payload,
    })
  }

  return {
    windowID: snapshot.windowID,
    routeID: snapshot.routeID,
    revision: snapshot.revision,
    payload: snapshot.payload,
    update,
  }
}
