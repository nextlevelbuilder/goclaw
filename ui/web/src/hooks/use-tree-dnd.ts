import { useState, useCallback, useRef } from "react";
import {
  PointerSensor,
  KeyboardSensor,
  useSensor,
  useSensors,
  type DragStartEvent,
  type DragOverEvent,
  type DragEndEvent,
} from "@dnd-kit/core";

/** Encapsulates @dnd-kit sensor setup and drag state for file tree DnD. */
export function useTreeDnd(onMove?: (fromPath: string, toFolder: string) => void) {
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
    useSensor(KeyboardSensor),
  );

  const [activeId, setActiveId] = useState<string | null>(null);
  const [overId, setOverId] = useState<string | null>(null);

  // Track hover timer for auto-expand.
  const hoverTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [autoExpandPath, setAutoExpandPath] = useState<string | null>(null);

  const clearHoverTimer = useCallback(() => {
    if (hoverTimerRef.current) {
      clearTimeout(hoverTimerRef.current);
      hoverTimerRef.current = null;
    }
  }, []);

  const handleDragStart = useCallback((event: DragStartEvent) => {
    setActiveId(String(event.active.id));
  }, []);

  const handleDragOver = useCallback((event: DragOverEvent) => {
    const overId = event.over?.id ? String(event.over.id) : null;
    setOverId(overId);

    // Auto-expand: start timer when hovering a folder.
    clearHoverTimer();
    if (overId) {
      hoverTimerRef.current = setTimeout(() => {
        setAutoExpandPath(overId);
      }, 800);
    }
  }, [clearHoverTimer]);

  const handleDragEnd = useCallback((event: DragEndEvent) => {
    clearHoverTimer();
    setAutoExpandPath(null);
    const fromPath = String(event.active.id);
    const toFolder = event.over?.id ? String(event.over.id) : null;

    setActiveId(null);
    setOverId(null);

    if (!onMove || toFolder === null) return;

    // Map root drop zone ID to empty string.
    const dest = toFolder === "__root__" ? "" : toFolder;

    // Prevent dropping onto self or own descendant.
    if (fromPath === dest || dest.startsWith(fromPath + "/")) return;

    onMove(fromPath, dest);
  }, [onMove, clearHoverTimer]);

  const handleDragCancel = useCallback(() => {
    clearHoverTimer();
    setAutoExpandPath(null);
    setActiveId(null);
    setOverId(null);
  }, [clearHoverTimer]);

  return {
    sensors,
    activeId,
    overId,
    autoExpandPath,
    handlers: {
      onDragStart: handleDragStart,
      onDragOver: handleDragOver,
      onDragEnd: handleDragEnd,
      onDragCancel: handleDragCancel,
    },
  };
}
