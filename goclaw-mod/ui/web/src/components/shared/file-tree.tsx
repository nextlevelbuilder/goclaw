import { useState, useEffect, useCallback, useMemo } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { DndContext, DragOverlay } from "@dnd-kit/core";
import { Folder, FolderOpen, ChevronRight, Loader2, Trash2 } from "lucide-react";
import { formatSize, type TreeNode } from "@/lib/file-helpers";
import { useTreeDnd } from "@/hooks/use-tree-dnd";
import { DragPreview } from "@/components/shared/drag-preview";
import { FileIcon } from "./file-tree-file-icon";
import { DraggableItem, DroppableFolder, RootDropZone } from "./file-tree-dnd-wrappers";

export function TreeItem({
  node,
  depth,
  activePath,
  onSelect,
  onDelete,
  onLoadMore,
  dndEnabled,
  autoExpandPath,
  showSize,
}: {
  node: TreeNode;
  depth: number;
  activePath: string | null;
  onSelect: (path: string) => void;
  onDelete?: (path: string, isDir: boolean) => void;
  onLoadMore?: (path: string) => void;
  dndEnabled: boolean;
  autoExpandPath: string | null;
  showSize?: boolean;
}) {
  const { t } = useTranslation("common");
  const [expanded, setExpanded] = useState(depth === 0);
  const isActive = activePath === node.path;

  // Auto-expand folder when hovered during drag for 800ms.
  useEffect(() => {
    if (autoExpandPath === node.path && node.isDir && !expanded) {
      setExpanded(true);
      if (node.hasChildren && node.children.length === 0 && !node.loading) {
        onLoadMore?.(node.path);
      }
    }
  }, [autoExpandPath, node.path, node.isDir, expanded, node.hasChildren, node.children.length, node.loading, onLoadMore]);

  const handleToggle = useCallback(() => {
    const willExpand = !expanded;
    setExpanded(willExpand);
    if (willExpand && node.isDir && node.hasChildren && node.children.length === 0 && !node.loading) {
      onLoadMore?.(node.path);
    }
  }, [expanded, node.isDir, node.hasChildren, node.children.length, node.loading, node.path, onLoadMore]);

  const deleteBtn = onDelete && !node.protected && (
    <button
      type="button"
      className="ml-auto shrink-0 opacity-0 group-hover/tree-item:opacity-100 text-destructive hover:text-destructive/80 transition-opacity cursor-pointer p-0.5"
      title={node.isDir ? t("deleteFolder") : t("deleteFile")}
      onClick={(e) => { e.stopPropagation(); onDelete(node.path, node.isDir); }}
    >
      <Trash2 className="h-3.5 w-3.5" />
    </button>
  );

  const sizeLabel = showSize && (node.isDir ? 0 : node.size) > 0 && (
    <span className="ml-auto shrink-0 text-2xs text-muted-foreground tabular-nums">
      {formatSize(node.size)}
    </span>
  );

  if (node.isDir) {
    const folderContent = (isDropTargetActive: boolean) => (
      <>
        <div
          className={`group/tree-item flex w-full items-center gap-1 rounded px-2 py-1 text-left text-sm cursor-pointer ${
            isDropTargetActive ? "bg-primary/10 ring-1 ring-primary" : "hover:bg-accent"
          }`}
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
            dndEnabled={dndEnabled}

            autoExpandPath={autoExpandPath}
            showSize={showSize}
          />
        ))}
        {expanded && node.hasChildren && node.children.length === 0 && !node.loading && (
          <div
            className="flex items-center gap-1 text-xs text-muted-foreground cursor-pointer hover:text-foreground"
            style={{ paddingLeft: `${(depth + 1) * 16 + 20}px` }}
            onClick={() => onLoadMore?.(node.path)}
          >
            <Loader2 className="h-3 w-3" />
            <span>{t("loadMore")}</span>
          </div>
        )}
      </>
    );

    if (dndEnabled) {
      return (
        <DraggableItem id={node.path} enabled>
          <DroppableFolder id={node.path} enabled>
            {({ isDropTarget: active }) => folderContent(active)}
          </DroppableFolder>
        </DraggableItem>
      );
    }

    return <div>{folderContent(false)}</div>;
  }

  // File node
  const fileContent = (
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

  if (dndEnabled) {
    return (
      <DraggableItem id={node.path} enabled>
        {fileContent}
      </DraggableItem>
    );
  }

  return fileContent;
}

/** Find a node by path in the tree. */
function findNode(tree: TreeNode[], path: string): TreeNode | undefined {
  for (const node of tree) {
    if (node.path === path) return node;
    if (node.children.length > 0) {
      const found = findNode(node.children, path);
      if (found) return found;
    }
  }
  return undefined;
}

export function FileTreePanel({
  tree,
  filesLoading,
  activePath,
  onSelect,
  onDelete,
  onLoadMore,
  onMove,
  showSize,
}: {
  tree: TreeNode[];
  filesLoading: boolean;
  activePath: string | null;
  onSelect: (path: string) => void;
  onDelete?: (path: string, isDir: boolean) => void;
  onLoadMore?: (path: string) => void;
  onMove?: (fromPath: string, toFolder: string) => void;
  showSize?: boolean;
}) {
  const { t } = useTranslation("common");
  const { sensors, activeId, autoExpandPath, handlers } = useTreeDnd(onMove);
  const dndEnabled = !!onMove;

  // Find the active node for DragOverlay preview.
  const activeNode = useMemo(
    () => (activeId ? findNode(tree, activeId) : undefined),
    [activeId, tree],
  );

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

  const treeContent = (
    <div className="flex-1 min-h-0">
      {/* Root-level drop target for moving to root */}
      {dndEnabled ? (
        <RootDropZone>
          {tree.map((node) => (
            <TreeItem
              key={node.path} node={node} depth={0} activePath={activePath}
              onSelect={onSelect} onDelete={onDelete} onLoadMore={onLoadMore}
              dndEnabled={dndEnabled} autoExpandPath={autoExpandPath}
              showSize={showSize}
            />
          ))}
        </RootDropZone>
      ) : (
        tree.map((node) => (
          <TreeItem
            key={node.path} node={node} depth={0} activePath={activePath}
            onSelect={onSelect} onDelete={onDelete} onLoadMore={onLoadMore}
            dndEnabled={false} autoExpandPath={null}
            showSize={showSize}
          />
        ))
      )}
    </div>
  );

  if (!dndEnabled) return treeContent;

  return (
    <DndContext sensors={sensors} {...handlers}>
      {treeContent}
      {/* Portal to document.body so DragOverlay isn't offset by Radix Dialog's CSS transform. */}
      {createPortal(
        <DragOverlay dropAnimation={null}>
          {activeNode ? (
            <DragPreview name={activeNode.name} isDir={activeNode.isDir} />
          ) : null}
        </DragOverlay>,
        document.body,
      )}
    </DndContext>
  );
}

