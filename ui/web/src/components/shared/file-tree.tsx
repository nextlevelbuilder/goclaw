import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Folder,
  FolderOpen,
  FileText,
  FileCode2,
  File,
  FileImage,
  FileJson2,
  FileSpreadsheet,
  FileTerminal,
  FileArchive,
  FileVideo,
  FileAudio,
  FileCog,
  FileType,
  FileLock,
  ChevronRight,
  Loader2,
  Trash2,
} from "lucide-react";
import { extOf, CODE_EXTENSIONS, IMAGE_EXTENSIONS, formatSize, type TreeNode } from "@/lib/file-helpers";

const cls = "h-4 w-4 shrink-0";

function FileIcon({ name }: { name: string }) {
  const ext = extOf(name);

  // Markdown
  if (ext === "md" || ext === "mdx") return <FileText className={`${cls} text-blue-500`} />;
  // JSON / YAML config
  if (ext === "json" || ext === "json5") return <FileJson2 className={`${cls} text-yellow-600`} />;
  if (ext === "yaml" || ext === "yml" || ext === "toml") return <FileCog className={`${cls} text-purple-500`} />;
  // Spreadsheet / CSV
  if (ext === "csv") return <FileSpreadsheet className={`${cls} text-green-600`} />;
  // Shell / terminal
  if (ext === "sh" || ext === "bash" || ext === "zsh") return <FileTerminal className={`${cls} text-lime-600`} />;
  // Images
  if (IMAGE_EXTENSIONS.has(ext)) return <FileImage className={`${cls} text-emerald-500`} />;
  // Video
  if (ext === "mp4" || ext === "webm" || ext === "mov" || ext === "avi" || ext === "mkv") return <FileVideo className={`${cls} text-pink-500`} />;
  // Audio
  if (ext === "mp3" || ext === "wav" || ext === "ogg" || ext === "flac" || ext === "m4a") return <FileAudio className={`${cls} text-violet-500`} />;
  // Archive
  if (ext === "zip" || ext === "tar" || ext === "gz" || ext === "rar" || ext === "7z" || ext === "bz2") return <FileArchive className={`${cls} text-amber-600`} />;
  // Font
  if (ext === "ttf" || ext === "otf" || ext === "woff" || ext === "woff2") return <FileType className={`${cls} text-slate-500`} />;
  // Env / secrets
  if (ext === "env" || ext === "pem" || ext === "key" || ext === "crt") return <FileLock className={`${cls} text-red-500`} />;
  // Code (generic)
  if (CODE_EXTENSIONS.has(ext)) return <FileCode2 className={`${cls} text-orange-500`} />;
  // Default
  return <File className={`${cls} text-muted-foreground`} />;
}

export function TreeItem({
  node,
  depth,
  activePath,
  onSelect,
  onDelete,
  onLoadMore,
  showSize,
}: {
  node: TreeNode;
  depth: number;
  activePath: string | null;
  onSelect: (path: string) => void;
  onDelete?: (path: string, isDir: boolean) => void;
  onLoadMore?: (path: string) => void;
  showSize?: boolean;
}) {
  const [expanded, setExpanded] = useState(depth === 0);
  const isActive = activePath === node.path;

  const handleToggle = () => {
    const willExpand = !expanded;
    setExpanded(willExpand);
    // Lazy load: if expanding a folder with hasChildren but no loaded children
    if (willExpand && node.isDir && node.hasChildren && node.children.length === 0 && !node.loading) {
      onLoadMore?.(node.path);
    }
  };

  const deleteBtn = onDelete && !node.protected && (
    <button
      type="button"
      className="ml-auto shrink-0 opacity-0 group-hover/tree-item:opacity-100 text-destructive hover:text-destructive/80 transition-opacity cursor-pointer p-0.5"
      title={`Delete ${node.isDir ? "folder" : "file"}`}
      onClick={(e) => { e.stopPropagation(); onDelete(node.path, node.isDir); }}
    >
      <Trash2 className="h-3.5 w-3.5" />
    </button>
  );

  const sizeLabel = showSize && (node.isDir ? 0 : node.size) > 0 && (
    <span className="ml-auto shrink-0 text-[10px] text-muted-foreground tabular-nums">
      {formatSize(node.size)}
    </span>
  );

  if (node.isDir) {
    return (
      <div>
        <div
          className="group/tree-item flex w-full items-center gap-1 rounded px-2 py-1 text-left text-sm hover:bg-accent cursor-pointer"
          style={{ paddingLeft: `${depth * 16 + 8}px` }}
          onClick={handleToggle}
        >
          <ChevronRight
            className={`h-3 w-3 shrink-0 transition-transform ${expanded ? "rotate-90" : ""}`}
          />
          {expanded ? (
            <FolderOpen className="h-4 w-4 shrink-0 text-yellow-600" />
          ) : (
            <Folder className="h-4 w-4 shrink-0 text-yellow-600" />
          )}
          <span className="truncate">{node.name}</span>
          {node.loading && <Loader2 className="h-3 w-3 shrink-0 animate-spin text-muted-foreground ml-1" />}
          {sizeLabel}
          {deleteBtn}
        </div>
        {expanded && node.children.map((child) => (
          <TreeItem
            key={child.path}
            node={child}
            depth={depth + 1}
            activePath={activePath}
            onSelect={onSelect}
            onDelete={onDelete}
            onLoadMore={onLoadMore}
            showSize={showSize}
          />
        ))}
        {/* Show chevron hint for loadable but not-yet-expanded empty folders */}
        {expanded && node.hasChildren && node.children.length === 0 && !node.loading && (
          <div
            className="flex items-center gap-1 text-xs text-muted-foreground cursor-pointer hover:text-foreground"
            style={{ paddingLeft: `${(depth + 1) * 16 + 20}px` }}
            onClick={() => onLoadMore?.(node.path)}
          >
            <Loader2 className="h-3 w-3" />
            <span>Load more...</span>
          </div>
        )}
      </div>
    );
  }

  return (
    <div
      className={`group/tree-item flex w-full items-center gap-1.5 rounded px-2 py-1 text-left text-sm cursor-pointer ${
        isActive ? "bg-accent text-accent-foreground" : "hover:bg-accent/50"
      }`}
      style={{ paddingLeft: `${depth * 16 + 20}px` }}
      onClick={() => onSelect(node.path)}
    >
      <FileIcon name={node.name} />
      <span className="truncate">{node.name}</span>
      {sizeLabel}
      {deleteBtn}
    </div>
  );
}

export function FileTreePanel({
  tree,
  filesLoading,
  activePath,
  onSelect,
  onDelete,
  onLoadMore,
  showSize,
}: {
  tree: TreeNode[];
  filesLoading: boolean;
  activePath: string | null;
  onSelect: (path: string) => void;
  onDelete?: (path: string, isDir: boolean) => void;
  onLoadMore?: (path: string) => void;
  showSize?: boolean;
}) {
  const { t } = useTranslation("common");
  if (filesLoading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }
  if (tree.length === 0) {
    return <p className="px-3 py-4 text-sm text-muted-foreground">{t("noFiles")}</p>;
  }
  return (
    <>
      {tree.map((node) => (
        <TreeItem key={node.path} node={node} depth={0} activePath={activePath} onSelect={onSelect} onDelete={onDelete} onLoadMore={onLoadMore} showSize={showSize} />
      ))}
    </>
  );
}
