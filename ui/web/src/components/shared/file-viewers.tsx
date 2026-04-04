/**
 * FileContentBody and FileContentPanel — smart file viewers that route
 * to the correct viewer based on file type.
 *
 * Viewer panel implementations live in file-viewer-panels.tsx.
 */
import { useTranslation } from "react-i18next";
import { Loader2 } from "lucide-react";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import {
  extOf,
  langFor,
  stripFrontmatter,
  isImageFile,
  isTextFile,
  CODE_EXTENSIONS,
} from "@/lib/file-helpers";
import {
  CodeViewer,
  CsvViewer,
  ImageViewer,
  UnsupportedFileViewer,
} from "./file-viewer-panels";

export function FileContentBody({
  path,
  content,
  size,
  fetchBlob,
  onDownload,
}: {
  path: string;
  content: string;
  size?: number;
  fetchBlob?: (path: string) => Promise<Blob>;
  onDownload?: () => void;
}) {
  const ext = extOf(path);

  // Image files
  if (isImageFile(path) && fetchBlob) {
    return <ImageViewer path={path} fetchBlob={fetchBlob} />;
  }

  // Text-based files
  if (isTextFile(path) || ext === "md" || ext === "csv" || CODE_EXTENSIONS.has(ext)) {
    const displayContent = ext === "md" ? stripFrontmatter(content) : content;
    if (ext === "md") return <MarkdownRenderer content={displayContent} />;
    if (ext === "csv") return <CsvViewer content={displayContent} />;
    if (CODE_EXTENSIONS.has(ext)) return <CodeViewer content={displayContent} language={langFor(ext)} />;
    return (
      <pre className="whitespace-pre-wrap rounded-md border bg-muted/30 p-4 text-sm">
        {displayContent}
      </pre>
    );
  }

  // Unsupported files
  return <UnsupportedFileViewer path={path} size={size ?? 0} onDownload={onDownload} />;
}

export function FileContentPanel({
  fileContent,
  contentLoading,
  fetchBlob,
  onDownload,
}: {
  fileContent: { content: string; path: string; size: number } | null;
  contentLoading: boolean;
  fetchBlob?: (path: string) => Promise<Blob>;
  onDownload?: (path: string) => void;
}) {
  const { t } = useTranslation("common");
  if (contentLoading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }
  if (fileContent) {
    return (
      <FileContentBody
        path={fileContent.path}
        content={fileContent.content}
        size={fileContent.size}
        fetchBlob={fetchBlob}
        onDownload={onDownload ? () => onDownload(fileContent.path) : undefined}
      />
    );
  }
  return (
    <div className="flex items-center justify-center py-8 text-sm text-muted-foreground">
      {t("selectFileToView")}
    </div>
  );
}
