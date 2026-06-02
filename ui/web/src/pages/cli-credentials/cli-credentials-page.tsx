import { useTranslation } from "react-i18next";
import { PageHeader } from "@/components/shared/page-header";
import { CliCredentialsPanel } from "./cli-credentials-panel";

/**
 * CliCredentialsPage — standalone route wrapper.
 * The route /cli-credentials now redirects to /packages?tab=cli-credentials.
 * This page is kept for backward compat in case the redirect is bypassed.
 * All content logic lives in CliCredentialsPanel (shared with tab).
 */
export function CliCredentialsPage() {
  const { t } = useTranslation("cli-credentials");

  return (
    <div className="p-4 sm:p-6">
      <PageHeader title={t("title")} description={t("description")} />
      <div className="mt-4">
        <CliCredentialsPanel />
      </div>
    </div>
  );
}
