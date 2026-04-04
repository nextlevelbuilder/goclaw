// Recursive file tree with drag-and-drop move support for storage management.
// Uses @dnd-kit/core for DnD — mirrors web UI file-tree.tsx adapted for desktop styling.

import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { DndContext, DragOverlay } from '@dnd-kit/core'
import type { TreeNode } from '../../lib/file-helpers'
import { TreeItem, RootDropZone, DragPreview, SpinnerIcon } from './file-tree-node'
import { useFileTreeDnd } from './use-file-tree-dnd'

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

  const {
    sensors, activeNode, autoExpandPath,
    handleDragStart, handleDragOver, handleDragEnd, handleDragCancel,
  } = useFileTreeDnd({ tree, onMove })

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
