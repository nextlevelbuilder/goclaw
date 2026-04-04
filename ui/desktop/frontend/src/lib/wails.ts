// Type-safe wrappers for Wails Go runtime bindings
// Wails injects window.go at startup with bound Go methods

export interface UpdateInfo {
  available: boolean
  version: string
  download_url: string
  release_url: string
  release_notes: string
}

declare global {
  interface Window {
    go: {
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
  }
}

export const wails = {
  getGatewayURL: (): Promise<string> => window.go.main.App.GetGatewayURL(),
  getGatewayToken: (): Promise<string> => window.go.main.App.GetGatewayToken(),
  getGatewayPort: (): Promise<number> => window.go.main.App.GetGatewayPort(),
  isGatewayReady: (): Promise<boolean> => window.go.main.App.IsGatewayReady(),
  getVersion: (): Promise<string> => window.go.main.App.GetVersion(),
  checkForUpdate: (): Promise<UpdateInfo> => window.go.main.App.CheckForUpdate(),
  applyUpdate: (): Promise<void> => window.go.main.App.ApplyUpdate(),
  restartApp: (): Promise<void> => window.go.main.App.RestartApp(),
  getDataDir: (): Promise<string> => window.go.main.App.GetDataDir(),
  resetDatabase: (): Promise<void> => window.go.main.App.ResetDatabase(),
  openFile: (path: string): Promise<void> => window.go.main.App.OpenFile(path),
  saveFile: (srcPath: string): Promise<void> => window.go.main.App.SaveFile(srcPath),
  downloadURL: (url: string, filename: string): Promise<void> => window.go.main.App.DownloadURL(url, filename),
}
