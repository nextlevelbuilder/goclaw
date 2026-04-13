import { useState, useCallback, useEffect } from "react";
import {
  ChevronRight, Folder, FolderOpen, Loader2,
  FileText, Brain, StickyNote, Sparkles, Clock, Image, FileType,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { formatRelativeTime } from "@/lib/format";
import type { TreeNode } from "@/lib/file-helpers";
import type { VaultTreeEntry } from "../hooks/use-vault-tree";

const DOC_TYPE_ICONS: Record<string, { icon: LucideIcon; color: string }> = {
  context:  { icon: FileText,   color: "text-blue-600 dark:text-blue-400" },
  memory:   { icon: Brain,      color: "text-purple-600 dark:text-purple-400" },
  note:     { icon: StickyNote, color: "text-amber-600 dark:text-amber-400" },
  skill:    { icon: Sparkles,   color: "text-emerald-600 dark:text-emerald-400" },
  episodic: { icon: Clock,      color: "text-orange-600 dark:text-orange-400" },
  media:    { icon: Image,      color: "text-rose-600 dark:text-rose-400" },
  document: { icon: FileType,   color: "text-cyan-600 dark:text-cyan-400" },
};
const DEFAULT_ICON = { icon: FileText, color: "text-muted-foreground" };

const SCOPE_DOT: Record<string, string> = {
  personal: "bg-blue-400",
  team:     "bg-green-400",
  shared:   "bg-amber-400",
};

/** Truncate filename in the middle: "very-long-file-name.md" → "very-lo…me.md" */
function truncateMiddle(s: string, max = 28): string {
  if (s.length <= max) return s;
  const keep = Math.floor((max - 1) / 2);
  return s.slice(0, keep) + "…" + s.slice(s.length - keep);
}

export interface VaultTreeProps {
  tree: TreeNode[];
  meta: Map<string, VaultTreeEntry>;
  loading: boolean;
  activePath: string | null;
  onSelect: (path: string) => void;
  onLoadMore: (path: string) => void;
  /** Incremented on each loadRoot — forces re-mount to reset auto-expand state */
  treeVersion: number;
}

function VaultTreeNode({
  node, depth, meta, activePath, onSelect, onLoadMore,
}: {
  node: TreeNode; depth: number; meta: Map<string, VaultTreeEntry>;
  activePath: string | null; onSelect: (path: string) => void; onLoadMore: (path: string) => void;
}) {
  // Auto-expand first level folders (depth=0) by default
  const shouldAutoExpand = depth === 0 && node.isDir;
  const [expanded, setExpanded] = useState(shouldAutoExpand);
  const [didAutoLoad, setDidAutoLoad] = useState(false);

  // Auto-load children when auto-expanded
  useEffect(() => {
    if (shouldAutoExpand && !didAutoLoad && node.hasChildren && node.children.length === 0 && !node.loading) {
      setDidAutoLoad(true);
      onLoadMore(node.path);
    }
  }, [shouldAutoExpand, didAutoLoad, node.hasChildren, node.children.length, node.loading, node.path, onLoadMore]);
  const entry = meta.get(node.path);

  const handleToggle = useCallback(() => {
    const willExpand = !expanded;
    setExpanded(willExpand);
    if (willExpand && node.isDir && node.hasChildren && node.children.length === 0 && !node.loading) {
      onLoadMore(node.path);
    }
  }, [expanded, node.isDir, node.hasChildren, node.children.length, node.loading, node.path, onLoadMore]);

  // --- Folder node ---
  if (node.isDir) {
    return (
      <div>
        <div
          className="group flex w-full items-center gap-1 rounded px-2 py-1 text-left text-sm cursor-pointer hover:bg-accent"
          style={{ paddingLeft: `${depth * 16 + 8}px` }}
          onClick={handleToggle}
        >
          <ChevronRight className={`h-3 w-3 shrink-0 transition-transform text-muted-foreground group-hover:text-foreground ${expanded ? "rotate-90" : ""}`} />
          {expanded
            ? <FolderOpen className="h-4 w-4 shrink-0 text-yellow-600 dark:text-yellow-500" />
            : <Folder className="h-4 w-4 shrink-0 text-yellow-600 dark:text-yellow-500" />}
          <span className="truncate text-xs text-foreground/80 group-hover:text-foreground">{node.name}</span>
          {node.loading && <Loader2 className="h-3 w-3 shrink-0 animate-spin text-muted-foreground ml-auto" />}
          {!node.loading && node.children.length > 0 && (
            <span className="ml-auto shrink-0 rounded-full bg-muted px-1 text-2xs tabular-nums text-muted-foreground">
              {node.children.length}
            </span>
          )}
        </div>
        {expanded && node.children.map((child) => (
          <VaultTreeNode key={child.path} node={child} depth={depth + 1}
            meta={meta} activePath={activePath} onSelect={onSelect} onLoadMore={onLoadMore} />
        ))}
        {expanded && node.hasChildren && node.children.length === 0 && !node.loading && (
          <div className="flex items-center gap-1 text-xs text-muted-foreground cursor-pointer hover:text-foreground"
            style={{ paddingLeft: `${(depth + 1) * 16 + 20}px` }}
            onClick={() => onLoadMore(node.path)}>
            <Loader2 className="h-3 w-3" /><span>Load more</span>
          </div>
        )}
      </div>
    );
  }

  // --- File node (compact single-line) ---
  // Show filename from path (includes extension), title only in tooltip
  const isActive = activePath === node.path;
  const docType = entry?.docType ?? "";
  const { icon: Icon, color } = DOC_TYPE_ICONS[docType] ?? DEFAULT_ICON;
  const fileName = node.name; // basename from path, has extension (e.g. "knowledge_graph.md")
  const fullTitle = entry?.title || fileName;
  const scopeDot = entry?.scope ? SCOPE_DOT[entry.scope] : null;
  const relTime = entry?.updatedAt ? formatRelativeTime(entry.updatedAt) : null;

  return (
    <TooltipProvider delayDuration={400}>
      <Tooltip>
        <TooltipTrigger asChild>
          <div
            className={`group flex w-full items-center gap-1.5 rounded px-2 py-1 text-left cursor-pointer ${
              isActive ? "bg-accent" : "hover:bg-accent/50"
            }`}
            style={{ paddingLeft: `${depth * 16 + 20}px` }}
            onClick={() => onSelect(node.path)}
          >
            <Icon className={`h-3.5 w-3.5 shrink-0 ${color}`} />
            <span className={`truncate text-xs ${isActive ? "text-accent-foreground" : "text-foreground/80 group-hover:text-foreground"}`}>{truncateMiddle(fileName)}</span>
            {scopeDot && <span className={`ml-auto h-1.5 w-1.5 rounded-full shrink-0 ${scopeDot}`} />}
          </div>
        </TooltipTrigger>
        <TooltipContent side="right" className="text-xs">
          <p className="font-medium">{fullTitle}</p>
          {relTime && <p className="text-muted-foreground">{relTime}</p>}
          {entry?.scope && <p className="text-muted-foreground capitalize">{entry.scope}</p>}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

export function VaultTree({ tree, meta, loading, activePath, onSelect, onLoadMore, treeVersion }: VaultTreeProps) {
  if (loading && tree.length === 0) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }
  if (tree.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-8 gap-1 text-muted-foreground">
        <FileText className="h-5 w-5" /><span className="text-sm">No documents</span>
      </div>
    );
  }
  return (
    <div className="flex-1 min-h-0">
      {tree.map((node) => (
        <VaultTreeNode key={`${treeVersion}:${node.path}`} node={node} depth={0}
          meta={meta} activePath={activePath} onSelect={onSelect} onLoadMore={onLoadMore} />
      ))}
    </div>
  );
}
