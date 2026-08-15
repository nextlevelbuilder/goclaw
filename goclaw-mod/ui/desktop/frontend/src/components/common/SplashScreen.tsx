/** Splash screen shown while gateway boots. */
export function SplashScreen({ ready }: { ready: boolean }) {
  return (
    <div className="h-dvh flex flex-col items-center justify-center canvas-bg select-none animate-fade-in">
      {/* Logo with gentle pulse */}
      <img
        src="/goclaw-icon.svg"
        alt="GoClaw"
        className="h-20 w-20 mb-6 animate-pulse"
        style={{ animationDuration: '2s' }}
      />

      {/* App name */}
      <h1 className="text-2xl font-bold text-text-primary mb-1 tracking-tight">GoClaw</h1>
      <p className="text-sm text-text-muted mb-8">Desktop AI Gateway</p>

      {/* Status */}
      <div className="flex items-center gap-2 text-xs text-text-muted">
        {ready ? (
          <>
            <span className="w-1.5 h-1.5 rounded-full bg-success" />
            <span>Ready</span>
          </>
        ) : (
          <>
            <div className="w-3 h-3 border-[1.5px] border-accent border-t-transparent rounded-full animate-spin" />
            <span>Starting gateway...</span>
          </>
        )}
      </div>
    </div>
  )
}
