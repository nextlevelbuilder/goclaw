import { useTranslation } from "react-i18next";
import { getRuntimeBranding } from "@/lib/branding";

export function SetupLayout({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation("setup");
  const branding = getRuntimeBranding();

  return (
    <div className="flex min-h-dvh items-start justify-center bg-background px-4 py-8 sm:items-center">
      <div className="w-full max-w-2xl space-y-6 overflow-y-auto max-h-dvh sm:max-h-none">
        <div className="text-center">
          <img src={branding.logoUrl} alt={branding.appName} className="mx-auto mb-4 h-16 w-16" />
          <h1 className="text-4xl font-bold tracking-tight">{branding.appName} Setup</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {t("layout.subtitle", "Let's get your gateway up and running")}
          </p>
        </div>
        {children}
      </div>
    </div>
  );
}
