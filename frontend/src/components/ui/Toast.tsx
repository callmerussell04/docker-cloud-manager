import { useToastStore } from '@/store/toastStore';
import { X, CheckCircle, AlertCircle, Info } from 'lucide-react';
import { cn } from '@/lib/utils';

export function ToastContainer() {
  const { toasts, removeToast } = useToastStore();

  return (
    <div className="fixed bottom-4 right-4 z-[200] flex w-[calc(100vw-2rem)] max-w-md flex-col gap-2 sm:w-full">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          className={cn(
            "relative flex items-start gap-3 rounded-xl border px-4 py-3 pr-10 shadow-lg transition-all animate-in slide-in-from-right-5",
            toast.type === 'success' && "bg-green-50 border-green-200 text-green-900 dark:bg-green-950 dark:border-green-900 dark:text-green-100",
            toast.type === 'error' && "bg-red-50 border-red-200 text-red-900 dark:bg-red-950 dark:border-red-900 dark:text-red-100",
            toast.type === 'info' && "bg-blue-50 border-blue-200 text-blue-900 dark:bg-blue-950 dark:border-blue-900 dark:text-blue-100"
          )}
        >
          {toast.type === 'success' && <CheckCircle className="mt-0.5 h-5 w-5 shrink-0" />}
          {toast.type === 'error' && <AlertCircle className="mt-0.5 h-5 w-5 shrink-0" />}
          {toast.type === 'info' && <Info className="mt-0.5 h-5 w-5 shrink-0" />}
          <div className="min-w-0 flex-1 space-y-1">
            <p className="break-words text-sm font-medium leading-5">{toast.message}</p>
            {toast.description && (
              <p className="break-words text-xs opacity-80">{toast.description}</p>
            )}
          </div>
          <button
            onClick={() => removeToast(toast.id)}
            className="absolute right-3 top-3 opacity-70 transition-opacity hover:opacity-100"
            aria-label="Закрыть уведомление"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      ))}
    </div>
  );
}
