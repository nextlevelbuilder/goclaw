import { create } from 'zustand'

export interface Toast {
  id: string
  title: string
  message?: string
  variant: 'default' | 'success' | 'destructive' | 'warning'
  duration?: number
}

interface ToastState {
  toasts: Toast[]
  addToast: (toast: Omit<Toast, 'id'>) => void
  dismiss: (id: string) => void
}

let counter = 0

export const useToastStore = create<ToastState>((set) => ({
  toasts: [],
  addToast: (toast) => {
    const id = String(++counter)
    set((s) => ({ toasts: [...s.toasts, { ...toast, id }] }))

    const duration = toast.duration ?? 4000
    setTimeout(() => {
      set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }))
    }, duration)
  },
  dismiss: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}))

// Convenience functions for one-line usage across the app
export const toast = {
  success: (title: string, message?: string) =>
    useToastStore.getState().addToast({ title, message, variant: 'success' }),
  error: (title: string, message?: string) =>
    useToastStore.getState().addToast({ title, message, variant: 'destructive' }),
  warning: (title: string, message?: string) =>
    useToastStore.getState().addToast({ title, message, variant: 'warning' }),
  info: (title: string, message?: string) =>
    useToastStore.getState().addToast({ title, message, variant: 'default' }),
}
