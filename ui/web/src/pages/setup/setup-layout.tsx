import { useTranslation } from "react-i18next";

export function SetupLayout({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation("setup");

  return (
    <div className="flex min-h-dvh items-center justify-center bg-background px-4 py-8">
      <div className="w-full max-w-2xl space-y-6">
        <div className="text-center">
          <img src="/goclaw-icon.svg" alt="GoClaw" className="mx-auto mb-4 h-16 w-16" />
          <h1 className="text-4xl font-bold tracking-tight">GoClaw Setup</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {t("layout.subtitle", "Let's get your gateway up and running")}
          </p>
        </div>
        {children}
      </div>
    </div>
  );
}
