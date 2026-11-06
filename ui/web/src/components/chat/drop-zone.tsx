import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Upload } from "lucide-react";

interface DropZoneProps {
  onDrop: (files: File[]) => void;
  /** Swallow drops (so the browser does not open the file) without accepting them or showing the overlay. */
  disabled?: boolean;
  children: React.ReactNode;
}

/** Drag-and-drop overlay for file uploads. Uses a counter to handle child boundary events. */
export function DropZone({ onDrop, disabled, children }: DropZoneProps) {
  const { t } = useTranslation("chat");
  const [isDragging, setIsDragging] = useState(false);
  const dragCounterRef = useRef(0);

  return (
    <div
      className="relative flex min-h-0 flex-1 flex-col"
      onDragOver={(e) => e.preventDefault()}
      onDragEnter={(e) => {
        e.preventDefault();
        if (disabled) return;
        dragCounterRef.current++;
        if (dragCounterRef.current === 1) setIsDragging(true);
      }}
      onDragLeave={() => {
        if (disabled) return;
        dragCounterRef.current--;
        if (dragCounterRef.current === 0) setIsDragging(false);
      }}
      onDrop={(e) => {
        e.preventDefault();
        dragCounterRef.current = 0;
        setIsDragging(false);
        if (disabled) return;
        const files = Array.from(e.dataTransfer.files);
        if (files.length > 0) onDrop(files);
      }}
    >
      {children}

      {isDragging && !disabled && (
        <div className="absolute inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm">
          <div className="flex flex-col items-center gap-2 text-muted-foreground">
            <Upload className="h-10 w-10" />
            <span className="text-lg font-medium">{t("dropFilesHere")}</span>
          </div>
        </div>
      )}
    </div>
  );
}
