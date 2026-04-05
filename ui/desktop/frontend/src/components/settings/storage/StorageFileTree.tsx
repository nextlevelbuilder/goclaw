// Recursive file tree with drag-and-drop move support for storage management.
// Uses @dnd-kit/core for DnD — mirrors web UI file-tree.tsx adapted for desktop styling.

import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import {
  DndContext,
  DragOverlay,
  useDroppable,
  useDraggable,
  PointerSensor,
  useSensor,
  useSensors,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
} from '@dnd-kit/core'
import {
  extOf,
  CODE_EXTENSIONS,
  IMAGE_EXTENSIONS,
  formatSize,
  type TreeNode,
} from '../../../lib/file-helpers'

// --- Inline SVG icons (desktop convention: no lucide-react) ---

const cls = 'h-4 w-4 shrink-0'

function ChevronRightIcon({ className }: { className?: string }) {
  return (
    <svg className={className ?? 'h-3 w-3 shrink-0'} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <polyline points="9 18 15 12 9 6" />
    </svg>
  )
}

function FolderIcon({ open }: { open?: boolean }) {
  if (open) {
    return (
      <svg className={`${cls} text-yellow-600`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
        <path d="M5 19a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2h4l2 2h4a2 2 0 0 1 2 2v1" />
        <path d="M20.28 11H7.72a2 2 0 0 0-1.93 1.47l-1.44 5.09A2 2 0 0 0 6.28 20h11.44a2 2 0 0 0 1.93-2.44l-1.44-5.09A2 2 0 0 0 16.28 11z" />
      </svg>
    )
  }
  return (
    <svg className={`${cls} text-yellow-600`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" />
    </svg>
  )
}

function FileIcon({ name }: { name: string }) {
  const ext = extOf(name)
  let color = 'text-text-muted'
  if (ext === 'md' || ext === 'mdx') color = 'text-blue-500'
  else if (ext === 'json' || ext === 'json5') color = 'text-yellow-600'
  else if (ext === 'yaml' || ext === 'yml' || ext === 'toml') color = 'text-orange-500'
  else if (ext === 'csv') color = 'text-green-600'
  else if (ext === 'sh' || ext === 'bash' || ext === 'zsh') color = 'text-lime-600'
  else if (IMAGE_EXTENSIONS.has(ext)) color = 'text-emerald-500'
  else if (CODE_EXTENSIONS.has(ext)) color = 'text-orange-500'

  return (
    <svg className={`${cls} ${color}`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z" />
      <path d="M14 2v4a2 2 0 0 0 2 2h4" />
    </svg>
  )
}

function SpinnerIcon() {
  return (
    <svg className="h-3 w-3 shrink-0 animate-spin text-text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
      <path d="M21 12a9 9 0 1 1-6.219-8.56" />
    </svg>
  )
}

function TrashIcon() {
  return (
    <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <path d="M3 6h18" /><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6" />
      <path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2" />
    </svg>
  )
}

// --- DnD wrappers ---

function DraggableItem({ id, enabled, children }: { id: string; enabled: boolean; children: React.ReactNode }) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({ id, disabled: !enabled })
  return (
    <div ref={setNodeRef} {...(enabled ? { ...listeners, ...attributes } : {})} className={isDragging ? 'opacity-40' : ''}>
      {children}
    </div>
  )
}

function DroppableFolder({ id, enabled, children }: { id: string; enabled: boolean; children: (props: { isDropTarget: boolean }) => React.ReactNode }) {
  const { setNodeRef, isOver } = useDroppable({ id, disabled: !enabled })
  return <div ref={setNodeRef}>{children({ isDropTarget: isOver })}</div>
}

function RootDropZone({ children }: { children: React.ReactNode }) {
  const { setNodeRef, isOver } = useDroppable({ id: '__root__' })
  return <div ref={setNodeRef} className={`min-h-full ${isOver ? 'bg-accent/5' : ''}`}>{children}</div>
}

// --- DragPreview ---

function DragPreview({ name, isDir }: { name: string; isDir: boolean }) {
  return (
    <div className="flex items-center gap-1.5 rounded-md bg-surface-secondary border border-border px-2.5 py-1.5 text-xs shadow-lg">
      {isDir ? <FolderIcon /> : <FileIcon name={name} />}
      <span className="truncate max-w-[180px]">{name}</span>
    </div>
  )
}

// --- TreeItem ---

function TreeItem({
  node, depth, activePath, onSelect, onDelete, onLoadMore, dndEnabled, autoExpandPath, showSize,
}: {
  node: TreeNode; depth: number; activePath: string | null
  onSelect: (path: string) => void; onDelete?: (path: string, isDir: boolean) => void
  onLoadMore?: (path: string) => void; dndEnabled: boolean; autoExpandPath: string | null; showSize?: boolean
}) {
  const { t } = useTranslation('common')
  const [expanded, setExpanded] = useState(depth === 0)
  const isActive = activePath === node.path

  // Auto-expand folder when hovered during drag
  useEffect(() => {
    if (autoExpandPath === node.path && node.isDir && !expanded) {
      setExpanded(true)
      if (node.hasChildren && node.children.length === 0 && !node.loading) {
        onLoadMore?.(node.path)
      }
    }
  }, [autoExpandPath, node.path, node.isDir, expanded, node.hasChildren, node.children.length, node.loading, onLoadMore])

  const handleToggle = useCallback(() => {
    const willExpand = !expanded
    setExpanded(willExpand)
    if (willExpand && node.isDir && node.hasChildren && node.children.length === 0 && !node.loading) {
      onLoadMore?.(node.path)
    }
  }, [expanded, node.isDir, node.hasChildren, node.children.length, node.loading, node.path, onLoadMore])

  const deleteBtn = onDelete && !node.protected && (
    <button
      type="button"
      className="ml-auto shrink-0 opacity-0 group-hover/tree-item:opacity-100 text-error hover:text-error/80 transition-opacity cursor-pointer p-0.5"
      title={node.isDir ? t('deleteFolder') : t('deleteFile')}
      onClick={(e) => { e.stopPropagation(); onDelete(node.path, node.isDir) }}
    >
      <TrashIcon />
    </button>
  )

  const sizeLabel = showSize && !node.isDir && node.size > 0 && (
    <span className="ml-auto shrink-0 text-[10px] text-text-muted tabular-nums">
      {formatSize(node.size)}
    </span>
  )

  if (node.isDir) {
    const folderContent = (isDropTargetActive: boolean) => (
      <>
        <div
          className={`group/tree-item flex w-full items-center gap-1 rounded px-2 py-1 text-left text-xs cursor-pointer ${
            isDropTargetActive ? 'bg-accent/10 ring-1 ring-accent' : 'hover:bg-surface-tertiary'
          }`}
          style={{ paddingLeft: `${depth * 16 + 8}px` }}
          onClick={handleToggle}
        >
          <ChevronRightIcon className={`h-3 w-3 shrink-0 transition-transform ${expanded ? 'rotate-90' : ''}`} />
          <FolderIcon open={expanded} />
          <span className="truncate text-text-primary">{node.name}</span>
          {node.loading && <SpinnerIcon />}
          {sizeLabel}
          {deleteBtn}
        </div>
        {expanded && node.children.map((child) => (
          <TreeItem
            key={child.path} node={child} depth={depth + 1} activePath={activePath}
            onSelect={onSelect} onDelete={onDelete} onLoadMore={onLoadMore}
            dndEnabled={dndEnabled} autoExpandPath={autoExpandPath} showSize={showSize}
          />
        ))}
        {expanded && node.hasChildren && node.children.length === 0 && !node.loading && (
          <div
            className="flex items-center gap-1 text-[11px] text-text-muted cursor-pointer hover:text-text-primary"
            style={{ paddingLeft: `${(depth + 1) * 16 + 20}px` }}
            onClick={() => onLoadMore?.(node.path)}
          >
            <SpinnerIcon />
            <span>{t('loadMore')}</span>
          </div>
        )}
      </>
    )

    if (dndEnabled) {
      return (
        <DraggableItem id={node.path} enabled>
          <DroppableFolder id={node.path} enabled>
            {({ isDropTarget: active }) => folderContent(active)}
          </DroppableFolder>
        </DraggableItem>
      )
    }
    return <div>{folderContent(false)}</div>
  }

  // File node
  const fileContent = (
    <div
      className={`group/tree-item flex w-full items-center gap-1.5 rounded px-2 py-1 text-left text-xs cursor-pointer ${
        isActive ? 'bg-accent/10 text-accent' : 'hover:bg-surface-tertiary text-text-primary'
      }`}
      style={{ paddingLeft: `${depth * 16 + 20}px` }}
      onClick={() => onSelect(node.path)}
    >
      <FileIcon name={node.name} />
      <span className="truncate">{node.name}</span>
      {sizeLabel}
      {deleteBtn}
    </div>
  )

  if (dndEnabled) {
    return <DraggableItem id={node.path} enabled>{fileContent}</DraggableItem>
  }
  return fileContent
}

// --- Helpers ---

function findNode(tree: TreeNode[], path: string): TreeNode | undefined {
  for (const node of tree) {
    if (node.path === path) return node
    if (node.children.length > 0) {
      const found = findNode(node.children, path)
      if (found) return found
    }
  }
  return undefined
}

// --- FileTreePanel (main export) ---

interface FileTreePanelProps {
  tree: TreeNode[]
  filesLoading: boolean
  activePath: string | null
  onSelect: (path: string) => void
  onDelete?: (path: string, isDir: boolean) => void
  onLoadMore?: (path: string) => void
  onMove?: (fromPath: string, toFolder: string) => void
  showSize?: boolean
}

export function FileTreePanel({
  tree, filesLoading, activePath, onSelect, onDelete, onLoadMore, onMove, showSize,
}: FileTreePanelProps) {
  const { t } = useTranslation('common')
  const dndEnabled = !!onMove
  const [activeId, setActiveId] = useState<string | null>(null)
  const [autoExpandPath, setAutoExpandPath] = useState<string | null>(null)
  const autoExpandTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
  )

  const handleDragStart = useCallback((e: DragStartEvent) => {
    setActiveId(String(e.active.id))
  }, [])

  const handleDragOver = useCallback((e: DragOverEvent) => {
    const overId = e.over?.id ? String(e.over.id) : null
    if (autoExpandTimerRef.current) clearTimeout(autoExpandTimerRef.current)
    if (overId && overId !== '__root__') {
      autoExpandTimerRef.current = setTimeout(() => setAutoExpandPath(overId), 800)
    } else {
      setAutoExpandPath(null)
    }
  }, [])

  const handleDragEnd = useCallback((e: DragEndEvent) => {
    if (autoExpandTimerRef.current) clearTimeout(autoExpandTimerRef.current)
    setActiveId(null)
    setAutoExpandPath(null)

    const fromId = String(e.active.id)
    const overId = e.over?.id ? String(e.over.id) : null
    if (!overId || !onMove) return

    const toFolder = overId === '__root__' ? '' : overId
    if (fromId !== toFolder) onMove(fromId, toFolder)
  }, [onMove])

  const handleDragCancel = useCallback(() => {
    if (autoExpandTimerRef.current) clearTimeout(autoExpandTimerRef.current)
    setActiveId(null)
    setAutoExpandPath(null)
  }, [])

  const activeNode = useMemo(
    () => (activeId ? findNode(tree, activeId) : undefined),
    [activeId, tree],
  )

  if (filesLoading) {
    return (
      <div className="flex items-center justify-center py-8">
        <SpinnerIcon />
      </div>
    )
  }

  if (tree.length === 0) {
    return <p className="px-3 py-4 text-xs text-text-muted">{t('noFiles')}</p>
  }

  const treeContent = (
    <div className="flex-1 min-h-0">
      {dndEnabled ? (
        <RootDropZone>
          {tree.map((node) => (
            <TreeItem
              key={node.path} node={node} depth={0} activePath={activePath}
              onSelect={onSelect} onDelete={onDelete} onLoadMore={onLoadMore}
              dndEnabled showSize={showSize} autoExpandPath={autoExpandPath}
            />
          ))}
        </RootDropZone>
      ) : (
        tree.map((node) => (
          <TreeItem
            key={node.path} node={node} depth={0} activePath={activePath}
            onSelect={onSelect} onDelete={onDelete} onLoadMore={onLoadMore}
            dndEnabled={false} showSize={showSize} autoExpandPath={null}
          />
        ))
      )}
    </div>
  )

  if (!dndEnabled) return treeContent

  return (
    <DndContext
      sensors={sensors}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
      onDragCancel={handleDragCancel}
    >
      {treeContent}
      {createPortal(
        <DragOverlay dropAnimation={null}>
          {activeNode ? <DragPreview name={activeNode.name} isDir={activeNode.isDir} /> : null}
        </DragOverlay>,
        document.body,
      )}
    </DndContext>
  )
}

