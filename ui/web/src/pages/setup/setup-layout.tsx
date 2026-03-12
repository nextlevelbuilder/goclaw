import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { ROUTES } from "@/lib/constants";

export function SetupLayout({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation("setup");
  const navigate = useNavigate();

  const handleSkipSetup = () => {
    localStorage.setItem("setup_skipped", "1");
    navigate(ROUTES.OVERVIEW, { replace: true });
  };

  return (
    <div className="flex min-h-dvh items-center justify-center bg-background px-4 py-8">
      <div className="w-full max-w-2xl space-y-6">
        <div className="text-center">
          <h1 className="text-3xl font-semibold tracking-tight">GoClaw Setup</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {t("layout.subtitle", "Let's get your gateway up and running")}
          </p>
          <button
            type="button"
            onClick={handleSkipSetup}
            className="mt-1 text-xs text-muted-foreground/60 underline-offset-2 hover:text-muted-foreground hover:underline"
          >
            {t("layout.skipSetup", "Skip setup and go to dashboard")}
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}
