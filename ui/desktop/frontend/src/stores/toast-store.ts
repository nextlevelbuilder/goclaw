import { create } from 'zustand'

export type ToastVariant = 'default' | 'success' | 'destructive' | 'warning'

export interface ToastAction {
  label: string
  onClick: () => void
}

export interface Toast {
  id: string
  title: string
  message?: string
  variant: ToastVariant
  /** Auto-dismiss delay in ms. Defaults to 4000 (8000 for errors). 0 keeps it until dismissed. */
  duration?: number
  /** Optional inline action such as "Try again". */
  action?: ToastAction
}

interface ToastState {
  toasts: Toast[]
  addToast: (toast: Omit<Toast, 'id'>) => string
  dismiss: (id: string) => void
}

let counter = 0
const timers = new Map<string, ReturnType<typeof setTimeout>>()

export const useToastStore = create<ToastState>((set) => ({
  toasts: [],
  addToast: (toast) => {
    const id = String(++counter)
    set((s) => ({ toasts: [...s.toasts, { ...toast, id }] }))

    const duration = toast.duration ?? (toast.variant === 'destructive' ? 8000 : 4000)
    if (duration > 0) {
      timers.set(id, setTimeout(() => {
        timers.delete(id)
        set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }))
      }, duration))
    }
    return id
  },
  dismiss: (id) => {
    const timer = timers.get(id)
    if (timer) {
      clearTimeout(timer)
      timers.delete(id)
    }
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }))
  },
}))

type ToastOptions = Pick<Toast, 'duration' | 'action'>

// Convenience functions for one-line usage across the app
export const toast = {
  success: (title: string, message?: string, opts?: ToastOptions) =>
    useToastStore.getState().addToast({ title, message, variant: 'success', ...opts }),
  error: (title: string, message?: string, opts?: ToastOptions) =>
    useToastStore.getState().addToast({ title, message, variant: 'destructive', ...opts }),
  warning: (title: string, message?: string, opts?: ToastOptions) =>
    useToastStore.getState().addToast({ title, message, variant: 'warning', ...opts }),
  info: (title: string, message?: string, opts?: ToastOptions) =>
    useToastStore.getState().addToast({ title, message, variant: 'default', ...opts }),
}
