// Type-safe wrappers for Wails Go runtime bindings
// Wails injects window.go (bound Go methods) and window.runtime (window/event
// helpers) at startup. Neither exists in tests or a plain browser, so the
// runtime helpers below are guarded and degrade to no-ops.

export interface UpdateInfo {
  available: boolean
  version: string
  download_url: string
  release_url: string
  release_notes: string
}

/** The subset of the injected Wails runtime the frontend uses. */
export interface WailsRuntime {
  WindowMinimise(): void
  WindowToggleMaximise(): void
  WindowIsMaximised(): Promise<boolean>
  Quit(): void
  BrowserOpenURL(url: string): void
  EventsOnMultiple(eventName: string, callback: (...data: never[]) => void, maxCallbacks: number): () => void
}

declare global {
  interface Window {
    go?: {
      main: {
        App: {
          GetGatewayURL(): Promise<string>
          GetGatewayToken(): Promise<string>
          GetGatewayPort(): Promise<number>
          IsGatewayReady(): Promise<boolean>
          GetVersion(): Promise<string>
          CheckForUpdate(): Promise<UpdateInfo>
          ApplyUpdate(): Promise<void>
          RestartApp(): Promise<void>
          GetDataDir(): Promise<string>
          ResetDatabase(): Promise<void>
          OpenFile(path: string): Promise<void>
          SaveFile(srcPath: string): Promise<void>
          DownloadURL(url: string, filename: string): Promise<void>
        }
      }
    }
    runtime?: Partial<WailsRuntime>
  }
}

/** Bound Go methods. Throws (as a rejected promise) when not running inside Wails. */
function bridge(): NonNullable<Window['go']>['main']['App'] {
  const app = typeof window === 'undefined' ? undefined : window.go?.main?.App
  if (!app) throw new Error('Wails bindings are not available')
  return app
}

// Every wrapper is async so a missing bridge surfaces as a rejection callers can .catch().
export const wails = {
  getGatewayURL: async (): Promise<string> => bridge().GetGatewayURL(),
  getGatewayToken: async (): Promise<string> => bridge().GetGatewayToken(),
  getGatewayPort: async (): Promise<number> => bridge().GetGatewayPort(),
  isGatewayReady: async (): Promise<boolean> => bridge().IsGatewayReady(),
  getVersion: async (): Promise<string> => bridge().GetVersion(),
  checkForUpdate: async (): Promise<UpdateInfo> => bridge().CheckForUpdate(),
  applyUpdate: async (): Promise<void> => bridge().ApplyUpdate(),
  restartApp: async (): Promise<void> => bridge().RestartApp(),
  getDataDir: async (): Promise<string> => bridge().GetDataDir(),
  resetDatabase: async (): Promise<void> => bridge().ResetDatabase(),
  openFile: async (path: string): Promise<void> => bridge().OpenFile(path),
  saveFile: async (srcPath: string): Promise<void> => bridge().SaveFile(srcPath),
  downloadURL: async (url: string, filename: string): Promise<void> => bridge().DownloadURL(url, filename),
}

function rt(): Partial<WailsRuntime> | undefined {
  return typeof window === 'undefined' ? undefined : window.runtime
}

/** Whether the Wails runtime is present (false in tests and a plain browser). */
export function hasWailsRuntime(): boolean {
  return typeof rt()?.WindowMinimise === 'function'
}

/** Window and event helpers backed by the injected runtime; safe no-ops without it. */
export const wailsRuntime = {
  minimise: (): void => { rt()?.WindowMinimise?.() },
  toggleMaximise: (): void => { rt()?.WindowToggleMaximise?.() },
  isMaximised: async (): Promise<boolean> => {
    const fn = rt()?.WindowIsMaximised
    if (!fn) return false
    try { return await fn() } catch { return false }
  },
  quit: (): void => { rt()?.Quit?.() },
  browserOpenURL: (url: string): void => {
    const fn = rt()?.BrowserOpenURL
    if (fn) fn(url)
    else window.open(url, '_blank', 'noopener,noreferrer')
  },
  /** Subscribe to a backend event. Returns an unsubscribe function. */
  eventsOn: <T>(eventName: string, callback: (data: T) => void): (() => void) => {
    const fn = rt()?.EventsOnMultiple
    if (!fn) return () => {}
    return fn(eventName, callback as (...data: never[]) => void, -1)
  },
}
