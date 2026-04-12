/** Sub-components for rendering individual file entries and skill rows in the upload dialog. */
import { CheckCircle2, XCircle, Loader2, TriangleAlert, X, Package } from "lucide-react";
import type { FileEntry, SkillEntry, SkillStatus } from "./lib/skill-upload-types";

type TFunc = (key: string, opts?: Record<string, unknown>) => string;

// ---------------------------------------------------------------------------
// FileEntryBlock — renders a ZIP file with its skill rows
// ---------------------------------------------------------------------------

export function FileEntryBlock({
  entry,
  onRemove,
  uploading,
  t,
}: {
  entry: FileEntry;
  onRemove: () => void;
  uploading: boolean;
  t: TFunc;
}) {
  const isMulti = entry.skills.length > 1;
  const sizeKB = (entry.file.size / 1024).toFixed(1);
  const isValidating = entry.skills.some((s) => s.status === "validating");

  if (!isMulti) {
    // Single-skill: one row, ZIP filename as subtitle
    const skill = entry.skills[0]!;
    return (
      <SkillRow
        skill={skill}
        subtitle={skill.name ? entry.file.name : undefined}
        primaryLabel={skill.name || entry.file.name}
        sizeKB={sizeKB}
        showSize
        onRemove={onRemove}
        uploading={uploading}
        t={t}
      />
    );
  }

  // Multi-skill: group header + individual skill rows
  return (
    <div className="rounded-md border overflow-hidden">
      {/* ZIP group header */}
      <div className="flex items-center gap-2 bg-muted/40 px-3 py-1.5 text-xs text-muted-foreground">
        <Package className="h-3.5 w-3.5 shrink-0" />
        <span className="flex-1 truncate font-medium">{entry.file.name}</span>
        <span className="shrink-0">{sizeKB} KB</span>
        {isValidating ? null : (
          <span className="shrink-0">
            {t("upload.multiDetected", { count: entry.skills.length })}
          </span>
        )}
        {!uploading && (
          <button
            type="button"
            aria-label={t("upload.remove")}
            onClick={(e) => { e.stopPropagation(); onRemove(); }}
            className="shrink-0 rounded-sm p-0.5 text-muted-foreground hover:text-foreground"
          >
            <X className="h-3 w-3" />
          </button>
        )}
      </div>

      {/* Individual skill rows — indented */}
      <div className="flex flex-col divide-y">
        {entry.skills.map((skill) => (
          <SkillRow
            key={skill.id}
            skill={skill}
            primaryLabel={skill.name || skill.dir || skill.slug || "…"}
            subtitle={skill.dir || undefined}
            showSize={false}
            onRemove={undefined}
            uploading={uploading}
            t={t}
            indent
          />
        ))}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// SkillRow — single skill entry row
// ---------------------------------------------------------------------------

export function SkillRow({
  skill,
  primaryLabel,
  subtitle,
  sizeKB,
  showSize,
  onRemove,
  uploading,
  t,
  indent = false,
}: {
  skill: SkillEntry;
  primaryLabel: string;
  subtitle?: string;
  sizeKB?: string;
  showSize: boolean;
  onRemove?: () => void;
  uploading: boolean;
  t: TFunc;
  indent?: boolean;
}) {
  const canRemove = !uploading && skill.status !== "uploading" && skill.status !== "success" && onRemove;

  return (
    <div className={`flex items-center gap-2 px-3 py-2 text-sm ${indent ? "pl-6" : "rounded-md border"}`}>
      <SkillStatusIcon status={skill.status} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="truncate font-medium">{primaryLabel}</span>
          <SkillBadge status={skill.status} t={t} />
          {showSize && sizeKB && (
            <span className="shrink-0 text-xs text-muted-foreground">{sizeKB} KB</span>
          )}
        </div>
        <SkillSubtitle skill={skill} subtitle={subtitle} t={t} />
      </div>
      {canRemove && (
        <button
          type="button"
          aria-label={t("upload.remove")}
          onClick={(e) => { e.stopPropagation(); onRemove(); }}
          className="shrink-0 rounded-sm p-1 text-muted-foreground hover:text-foreground"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// SkillSubtitle
// ---------------------------------------------------------------------------

export function SkillSubtitle({
  skill,
  subtitle,
  t,
}: {
  skill: SkillEntry;
  subtitle?: string;
  t: TFunc;
}) {
  if (skill.status === "invalid" || skill.status === "error") {
    return (
      <p className="text-xs text-destructive truncate">
        {skill.error ? t(skill.error) : t("upload.failed")}
      </p>
    );
  }
  if (skill.status === "warning") {
    return <p className="text-xs text-amber-600 truncate">{skill.error ?? t("upload.failed")}</p>;
  }
  if (skill.status === "validating") {
    return <p className="text-xs text-muted-foreground">{t("upload.validating")}</p>;
  }
  if (skill.status === "unchanged") {
    return (
      <p className="text-xs text-muted-foreground truncate">
        {t("upload.skillUnchanged")}
      </p>
    );
  }
  if (subtitle && skill.status !== "success") {
    return <p className="text-xs text-muted-foreground truncate">{subtitle}</p>;
  }
  return null;
}

// ---------------------------------------------------------------------------
// SkillBadge — colored badge for NEW / UNCHANGED / ERROR states
// ---------------------------------------------------------------------------

export function SkillBadge({ status, t }: { status: SkillStatus; t: TFunc }) {
  const base = "shrink-0 text-[10px] font-semibold uppercase px-1.5 py-0.5 rounded";
  switch (status) {
    case "valid":
      return <span className={`${base} bg-green-50 text-green-700 border border-green-200`}>{t("upload.new")}</span>;
    case "unchanged":
      return <span className={`${base} bg-purple-50 text-purple-700 border border-purple-200`}>{t("upload.unchanged")}</span>;
    case "invalid":
    case "error":
      return <span className={`${base} bg-red-50 text-red-700 border border-red-200`}>{t("upload.failed")}</span>;
    default:
      return null;
  }
}

// ---------------------------------------------------------------------------
// SkillStatusIcon
// ---------------------------------------------------------------------------

export function SkillStatusIcon({ status }: { status: SkillStatus }) {
  switch (status) {
    case "validating":
    case "uploading":
      return <Loader2 className="h-4 w-4 shrink-0 animate-spin text-muted-foreground" />;
    case "valid":
      return <CheckCircle2 className="h-4 w-4 shrink-0 text-primary" />;
    case "unchanged":
      return <CheckCircle2 className="h-4 w-4 shrink-0 text-muted-foreground" />;
    case "success":
      return <CheckCircle2 className="h-4 w-4 shrink-0 text-green-600" />;
    case "warning":
      return <TriangleAlert className="h-4 w-4 shrink-0 text-amber-600" />;
    case "invalid":
    case "error":
      return <XCircle className="h-4 w-4 shrink-0 text-destructive" />;
  }
}
