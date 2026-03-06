export function SetupLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4 py-8">
      <div className="w-full max-w-2xl space-y-6">
        <div className="text-center">
          <h1 className="text-3xl font-semibold tracking-tight">GoClaw Setup</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            Let's get your gateway up and running
          </p>
        </div>
        {children}
      </div>
    </div>
  );
}
