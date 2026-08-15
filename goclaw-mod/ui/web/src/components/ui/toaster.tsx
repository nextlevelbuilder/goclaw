import { CheckCircle, XCircle, AlertTriangle, Info, X } from "lucide-react";
import { useToastStore, type Toast } from "@/stores/use-toast-store";
import { cn } from "@/lib/utils";

const icons: Record<Toast["variant"], typeof Info> = {
  success: CheckCircle,
  destructive: XCircle,
  warning: AlertTriangle,
  default: Info,
};

const styles: Record<Toast["variant"], string> = {
  success: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  destructive: "border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300",
  warning: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300",
  default: "border-border bg-card text-foreground",
};

function ToastItem({ toast }: { toast: Toast }) {
  const dismiss = useToastStore((s) => s.dismiss);
  const Icon = icons[toast.variant];

  return (
    <div
      className={cn(
        "pointer-events-auto flex w-full items-start gap-3 overflow-hidden rounded-lg border p-4 shadow-lg backdrop-blur-sm animate-in slide-in-from-bottom-2 fade-in duration-200",
        styles[toast.variant],
      )}
      role="alert"
    >
      <Icon className="mt-0.5 h-4 w-4 shrink-0" />
      <div className="flex-1 min-w-0 space-y-0.5">
        <p className="text-sm font-medium break-words">{toast.title}</p>
        {toast.message && (
          <p className="text-xs opacity-80 break-words">{toast.message}</p>
        )}
      </div>
      <button
        onClick={() => dismiss(toast.id)}
        className="shrink-0 rounded-sm opacity-50 hover:opacity-100 transition-opacity"
      >
        <X className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}

export function Toaster() {
  const toasts = useToastStore((s) => s.toasts);

  if (toasts.length === 0) return null;

  return (
    <div className="fixed bottom-4 right-4 z-[100] flex max-w-sm flex-col-reverse gap-2 pointer-events-none sm:bottom-6 sm:right-6 safe-bottom safe-right">
      {toasts.slice(-3).map((t) => (
        <ToastItem key={t.id} toast={t} />
      ))}
    </div>
  );
}
