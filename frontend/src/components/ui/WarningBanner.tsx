import { AlertTriangle, X } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from '@/lib/utils';

interface WarningBannerProps {
  children: ReactNode;
  className?: string;
  dismissLabel?: string;
  onDismiss?: () => void;
}

export function WarningBanner({ children, className, dismissLabel = 'Dismiss', onDismiss }: WarningBannerProps) {
  return (
    <div
      className={cn(
        'flex items-start gap-3 rounded-xl border border-yellow-200 bg-yellow-50 p-3 text-sm text-yellow-800 dark:border-yellow-900/50 dark:bg-yellow-900/20 dark:text-yellow-300',
        className
      )}
    >
      <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
      <div className="min-w-0 flex-1 leading-relaxed">{children}</div>
      {onDismiss && (
        <button
          type="button"
          onClick={onDismiss}
          aria-label={dismissLabel}
          className="rounded-md p-1 text-yellow-700 transition-colors hover:bg-yellow-100 hover:text-yellow-900 focus:outline-none focus:ring-2 focus:ring-yellow-500/40 dark:text-yellow-300 dark:hover:bg-yellow-900/40 dark:hover:text-yellow-100"
        >
          <X className="h-4 w-4" />
        </button>
      )}
    </div>
  );
}
