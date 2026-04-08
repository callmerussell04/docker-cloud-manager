import { useToastStore } from '@/store/toastStore';
import { X, CheckCircle, AlertCircle, Info } from 'lucide-react';
import { cn } from '@/lib/utils';

export function ToastContainer() {
  const { toasts, removeToast } = useToastStore();

  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          className={cn(
            "flex items-center gap-3 px-4 py-3 rounded-xl backdrop-blur-md border shadow-lg transition-all animate-in slide-in-from-right-5",
            toast.type === 'success' && "bg-green-500/20 border-green-500/30 text-green-800 dark:text-green-200",
            toast.type === 'error' && "bg-red-500/20 border-red-500/30 text-red-800 dark:text-red-200",
            toast.type === 'info' && "bg-blue-500/20 border-blue-500/30 text-blue-800 dark:text-blue-200"
          )}
        >
          {toast.type === 'success' && <CheckCircle className="w-5 h-5" />}
          {toast.type === 'error' && <AlertCircle className="w-5 h-5" />}
          {toast.type === 'info' && <Info className="w-5 h-5" />}
          <p className="text-sm font-medium pr-6">{toast.message}</p>
          <button
            onClick={() => removeToast(toast.id)}
            className="absolute right-3 opacity-70 hover:opacity-100"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      ))}
    </div>
  );
}