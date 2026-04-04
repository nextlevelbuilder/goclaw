/**
 * DnD wrapper components for the file tree: draggable items and droppable folders.
 * Uses @dnd-kit/core primitives.
 */
import { useDraggable, useDroppable } from "@dnd-kit/core";

/** Draggable wrapper for tree items (files and folders). */
export function DraggableItem({
  id,
  enabled,
  children,
}: {
  id: string;
  enabled: boolean;
  children: React.ReactNode;
}) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id,
    disabled: !enabled,
  });

  return (
    <div
      ref={setNodeRef}
      {...(enabled ? { ...listeners, ...attributes } : {})}
      className={isDragging ? "opacity-40" : ""}
    >
      {children}
    </div>
  );
}

/** Droppable wrapper for folder items. */
export function DroppableFolder({
  id,
  enabled,
  children,
}: {
  id: string;
  enabled: boolean;
  children: (props: { isDropTarget: boolean }) => React.ReactNode;
}) {
  const { setNodeRef, isOver } = useDroppable({
    id,
    disabled: !enabled,
  });

  return (
    <div ref={setNodeRef}>
      {children({ isDropTarget: isOver })}
    </div>
  );
}

/** Root-level droppable zone — dropping here moves to root (""). */
export function RootDropZone({ children }: { children: React.ReactNode }) {
  const { setNodeRef, isOver } = useDroppable({ id: "__root__" });

  return (
    <div ref={setNodeRef} className={`min-h-full ${isOver ? "bg-primary/5" : ""}`}>
      {children}
    </div>
  );
}
