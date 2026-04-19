import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Loader2, Download, Trash2, Info, X, GitBranch } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";

// Viewer-safe projection — backend strips asset_url / sha256 / asset_name from
// the GET /v1/packages response (see GitHubPackageListEntry in Go). The UI
// only renders repo / tag / binaries / installed_at, so those extra fields
// were never needed on this side.
export interface GitHubPackageEntry {
  name: string;
  repo: string;
  tag: string;
  binaries: string[];
  installed_at: string;
}

interface AssetPreview {
  name: string;
  size_bytes: number;
}

interface ReleaseDTO {
  tag: string;
  name: string;
  published_at: string;
  prerelease: boolean;
  matching_assets: AssetPreview[] | null;
  all_assets_count: number;
}

interface Props {
  packages: GitHubPackageEntry[] | null | undefined;
  onInstall: (pkg: string) => Promise<{ ok: boolean }>;
  onUninstall: (pkg: string) => Promise<{ ok: boolean }>;
}

const MUSL_DISMISS_KEY = "packages.musl_warning_dismissed";

// Owner: GitHub usernames are capped at 39 chars (alnum + hyphen, no leading/trailing hyphen).
// Repo: alnum + `.`/`_`/`-`. Mirrors the backend `gitHubSpecRE`.
const OWNER_REPO_RE =
  /^([A-Za-z0-9](?:[A-Za-z0-9-]{0,37})?[A-Za-z0-9]|[A-Za-z0-9])\/[A-Za-z0-9][A-Za-z0-9._-]*$/;

function stripPrefixAndTag(spec: string): string {
  // Destructuring with a default satisfies TS `noUncheckedIndexedAccess`
  // (split is guaranteed to return ≥1 element at runtime, but TS types it
  // as `string | undefined`).
  const [name = ""] = spec.replace(/^github:/, "").split("@");
  return name;
}

function isValidRepo(spec: string): boolean {
  return OWNER_REPO_RE.test(stripPrefixAndTag(spec));
}

function isValidFullSpec(spec: string): boolean {
  // owner/repo OR owner/repo@tag (prefix `github:` optional in the input box).
  // Tag capped at 255 chars to mirror the backend regex.
  return /^([A-Za-z0-9](?:[A-Za-z0-9-]{0,37})?[A-Za-z0-9]|[A-Za-z0-9])\/[A-Za-z0-9][A-Za-z0-9._-]*(@[^\s]{1,255})?$/.test(
    spec.replace(/^github:/, "")
  );
}

