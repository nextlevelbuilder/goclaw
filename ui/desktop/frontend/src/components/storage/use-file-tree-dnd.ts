// Drag-and-drop hook for the storage file tree.
// Encapsulates @dnd-kit sensor setup, auto-expand-on-hover logic, and move dispatch.

import { useState, useCallback, useRef, useMemo } from 'react'
import {
  PointerSensor,
  useSensor,
  useSensors,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
} from '@dnd-kit/core'
import type { TreeNode } from '../../lib/file-helpers'

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

interface UseFileTreeDndOptions {
  tree: TreeNode[]
  onMove?: (fromPath: string, toFolder: string) => void
}

export function useFileTreeDnd({ tree, onMove }: UseFileTreeDndOptions) {
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

  return {
    sensors,
    activeId,
    activeNode,
    autoExpandPath,
    handleDragStart,
    handleDragOver,
    handleDragEnd,
    handleDragCancel,
  }
}
