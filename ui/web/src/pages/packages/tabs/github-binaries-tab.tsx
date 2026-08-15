import { useTranslation } from "react-i18next";
import { usePackages } from "../hooks/use-packages";
import { GitHubBinariesSection } from "../github-binaries-section";

/** Thin wrapper — delegates all rendering to the shared GitHubBinariesSection component. */
export function GithubBinariesTab() {
  const { t } = useTranslation("packages");
  const { packages, installPackage, uninstallPackage } = usePackages();

  return (
    <GitHubBinariesSection
      packages={packages?.github}
      onInstall={(pkg) => installPackage(pkg, t as (key: string, opts?: Record<string, string>) => string)}
      onUninstall={(pkg) => uninstallPackage(pkg, t as (key: string, opts?: Record<string, string>) => string)}
    />
  );
}
