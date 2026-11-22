import { useToastStore } from '@/shared/ui/toastStore';
import { X, CheckCircle, AlertCircle, Info } from 'lucide-react';
import { cn } from '@/shared/lib/utils';
import { useT } from '@/shared/i18n';

export function ToastContainer() {
  const { toasts, removeToast } = useToastStore();
  const t = useT();

  return (
    <div className="fixed bottom-4 right-4 z-[200] flex w-[calc(100vw-2rem)] max-w-md flex-col gap-2 sm:w-full" aria-live="polite">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          role={toast.type === 'error' ? 'alert' : 'status'}
          className={cn(
            "glass glass-dense relative flex items-start gap-3 rounded-2xl px-4 py-3 pr-10 text-label transition-all animate-in slide-in-from-right-5"
          )}
        >
          {toast.type === 'success' && <CheckCircle className="mt-0.5 h-5 w-5 shrink-0 text-green" />}
          {toast.type === 'error' && <AlertCircle className="mt-0.5 h-5 w-5 shrink-0 text-red" />}
          {toast.type === 'info' && <Info className="mt-0.5 h-5 w-5 shrink-0 text-label-2" />}
          <div className="min-w-0 flex-1 space-y-1">
            <p className="break-words text-sm font-medium leading-5">{toast.message}</p>
            {toast.description && (
              <p className="break-words text-xs text-label-2">{toast.description}</p>
            )}
          </div>
          <button
            onClick={() => removeToast(toast.id)}
            className="absolute right-3 top-3 opacity-70 transition-opacity hover:opacity-100"
            aria-label={t('common.closeToast')}
            title={t('common.closeToast')}
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      ))}
    </div>
  );
}
