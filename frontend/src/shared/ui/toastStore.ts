import { create } from 'zustand';

export type ToastType = 'success' | 'error' | 'info';

export interface ToastOptions {
  description?: string;
  durationMs?: number;
}

interface Toast {
  id: string;
  message: string;
  type: ToastType;
  description?: string;
}

interface ToastState {
  toasts: Toast[];
  addToast: (message: string, type?: ToastType, options?: ToastOptions) => void;
  removeToast: (id: string) => void;
}

export const useToastStore = create<ToastState>((set) => ({
  toasts: [],
  addToast: (message, type = 'info', options) => {
    const id = Math.random().toString(36).slice(2, 9);
    set((state) => ({ toasts: [...state.toasts, { id, message, type, description: options?.description }] }));
    setTimeout(() => {
      set((state) => ({ toasts: state.toasts.filter((t) => t.id !== id) }));
    }, options?.durationMs ?? 5000);
  },
  removeToast: (id) =>
    set((state) => ({ toasts: state.toasts.filter((t) => t.id !== id) })),
}));