export function GitHubBinariesSection({ packages, onInstall, onUninstall }: Props) {
  const { t } = useTranslation("packages");
  const [input, setInput] = useState("");
  const [installing, setInstalling] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [pickerRepo, setPickerRepo] = useState("");
  const [uninstallTarget, setUninstallTarget] = useState<string | null>(null);
  const [dismissed, setDismissed] = useState<boolean>(() => {
    try {
      return window.localStorage.getItem(MUSL_DISMISS_KEY) === "1";
    } catch {
      return false;
    }
  });

  const handleDismiss = () => {
    setDismissed(true);
    try {
      window.localStorage.setItem(MUSL_DISMISS_KEY, "1");
    } catch {
      /* ignore */
    }
  };

  const handleBrowse = () => {
    if (!isValidRepo(input)) return;
    setPickerRepo(stripPrefixAndTag(input));
    setPickerOpen(true);
  };

  const handleInstall = async () => {
    const spec = input.trim();
    if (!isValidFullSpec(spec)) return;
    setInstalling(true);
    const full = spec.startsWith("github:") ? spec : `github:${spec}`;
    const res = await onInstall(full);
    setInstalling(false);
    if (res.ok) setInput("");
  };

  return (
    <section>
      <div className="flex items-center gap-2 mb-3">
        <GitBranch className="h-4 w-4 text-muted-foreground" />
        <h2 className="text-lg font-medium">{t("github.title")}</h2>
      </div>

      {!dismissed && (
        <Alert className="mb-3 border-amber-200/70 bg-amber-50/70 text-amber-950 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-100">
          <Info className="h-4 w-4 text-amber-600 dark:text-amber-300" />
          <AlertDescription className="flex items-start justify-between gap-2 text-xs text-amber-800 dark:text-amber-200">
            <span className="flex-1">{t("github.muslWarning")}</span>
            <button
              onClick={handleDismiss}
              className="shrink-0 rounded p-1 hover:bg-amber-100 dark:hover:bg-amber-900"
              aria-label={t("github.muslDismiss")}
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </AlertDescription>
        </Alert>
      )}

      <div className="flex flex-col sm:flex-row gap-2 mb-3">
        <input
          type="text"
          className="flex-1 rounded-md border border-input bg-background px-3 py-2 text-base md:text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          placeholder={t("github.placeholder")}
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleInstall()}
          disabled={installing}
        />
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={handleBrowse}
            disabled={!isValidRepo(input) || installing}
          >
            {t("github.browse")}
          </Button>
          <Button size="sm" onClick={handleInstall} disabled={!isValidFullSpec(input) || installing}>
            {installing ? (
              <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />
            ) : (
              <Download className="mr-1.5 h-4 w-4" />
            )}
            {installing ? t("actions.installing") : t("actions.install")}
          </Button>
        </div>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full min-w-[600px] text-sm">
          <thead>
            <tr className="border-b">
              <th className="text-left py-2 px-3 font-medium text-muted-foreground">
                {t("github.columns.repo")}
              </th>
              <th className="text-left py-2 px-3 font-medium text-muted-foreground">
                {t("github.columns.tag")}
              </th>
              <th className="text-left py-2 px-3 font-medium text-muted-foreground">
                {t("github.columns.binaries")}
              </th>
              <th className="text-left py-2 px-3 font-medium text-muted-foreground">
                {t("github.columns.installedAt")}
              </th>
              <th className="text-right py-2 px-3 font-medium text-muted-foreground">
                {t("table.actions")}
              </th>
            </tr>
          </thead>
          <tbody>
            {!packages?.length ? (
              <tr>
                <td colSpan={5} className="py-6 text-center text-muted-foreground text-sm">
                  {t("table.empty")}
                </td>
              </tr>
            ) : (
              packages.map((pkg) => (
                <tr key={pkg.name} className="border-b last:border-0 hover:bg-muted/50 transition-colors">
                  <td className="py-2 px-3 font-mono">{pkg.repo}</td>
                  <td className="py-2 px-3 font-mono">{pkg.tag}</td>
                  <td className="py-2 px-3 font-mono text-xs">{pkg.binaries?.join(", ")}</td>
                  <td className="py-2 px-3 text-muted-foreground text-xs">
                    {new Date(pkg.installed_at).toLocaleDateString()}
                  </td>
                  <td className="py-2 px-3 text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 px-2 text-destructive hover:text-destructive hover:bg-destructive/10"
                      onClick={() => setUninstallTarget(pkg.name)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      <GitHubReleasePicker
        repo={pickerRepo}
        open={pickerOpen}
        onClose={() => setPickerOpen(false)}
        onSelect={(tag) => {
          setInput(`${pickerRepo}@${tag}`);
          setPickerOpen(false);
        }}
      />

      <ConfirmDialog
        open={!!uninstallTarget}
        onOpenChange={() => setUninstallTarget(null)}
        title={t("confirmUninstall.title")}
        description={t("confirmUninstall.description", { name: uninstallTarget })}
        confirmLabel={t("actions.uninstall")}
        variant="destructive"
        onConfirm={async () => {
          if (uninstallTarget) {
            await onUninstall(`github:${uninstallTarget}`);
            setUninstallTarget(null);
          }
        }}
      />
    </section>
  );
}

interface PickerProps {
  repo: string;
  open: boolean;
  onClose: () => void;
  onSelect: (tag: string) => void;
}

function GitHubReleasePicker({ repo, open, onClose, onSelect }: PickerProps) {
  const { t } = useTranslation("packages");
  const http = useHttp();
  const { data, isFetching } = useQuery({
    queryKey: [...queryKeys.packages.all, "github-releases", repo],
    queryFn: () =>
      http.get<{ releases: ReleaseDTO[] }>(
        `/v1/packages/github-releases?repo=${encodeURIComponent(repo)}&limit=10`
      ),
    enabled: open && !!repo,
    staleTime: 10 * 60 * 1000,
  });

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("github.pickerTitle", { repo })}</DialogTitle>
        </DialogHeader>
        {isFetching ? (
          <div className="py-6 text-center text-muted-foreground">
            <Loader2 className="h-5 w-5 animate-spin mx-auto mb-2" />
            <span className="text-sm">{t("github.pickerLoading")}</span>
          </div>
        ) : !data?.releases?.length ? (
          <div className="py-6 text-center text-muted-foreground text-sm">
            {t("github.pickerEmpty", { repo })}
          </div>
        ) : (
          <div className="divide-y max-h-[60vh] overflow-y-auto overscroll-contain">
            {data.releases.map((rel) => (
              <button
                key={rel.tag}
                onClick={() => onSelect(rel.tag)}
                className="w-full text-left py-3 px-2 hover:bg-muted/50 transition-colors"
              >
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0 flex-1">
                    <div className="font-medium font-mono text-sm flex items-center gap-2">
                      {rel.tag}
                      {rel.prerelease && (
                        <span className="text-xs bg-amber-100 dark:bg-amber-900/40 text-amber-900 dark:text-amber-200 rounded px-1.5 py-0.5">
                          {t("github.pickerPrerelease")}
                        </span>
                      )}
                    </div>
                    {rel.name && rel.name !== rel.tag && (
                      <div className="text-xs text-muted-foreground truncate">{rel.name}</div>
                    )}
                    <div className="text-xs text-muted-foreground mt-1">
                      {new Date(rel.published_at).toLocaleDateString()} ·{" "}
                      {t("github.pickerMatchingAssets", {
                        count: rel.matching_assets?.length ?? 0,
                        total: rel.all_assets_count,
                      })}
                    </div>
                    {rel.matching_assets?.[0] && (
                      <div className="text-xs font-mono text-muted-foreground mt-1 truncate">
                        {rel.matching_assets[0].name}
                      </div>
                    )}
                  </div>
                  <Button size="sm" variant="outline" className="shrink-0">
                    {t("github.pickerSelect")}
                  </Button>
                </div>
              </button>
            ))}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
